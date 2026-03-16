// Package registry implements the Model Registry that provides orchestrators
// with current information about all available models, including capabilities,
// pricing, and recommended use cases.
//
// See Architecture.md Section 18 for the full specification.
package registry

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ModelTier represents a model's capability/cost tier.
type ModelTier string

const (
	TierLocal    ModelTier = "local"
	TierCheap    ModelTier = "cheap"
	TierStandard ModelTier = "standard"
	TierPremium  ModelTier = "premium"
)

// ModelInfo holds metadata about a registered model.
// See Architecture Section 18.3 for the schema.
type ModelInfo struct {
	ID                       string    `json:"id"`
	Family                   string    `json:"family"`
	Source                   string    `json:"source"` // "openrouter" | "bitnet"
	Tier                     ModelTier `json:"tier"`
	ContextWindow            int       `json:"context_window"`
	MaxOutput                int       `json:"max_output"`
	PromptPerMillion         float64   `json:"prompt_per_million"`
	CompletionPerMillion     float64   `json:"completion_per_million"`
	Strengths                []string  `json:"strengths"`
	Weaknesses               []string  `json:"weaknesses"`
	SupportsTools            bool      `json:"supports_tools"`
	SupportsVision           bool      `json:"supports_vision"`
	SupportsGrammar          bool      `json:"supports_grammar_constraints"`
	RecommendedFor           []string  `json:"recommended_for"`
	NotRecommendedFor        []string  `json:"not_recommended_for"`
	HistoricalSuccessRate    *float64  `json:"historical_success_rate"`
	AvgCostPerTask           *float64  `json:"avg_cost_per_task"`
	LastUpdated              time.Time `json:"last_updated"`
}

// Registry maintains the catalog of available AI models.
type Registry struct {
	db *sql.DB
}

// RegistrySchema creates the registry tables.
const RegistrySchema = `
CREATE TABLE IF NOT EXISTS models (
    id                        TEXT PRIMARY KEY,
    family                    TEXT NOT NULL,
    source                    TEXT NOT NULL,
    tier                      TEXT NOT NULL,
    context_window            INTEGER,
    max_output                INTEGER,
    prompt_per_million        REAL,
    completion_per_million    REAL,
    strengths                 TEXT,
    weaknesses                TEXT,
    supports_tools            BOOLEAN DEFAULT 0,
    supports_vision           BOOLEAN DEFAULT 0,
    supports_grammar          BOOLEAN DEFAULT 0,
    recommended_for           TEXT,
    not_recommended_for       TEXT,
    historical_success_rate   REAL,
    avg_cost_per_task         REAL,
    last_updated              DATETIME DEFAULT CURRENT_TIMESTAMP
);
`

// NewRegistry creates a Registry backed by SQLite at the given path.
// The path is typically ~/.axiom/registry.db.
func NewRegistry(dbPath string) (*Registry, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open registry db: %w", err)
	}
	if _, err := db.Exec(RegistrySchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init registry schema: %w", err)
	}
	return &Registry{db: db}, nil
}

// NewRegistryFromDB creates a Registry using an existing DB connection.
// Used for testing with in-memory databases.
func NewRegistryFromDB(db *sql.DB) (*Registry, error) {
	if _, err := db.Exec(RegistrySchema); err != nil {
		return nil, fmt.Errorf("init registry schema: %w", err)
	}
	return &Registry{db: db}, nil
}

// Close closes the registry database.
func (r *Registry) Close() error {
	return r.db.Close()
}

