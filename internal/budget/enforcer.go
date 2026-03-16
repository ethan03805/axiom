// Package budget implements budget enforcement, cost tracking, and spending
// reports for the Axiom engine. The budget system enforces per-request
// pre-authorization to prevent overspend.
//
// See Architecture.md Section 21 for the full specification.
package budget

import (
	"fmt"
	"sync"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// Enforcer handles per-request budget verification.
// Before every inference request, the engine calculates the maximum possible
// cost and verifies it fits within the remaining budget.
//
// See Architecture Section 21.3 (Dynamic pre-authorization, point 4).
type Enforcer struct {
	db      *state.DB
	emitter *events.Emitter

	mu             sync.Mutex
	maxUSD         float64
	warnAtPercent  float64
	warningEmitted bool
	exhaustEmitted bool
	paused         bool
	externalMode   bool // True if orchestrator is external (partial tracking)
}

// EnforcerConfig holds configuration for the budget enforcer.
type EnforcerConfig struct {
	MaxUSD        float64 // Project budget ceiling
	WarnAtPercent float64 // Warning threshold (default 80)
	ExternalMode  bool    // True for external client mode
}

// NewEnforcer creates a budget Enforcer.
func NewEnforcer(db *state.DB, emitter *events.Emitter, config EnforcerConfig) *Enforcer {
	if config.WarnAtPercent == 0 {
		config.WarnAtPercent = 80.0
	}
	return &Enforcer{
		db:            db,
		emitter:       emitter,
		maxUSD:        config.MaxUSD,
		warnAtPercent: config.WarnAtPercent,
		externalMode:  config.ExternalMode,
	}
}

// PreAuthorize checks if a request with the given maximum possible cost
// can proceed without exceeding the budget. This is called before every
// inference request.
//
// Returns nil if authorized, or an error describing why the request is rejected.
// See Architecture Section 21.3 point 4.
func (e *Enforcer) PreAuthorize(maxCostUSD float64) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.paused {
		return fmt.Errorf("budget exhausted: execution paused pending user action")
	}

	if e.maxUSD <= 0 {
		return nil // No budget limit set
	}

	currentSpend, err := e.db.GetProjectCost()
	if err != nil {
		return fmt.Errorf("check budget: %w", err)
	}

	remaining := e.maxUSD - currentSpend

	// Check if the maximum possible cost exceeds remaining budget.
	if maxCostUSD > remaining {
		return fmt.Errorf("budget exceeded: request max cost $%.4f, remaining $%.4f of $%.2f",
			maxCostUSD, remaining, e.maxUSD)
	}

	// Check warning threshold.
	pct := (currentSpend / e.maxUSD) * 100
	if pct >= e.warnAtPercent && !e.warningEmitted {
		e.warningEmitted = true
		e.emitter.Emit(events.Event{
			Type: events.EventBudgetWarning,
			Details: map[string]interface{}{
				"spent_usd":     currentSpend,
				"max_usd":       e.maxUSD,
				"percent":       pct,
				"warn_at":       e.warnAtPercent,
			},
		})
	}

	return nil
}

// RecordAndCheck records a completed inference cost and checks if the budget
// is now exhausted. If exhausted, emits budget_exhausted event and pauses.
// See Architecture Section 21.3 point 5.
func (e *Enforcer) RecordAndCheck(costUSD float64) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.maxUSD <= 0 {
		return
	}

	currentSpend, err := e.db.GetProjectCost()
	if err != nil {
		return
	}

	remaining := e.maxUSD - currentSpend

	if remaining <= 0 && !e.exhaustEmitted {
		e.exhaustEmitted = true
		e.paused = true
		e.emitter.Emit(events.Event{
			Type: events.EventBudgetExhausted,
			Details: map[string]interface{}{
				"spent_usd": currentSpend,
				"max_usd":   e.maxUSD,
			},
		})
	}
}

// IsPaused returns true if the budget is exhausted and execution is paused.
func (e *Enforcer) IsPaused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.paused
}

// IncreaseBudget increases the budget ceiling and resumes execution.
// See Architecture Section 21.3 point 5 and Section 30.3 point 4.
func (e *Enforcer) IncreaseBudget(newMaxUSD float64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.maxUSD = newMaxUSD
	e.paused = false
	e.exhaustEmitted = false
	e.warningEmitted = false
}

// MaxBudget returns the current budget ceiling.
func (e *Enforcer) MaxBudget() float64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.maxUSD
}

// IsExternalMode returns true if the orchestrator is in external client mode.
func (e *Enforcer) IsExternalMode() bool {
	return e.externalMode
}
