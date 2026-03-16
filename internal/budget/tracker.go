package budget

import (
	"fmt"

	"github.com/ethan03805/axiom/internal/state"
)

// CostReport holds a comprehensive cost breakdown at all granularities.
// See Architecture Section 21.2.
type CostReport struct {
	ProjectTotal  float64            `json:"project_total_usd"`
	ByTask        map[string]float64 `json:"by_task"`
	ByModel       map[string]float64 `json:"by_model"`
	ByAgentType   map[string]float64 `json:"by_agent_type"`
	BudgetMax     float64            `json:"budget_max_usd"`
	BudgetUsed    float64            `json:"budget_used_pct"`
	Remaining     float64            `json:"remaining_usd"`
	ProjectedTotal float64           `json:"projected_total_usd"`
	ExternalMode  bool               `json:"external_mode"`
	Disclaimer    string             `json:"disclaimer,omitempty"`
}

// Tracker provides cost tracking and reporting at all granularities.
// See Architecture Section 21.2.
type Tracker struct {
	db           *state.DB
	maxUSD       float64
	externalMode bool
}

// NewTracker creates a cost Tracker.
func NewTracker(db *state.DB, maxUSD float64, externalMode bool) *Tracker {
	return &Tracker{
		db:           db,
		maxUSD:       maxUSD,
		externalMode: externalMode,
	}
}

// GetReport generates a comprehensive cost report at all granularities.
func (t *Tracker) GetReport(completionPct float64) (*CostReport, error) {
	report := &CostReport{
		BudgetMax:    t.maxUSD,
		ExternalMode: t.externalMode,
	}

	if t.externalMode {
		report.Disclaimer = "Orchestrator inference cost not tracked (external mode)."
	}

	// Project total.
	total, err := t.db.GetProjectCost()
	if err != nil {
		return nil, fmt.Errorf("project cost: %w", err)
	}
	report.ProjectTotal = total
	report.Remaining = t.maxUSD - total

	if t.maxUSD > 0 {
		report.BudgetUsed = (total / t.maxUSD) * 100
	}

	// Projected total based on completion percentage.
	if completionPct > 0 && completionPct < 100 {
		report.ProjectedTotal = total / (completionPct / 100)
	} else {
		report.ProjectedTotal = total
	}

	// By model.
	byModel, err := t.db.GetCostByModel()
	if err == nil {
		report.ByModel = byModel
	}

	// By agent type.
	byAgent, err := t.db.GetCostByAgentType()
	if err == nil {
		report.ByAgentType = byAgent
	}

	// By task.
	report.ByTask = make(map[string]float64)
	tasks, err := t.db.ListTasks(state.TaskFilter{})
	if err == nil {
		for _, task := range tasks {
			cost, err := t.db.GetTaskCost(task.ID)
			if err == nil && cost > 0 {
				report.ByTask[task.ID] = cost
			}
		}
	}

	return report, nil
}

// GetTaskCost returns the total cost for a specific task.
func (t *Tracker) GetTaskCost(taskID string) (float64, error) {
	return t.db.GetTaskCost(taskID)
}

// GetProjectTotal returns the total project cost.
func (t *Tracker) GetProjectTotal() (float64, error) {
	return t.db.GetProjectCost()
}

// CalculateMaxRequestCost computes the maximum possible cost for an inference
// request based on max_tokens and model pricing.
// See Architecture Section 21.3 point 4.
func CalculateMaxRequestCost(maxTokens int, completionPricePerMillion float64) float64 {
	return float64(maxTokens) * completionPricePerMillion / 1_000_000
}