// Upsert inserts or updates a model in the registry.
func (r *Registry) Upsert(m *ModelInfo) error {
	strengths, _ := json.Marshal(m.Strengths)
	weaknesses, _ := json.Marshal(m.Weaknesses)
	recFor, _ := json.Marshal(m.RecommendedFor)
	notRecFor, _ := json.Marshal(m.NotRecommendedFor)

	_, err := r.db.Exec(`
		INSERT INTO models (id, family, source, tier, context_window, max_output,
			prompt_per_million, completion_per_million, strengths, weaknesses,
			supports_tools, supports_vision, supports_grammar,
			recommended_for, not_recommended_for, historical_success_rate,
			avg_cost_per_task, last_updated)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			family=excluded.family, source=excluded.source, tier=excluded.tier,
			context_window=excluded.context_window, max_output=excluded.max_output,
			prompt_per_million=excluded.prompt_per_million,
			completion_per_million=excluded.completion_per_million,
			strengths=excluded.strengths, weaknesses=excluded.weaknesses,
			supports_tools=excluded.supports_tools, supports_vision=excluded.supports_vision,
			supports_grammar=excluded.supports_grammar,
			recommended_for=excluded.recommended_for,
			not_recommended_for=excluded.not_recommended_for,
			last_updated=excluded.last_updated`,
		m.ID, m.Family, m.Source, string(m.Tier), m.ContextWindow, m.MaxOutput,
		m.PromptPerMillion, m.CompletionPerMillion,
		string(strengths), string(weaknesses),
		m.SupportsTools, m.SupportsVision, m.SupportsGrammar,
		string(recFor), string(notRecFor),
		m.HistoricalSuccessRate, m.AvgCostPerTask, m.LastUpdated,
	)
	if err != nil {
		return fmt.Errorf("upsert model %s: %w", m.ID, err)
	}
	return nil
}

// Get retrieves a model by ID.
func (r *Registry) Get(id string) (*ModelInfo, error) {
	row := r.db.QueryRow("SELECT * FROM models WHERE id = ?", id)
	return scanModel(row)
}

