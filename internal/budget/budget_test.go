package budget

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

func setupTestBudget(t *testing.T, maxUSD float64) (*Enforcer, *Tracker, *state.DB) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-budget-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	db.RunMigrations()
	t.Cleanup(func() { db.Close() })

	emitter := events.NewEmitter()

	enforcer := NewEnforcer(db, emitter, EnforcerConfig{
		MaxUSD:        maxUSD,
		WarnAtPercent: 80,
	})

	tracker := NewTracker(db, maxUSD, false)

	return enforcer, tracker, db
}

func addCost(t *testing.T, db *state.DB, taskID string, cost float64) {
	t.Helper()
	db.InsertCost(&state.CostEntry{
		TaskID:       taskID,
		AgentType:    "meeseeks",
		ModelID:      "test-model",
		InputTokens:  100,
		OutputTokens: 50,
		CostUSD:      cost,
		Timestamp:    time.Now(),
	})
}

func TestPreAuthorizeWithinBudget(t *testing.T) {
	enforcer, _, _ := setupTestBudget(t, 10.0)

	// Should allow a request costing $1 when budget is $10.
	if err := enforcer.PreAuthorize(1.0); err != nil {
		t.Errorf("expected authorization, got: %v", err)
	}
}

func TestPreAuthorizeExceedsBudget(t *testing.T) {
	enforcer, _, db := setupTestBudget(t, 1.0)

	// Create a task for the FK constraint.
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation"})

	// Spend $0.90.
	addCost(t, db, "t1", 0.90)

	// A request that could cost $0.20 should be rejected ($0.90 + $0.20 > $1.00).
	if err := enforcer.PreAuthorize(0.20); err == nil {
		t.Error("expected budget rejection for $0.20 with $0.10 remaining")
	}

	// A request costing $0.05 should still be allowed.
	if err := enforcer.PreAuthorize(0.05); err != nil {
		t.Errorf("expected authorization for $0.05: %v", err)
	}
}

func TestPreAuthorizeNoBudgetLimit(t *testing.T) {
	enforcer, _, _ := setupTestBudget(t, 0) // No limit

	// Should always allow when no budget set.
	if err := enforcer.PreAuthorize(1000.0); err != nil {
		t.Errorf("expected no limit, got: %v", err)
	}
}

func TestBudgetWarningEvent(t *testing.T) {
	enforcer, _, db := setupTestBudget(t, 10.0)

	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation"})

	var warningFired bool
	enforcer.emitter.Subscribe(events.EventBudgetWarning, func(e events.Event) {
		warningFired = true
	})

	// Spend $8.50 (85% of $10 budget, exceeds 80% warning threshold).
	addCost(t, db, "t1", 8.50)

	// Next pre-authorize should trigger the warning.
	enforcer.PreAuthorize(0.01)

	time.Sleep(100 * time.Millisecond)
	if !warningFired {
		t.Error("expected budget_warning event at 85% spend")
	}
}

func TestBudgetWarningOnlyOnce(t *testing.T) {
	enforcer, _, db := setupTestBudget(t, 10.0)
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation"})

	var warningCount int
	enforcer.emitter.Subscribe(events.EventBudgetWarning, func(e events.Event) {
		warningCount++
	})

	addCost(t, db, "t1", 8.50)

	// Multiple pre-authorize calls should only fire warning once.
	enforcer.PreAuthorize(0.01)
	enforcer.PreAuthorize(0.01)
	enforcer.PreAuthorize(0.01)

	time.Sleep(100 * time.Millisecond)
	if warningCount != 1 {
		t.Errorf("expected 1 warning, got %d", warningCount)
	}
}

func TestBudgetExhaustionPauses(t *testing.T) {
	enforcer, _, db := setupTestBudget(t, 1.0)
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation"})

	var exhaustFired bool
	enforcer.emitter.Subscribe(events.EventBudgetExhausted, func(e events.Event) {
		exhaustFired = true
	})

	// Spend the entire budget.
	addCost(t, db, "t1", 1.00)

	// RecordAndCheck should detect exhaustion.
	enforcer.RecordAndCheck(0)

	time.Sleep(100 * time.Millisecond)
	if !exhaustFired {
		t.Error("expected budget_exhausted event")
	}
	if !enforcer.IsPaused() {
		t.Error("expected enforcer to be paused")
	}

	// Further requests should be rejected.
	if err := enforcer.PreAuthorize(0.01); err == nil {
		t.Error("expected rejection while paused")
	}
}

