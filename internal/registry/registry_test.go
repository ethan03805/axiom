package registry

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func setupTestRegistry(t *testing.T) *Registry {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	reg, err := NewRegistryFromDB(db)
	if err != nil {
		t.Fatalf("create registry: %v", err)
	}
	return reg
}

func seedModels(t *testing.T, reg *Registry) {
	t.Helper()
	sr := 0.85
	models := []*ModelInfo{
		{
			ID: "anthropic/claude-4-sonnet", Family: "anthropic", Source: "openrouter",
			Tier: TierStandard, ContextWindow: 200000, MaxOutput: 64000,
			PromptPerMillion: 3.0, CompletionPerMillion: 15.0,
			Strengths: []string{"code-generation", "reasoning"},
			SupportsTools: true, LastUpdated: time.Now(),
			HistoricalSuccessRate: &sr,
		},
		{
			ID: "openai/gpt-4o", Family: "openai", Source: "openrouter",
			Tier: TierStandard, ContextWindow: 128000, MaxOutput: 16384,
			PromptPerMillion: 2.5, CompletionPerMillion: 10.0,
			Strengths: []string{"code-generation", "debugging"},
			SupportsTools: true, LastUpdated: time.Now(),
		},
		{
			ID: "anthropic/claude-4-opus", Family: "anthropic", Source: "openrouter",
			Tier: TierPremium, ContextWindow: 200000, MaxOutput: 64000,
			PromptPerMillion: 15.0, CompletionPerMillion: 75.0,
			Strengths: []string{"complex-algorithms", "architecture"},
			SupportsTools: true, LastUpdated: time.Now(),
		},
		{
			ID: "meta/llama-3-8b", Family: "meta", Source: "openrouter",
			Tier: TierCheap, ContextWindow: 8192, MaxOutput: 4096,
			PromptPerMillion: 0.1, CompletionPerMillion: 0.3,
			LastUpdated: time.Now(),
		},
		{
			ID: "falcon3-1b", Family: "local", Source: "bitnet",
			Tier: TierLocal, ContextWindow: 4096, MaxOutput: 2048,
			PromptPerMillion: 0, CompletionPerMillion: 0,
			SupportsGrammar: true, LastUpdated: time.Now(),
		},
	}
	for _, m := range models {
		if err := reg.Upsert(m); err != nil {
			t.Fatalf("seed model %s: %v", m.ID, err)
		}
	}
}

func TestUpsertAndGet(t *testing.T) {
	reg := setupTestRegistry(t)

	m := &ModelInfo{
		ID: "test/model-1", Family: "test", Source: "openrouter",
		Tier: TierStandard, ContextWindow: 8192, MaxOutput: 4096,
		PromptPerMillion: 1.0, CompletionPerMillion: 5.0,
		Strengths: []string{"testing"}, LastUpdated: time.Now(),
	}
	if err := reg.Upsert(m); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := reg.Get("test/model-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Family != "test" {
		t.Errorf("family = %s", got.Family)
	}
	if got.Tier != TierStandard {
		t.Errorf("tier = %s", got.Tier)
	}
	if got.CompletionPerMillion != 5.0 {
		t.Errorf("completion price = %f", got.CompletionPerMillion)
	}
	if len(got.Strengths) != 1 || got.Strengths[0] != "testing" {
		t.Errorf("strengths = %v", got.Strengths)
	}
}

func TestUpsertUpdatesExisting(t *testing.T) {
	reg := setupTestRegistry(t)

	m := &ModelInfo{
		ID: "test/model", Family: "test", Source: "openrouter",
		Tier: TierCheap, PromptPerMillion: 1.0, CompletionPerMillion: 2.0,
		LastUpdated: time.Now(),
	}
	reg.Upsert(m)

	// Update the tier and pricing.
	m.Tier = TierStandard
	m.CompletionPerMillion = 10.0
	reg.Upsert(m)

	got, _ := reg.Get("test/model")
	if got.Tier != TierStandard {
		t.Errorf("expected updated tier standard, got %s", got.Tier)
	}
	if got.CompletionPerMillion != 10.0 {
		t.Errorf("expected updated price 10.0, got %f", got.CompletionPerMillion)
	}
}

func TestListAll(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	models, err := reg.List("", "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(models) != 5 {
		t.Errorf("expected 5 models, got %d", len(models))
	}
}

func TestListByTier(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	models, _ := reg.List(TierStandard, "")
	if len(models) != 2 {
		t.Errorf("expected 2 standard models, got %d", len(models))
	}

	models, _ = reg.List(TierPremium, "")
	if len(models) != 1 {
		t.Errorf("expected 1 premium model, got %d", len(models))
	}

	models, _ = reg.List(TierLocal, "")
	if len(models) != 1 {
		t.Errorf("expected 1 local model, got %d", len(models))
	}
}

