package merge

import (
	"context"
)

// Strategy defines the interface for merge strategies.
type Strategy interface {
	Name() string
	CanMerge(ctx context.Context, base, ours, theirs string) (bool, error)
	Merge(ctx context.Context, base, ours, theirs string) (*MergeResult, error)
}

// MergeResult holds the outcome of a merge operation.
type MergeResult struct {
	Content    string
	Conflicts  []Conflict
	HasConflict bool
}

// Conflict describes a merge conflict region.
type Conflict struct {
	StartLine int
	EndLine   int
	Ours      string
	Theirs    string
	Base      string
}

// Merger coordinates the merging of concurrent task outputs.
type Merger struct {
	strategies []Strategy
}

// New creates a new Merger.
func New() *Merger {
	return &Merger{}
}

// RegisterStrategy adds a merge strategy.
func (m *Merger) RegisterStrategy(s Strategy) {
	m.strategies = append(m.strategies, s)
}

// MergeFile merges changes from two branches into a base file.
func (m *Merger) MergeFile(ctx context.Context, base, ours, theirs string) (*MergeResult, error) {
	return nil, nil
}

// MergeTask merges all artifacts from a completed task back to the main branch.
func (m *Merger) MergeTask(ctx context.Context, taskID string) error {
	return nil
}