// List returns all models, optionally filtered by tier and/or family.
func (r *Registry) List(tier ModelTier, family string) ([]*ModelInfo, error) {
	query := "SELECT * FROM models WHERE 1=1"
	var args []interface{}
	if tier != "" {
		query += " AND tier = ?"
		args = append(args, string(tier))
	}
	if family != "" {
		query += " AND family = ?"
		args = append(args, family)
	}
	query += " ORDER BY tier, family, id"

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list models: %w", err)
	}
	defer rows.Close()

	var models []*ModelInfo
	for rows.Next() {
		m, err := scanModelRows(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// SelectForTask returns ranked models suitable for a task given its tier and budget.
// Considers pricing, capability, historical performance, and model family diversity.
// See BUILD_PLAN step 13.5.
func (r *Registry) SelectForTask(taskTier ModelTier, budgetRemaining float64, excludeFamily string) ([]*ModelInfo, error) {
	// Get all models at or below the task tier.
	allowedTiers := tiersAtOrBelow(taskTier)
	if len(allowedTiers) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(allowedTiers))
	args := make([]interface{}, len(allowedTiers))
	for i, t := range allowedTiers {
		placeholders[i] = "?"
		args[i] = string(t)
	}

	query := fmt.Sprintf("SELECT * FROM models WHERE tier IN (%s)", strings.Join(placeholders, ","))
	if excludeFamily != "" {
		query += " AND family != ?"
		args = append(args, excludeFamily)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("select models: %w", err)
	}
	defer rows.Close()

	var candidates []*ModelInfo
	for rows.Next() {
		m, err := scanModelRows(rows)
		if err != nil {
			continue
		}
		candidates = append(candidates, m)
	}

	// Sort by: historical success rate (desc), then cost (asc).
	sort.Slice(candidates, func(i, j int) bool {
		si := successRate(candidates[i])
		sj := successRate(candidates[j])
		if si != sj {
			return si > sj
		}
		return candidates[i].CompletionPerMillion < candidates[j].CompletionPerMillion
	})

	return candidates, nil
}

// UpdatePerformance updates the historical success rate and average cost
// for a model after project completion.
// See Architecture Section 18.5.
func (r *Registry) UpdatePerformance(modelID string, successRate float64, avgCost float64) error {
	_, err := r.db.Exec(`
		UPDATE models SET historical_success_rate = ?, avg_cost_per_task = ?, last_updated = ?
		WHERE id = ?`,
		successRate, avgCost, time.Now(), modelID,
	)
	if err != nil {
		return fmt.Errorf("update performance for %s: %w", modelID, err)
	}
	return nil
}

// CostEstimate estimates the cost for given token counts.
func (r *Registry) CostEstimate(modelID string, inputTokens, outputTokens int) (float64, error) {
	m, err := r.Get(modelID)
	if err != nil {
		return 0, err
	}
	cost := float64(inputTokens)*m.PromptPerMillion/1_000_000 +
		float64(outputTokens)*m.CompletionPerMillion/1_000_000
	return cost, nil
}

// Count returns the total number of models in the registry.
func (r *Registry) Count() (int, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM models").Scan(&count)
	return count, err
}

// --- Helpers ---

func tiersAtOrBelow(tier ModelTier) []ModelTier {
	order := []ModelTier{TierLocal, TierCheap, TierStandard, TierPremium}
	var result []ModelTier
	for _, t := range order {
		result = append(result, t)
		if t == tier {
			break
		}
	}
	return result
}

func successRate(m *ModelInfo) float64 {
	if m.HistoricalSuccessRate != nil {
		return *m.HistoricalSuccessRate
	}
	return 0
}

func scanModel(row *sql.Row) (*ModelInfo, error) {
	m := &ModelInfo{}
	var strengths, weaknesses, recFor, notRecFor sql.NullString
	var hsr, acpt sql.NullFloat64

	err := row.Scan(
		&m.ID, &m.Family, &m.Source, &m.Tier, &m.ContextWindow, &m.MaxOutput,
		&m.PromptPerMillion, &m.CompletionPerMillion,
		&strengths, &weaknesses,
		&m.SupportsTools, &m.SupportsVision, &m.SupportsGrammar,
		&recFor, &notRecFor,
		&hsr, &acpt, &m.LastUpdated,
	)
	if err != nil {
		return nil, fmt.Errorf("scan model: %w", err)
	}

	if strengths.Valid {
		json.Unmarshal([]byte(strengths.String), &m.Strengths)
	}
	if weaknesses.Valid {
		json.Unmarshal([]byte(weaknesses.String), &m.Weaknesses)
	}
	if recFor.Valid {
		json.Unmarshal([]byte(recFor.String), &m.RecommendedFor)
	}
	if notRecFor.Valid {
		json.Unmarshal([]byte(notRecFor.String), &m.NotRecommendedFor)
	}
	if hsr.Valid {
		m.HistoricalSuccessRate = &hsr.Float64
	}
	if acpt.Valid {
		m.AvgCostPerTask = &acpt.Float64
	}
	return m, nil
}

func scanModelRows(rows *sql.Rows) (*ModelInfo, error) {
	m := &ModelInfo{}
	var strengths, weaknesses, recFor, notRecFor sql.NullString
	var hsr, acpt sql.NullFloat64

	err := rows.Scan(
		&m.ID, &m.Family, &m.Source, &m.Tier, &m.ContextWindow, &m.MaxOutput,
		&m.PromptPerMillion, &m.CompletionPerMillion,
		&strengths, &weaknesses,
		&m.SupportsTools, &m.SupportsVision, &m.SupportsGrammar,
		&recFor, &notRecFor,
		&hsr, &acpt, &m.LastUpdated,
	)
	if err != nil {
		return nil, fmt.Errorf("scan model row: %w", err)
	}

	if strengths.Valid {
		json.Unmarshal([]byte(strengths.String), &m.Strengths)
	}
	if weaknesses.Valid {
		json.Unmarshal([]byte(weaknesses.String), &m.Weaknesses)
	}
	if recFor.Valid {
		json.Unmarshal([]byte(recFor.String), &m.RecommendedFor)
	}
	if notRecFor.Valid {
		json.Unmarshal([]byte(notRecFor.String), &m.NotRecommendedFor)
	}
	if hsr.Valid {
		m.HistoricalSuccessRate = &hsr.Float64
	}
	if acpt.Valid {
		m.AvgCostPerTask = &acpt.Float64
	}
	return m, nil
}