func TestListByFamily(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	models, _ := reg.List("", "anthropic")
	if len(models) != 2 {
		t.Errorf("expected 2 anthropic models, got %d", len(models))
	}
}

func TestSelectForTask(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	// Standard task: should get local, cheap, and standard models.
	models, err := reg.SelectForTask(TierStandard, 10.0, "")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if len(models) != 4 { // local + cheap + 2 standard
		t.Errorf("expected 4 models for standard tier, got %d", len(models))
	}

	// Models with historical success rate should be ranked first.
	if models[0].ID != "anthropic/claude-4-sonnet" {
		t.Errorf("expected claude-4-sonnet first (has success rate), got %s", models[0].ID)
	}
}

func TestSelectForTaskWithFamilyExclusion(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	// Exclude anthropic family (for reviewer diversity).
	models, _ := reg.SelectForTask(TierStandard, 10.0, "anthropic")
	for _, m := range models {
		if m.Family == "anthropic" {
			t.Errorf("expected no anthropic models when excluded, got %s", m.ID)
		}
	}
}

func TestSelectForTaskLocalOnly(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	// Local task: should only get local models.
	models, _ := reg.SelectForTask(TierLocal, 0, "")
	if len(models) != 1 {
		t.Errorf("expected 1 local model, got %d", len(models))
	}
	if models[0].ID != "falcon3-1b" {
		t.Errorf("expected falcon3-1b, got %s", models[0].ID)
	}
}

func TestUpdatePerformance(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	if err := reg.UpdatePerformance("openai/gpt-4o", 0.92, 0.45); err != nil {
		t.Fatalf("update performance: %v", err)
	}

	m, _ := reg.Get("openai/gpt-4o")
	if m.HistoricalSuccessRate == nil || *m.HistoricalSuccessRate != 0.92 {
		t.Errorf("expected success rate 0.92")
	}
	if m.AvgCostPerTask == nil || *m.AvgCostPerTask != 0.45 {
		t.Errorf("expected avg cost 0.45")
	}
}

func TestCostEstimate(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	// claude-4-sonnet: prompt=$3/M, completion=$15/M
	// 1000 input + 500 output = $0.003 + $0.0075 = $0.0105
	cost, err := reg.CostEstimate("anthropic/claude-4-sonnet", 1000, 500)
	if err != nil {
		t.Fatalf("cost estimate: %v", err)
	}
	if cost < 0.0104 || cost > 0.0106 {
		t.Errorf("cost = $%.6f, want ~$0.0105", cost)
	}
}

func TestMergeCuratedData(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	curated := []*ModelInfo{
		{
			ID:            "openai/gpt-4o",
			Strengths:     []string{"code-review", "debugging", "refactoring"},
			Weaknesses:    []string{"slow-for-trivial"},
			RecommendedFor: []string{"standard-coding"},
		},
	}

	if err := reg.MergeCuratedData(curated); err != nil {
		t.Fatalf("merge: %v", err)
	}

	m, _ := reg.Get("openai/gpt-4o")
	if len(m.Strengths) != 3 {
		t.Errorf("expected 3 strengths after merge, got %d", len(m.Strengths))
	}
	if len(m.RecommendedFor) != 1 {
		t.Errorf("expected 1 recommended_for, got %d", len(m.RecommendedFor))
	}
}

func TestClassifyTier(t *testing.T) {
	tests := []struct {
		pricePerToken float64
		expected      ModelTier
	}{
		{0.0000001, TierCheap},   // $0.10/M
		{0.000003, TierStandard}, // $3/M
		{0.000015, TierStandard}, // $15/M
		{0.000075, TierPremium},  // $75/M
	}
	for _, tt := range tests {
		got := classifyTier(tt.pricePerToken)
		if got != tt.expected {
			t.Errorf("classifyTier(%f) = %s, want %s", tt.pricePerToken, got, tt.expected)
		}
	}
}

func TestExtractFamily(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"anthropic/claude-4-sonnet", "anthropic"},
		{"openai/gpt-4o", "openai"},
		{"meta/llama-3-8b", "meta"},
		{"standalone-model", "standalone-model"},
	}
	for _, tt := range tests {
		got := extractFamily(tt.modelID)
		if got != tt.expected {
			t.Errorf("extractFamily(%s) = %s, want %s", tt.modelID, got, tt.expected)
		}
	}
}

func TestCount(t *testing.T) {
	reg := setupTestRegistry(t)
	seedModels(t, reg)

	count, _ := reg.Count()
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}
