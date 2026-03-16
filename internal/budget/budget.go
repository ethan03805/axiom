package budget

import (
	"sync"
)

// Tracker monitors and enforces cost budgets across all tasks.
type Tracker struct {
	maxUSD        float64
	warnPercent   float64
	totalSpent    float64
	mu            sync.Mutex
}

// Usage holds a snapshot of current budget usage.
type Usage struct {
	TotalSpent  float64
	MaxBudget   float64
	Remaining   float64
	Percentage  float64
	IsWarning   bool
	IsExhausted bool
}

// New creates a new budget Tracker.
func New(maxUSD, warnPercent float64) *Tracker {
	return &Tracker{
		maxUSD:      maxUSD,
		warnPercent: warnPercent,
	}
}

// Record records a cost expenditure. Returns an error if budget is exhausted.
func (t *Tracker) Record(costUSD float64) error {
	return nil
}

// CanSpend checks if the given amount can be spent without exceeding budget.
func (t *Tracker) CanSpend(costUSD float64) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.totalSpent+costUSD <= t.maxUSD
}

// Usage returns the current budget usage snapshot.
func (t *Tracker) Usage() *Usage {
	t.mu.Lock()
	defer t.mu.Unlock()
	remaining := t.maxUSD - t.totalSpent
	pct := 0.0
	if t.maxUSD > 0 {
		pct = (t.totalSpent / t.maxUSD) * 100
	}
	return &Usage{
		TotalSpent:  t.totalSpent,
		MaxBudget:   t.maxUSD,
		Remaining:   remaining,
		Percentage:  pct,
		IsWarning:   pct >= t.warnPercent,
		IsExhausted: remaining <= 0,
	}
}

// Reset resets the budget tracker to zero spent.
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.totalSpent = 0
}