func TestIncreaseBudgetResumes(t *testing.T) {
	enforcer, _, db := setupTestBudget(t, 1.0)
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation"})

	addCost(t, db, "t1", 1.00)
	enforcer.RecordAndCheck(0)

	if !enforcer.IsPaused() {
		t.Fatal("expected paused")
	}

	// Increase budget.
	enforcer.IncreaseBudget(5.0)

	if enforcer.IsPaused() {
		t.Error("expected resumed after budget increase")
	}
	if enforcer.MaxBudget() != 5.0 {
		t.Errorf("expected max $5.0, got $%.2f", enforcer.MaxBudget())
	}

	// Should allow requests again.
	if err := enforcer.PreAuthorize(1.0); err != nil {
		t.Errorf("expected authorization after increase: %v", err)
	}
}

// --- Tracker tests ---

func TestCostReportAllGranularities(t *testing.T) {
	_, tracker, db := setupTestBudget(t, 10.0)

	// Create tasks and add costs.
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "done", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "T2", Status: "done", Tier: "standard", TaskType: "implementation"})

	db.InsertCost(&state.CostEntry{TaskID: "t1", AgentType: "meeseeks", ModelID: "claude-sonnet", InputTokens: 100, OutputTokens: 50, CostUSD: 0.50, Timestamp: time.Now()})
	db.InsertCost(&state.CostEntry{TaskID: "t1", AgentType: "reviewer", ModelID: "gpt-4o", InputTokens: 200, OutputTokens: 100, CostUSD: 0.30, Timestamp: time.Now()})
	db.InsertCost(&state.CostEntry{TaskID: "t2", AgentType: "meeseeks", ModelID: "claude-sonnet", InputTokens: 150, OutputTokens: 75, CostUSD: 0.60, Timestamp: time.Now()})

	report, err := tracker.GetReport(50.0) // 50% complete
	if err != nil {
		t.Fatalf("get report: %v", err)
	}

	// Project total: $0.50 + $0.30 + $0.60 = $1.40
	if report.ProjectTotal < 1.39 || report.ProjectTotal > 1.41 {
		t.Errorf("project total = $%.4f, want ~$1.40", report.ProjectTotal)
	}

	// Budget used percentage: 1.40/10.0 = 14%
	if report.BudgetUsed < 13 || report.BudgetUsed > 15 {
		t.Errorf("budget used = %.1f%%, want ~14%%", report.BudgetUsed)
	}

	// Remaining: $10 - $1.40 = $8.60
	if report.Remaining < 8.50 || report.Remaining > 8.70 {
		t.Errorf("remaining = $%.2f, want ~$8.60", report.Remaining)
	}

	// Projected total at 50% complete: $1.40 / 0.5 = $2.80
	if report.ProjectedTotal < 2.70 || report.ProjectedTotal > 2.90 {
		t.Errorf("projected = $%.2f, want ~$2.80", report.ProjectedTotal)
	}

	// By model.
	if len(report.ByModel) != 2 {
		t.Errorf("expected 2 models, got %d", len(report.ByModel))
	}

	// By agent type.
	if len(report.ByAgentType) != 2 {
		t.Errorf("expected 2 agent types, got %d", len(report.ByAgentType))
	}

	// By task.
	if len(report.ByTask) != 2 {
		t.Errorf("expected 2 tasks with costs, got %d", len(report.ByTask))
	}
}

func TestCostReportExternalMode(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-budget-ext-*")
	defer os.RemoveAll(tmpDir)

	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	db, _ := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	db.RunMigrations()
	defer db.Close()

	tracker := NewTracker(db, 10.0, true) // External mode

	report, err := tracker.GetReport(0)
	if err != nil {
		t.Fatalf("get report: %v", err)
	}

	if !report.ExternalMode {
		t.Error("expected external mode flag")
	}
	if report.Disclaimer == "" {
		t.Error("expected disclaimer for external mode")
	}
}

func TestCalculateMaxRequestCost(t *testing.T) {
	// 8192 tokens at $15/million = $0.122880
	cost := CalculateMaxRequestCost(8192, 15.0)
	if cost < 0.122 || cost > 0.123 {
		t.Errorf("cost = $%.6f, want ~$0.122880", cost)
	}

	// 100 tokens at $3/million = $0.000300
	cost = CalculateMaxRequestCost(100, 3.0)
	if cost < 0.000299 || cost > 0.000301 {
		t.Errorf("cost = $%.6f, want ~$0.000300", cost)
	}
}
