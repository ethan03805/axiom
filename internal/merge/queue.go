// Package merge implements the serialized merge queue that processes approved
// Meeseeks output one at a time, validates against HEAD, runs integration
// checks, and commits.
//
// See Architecture.md Section 16.4 for the full specification.
package merge

import (
	"fmt"
	"sync"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/git"
)

// MergeItem represents a single approved task output submitted to the queue.
type MergeItem struct {
	TaskID       string
	TaskTitle    string
	StagingDir   string            // Path to staged output files
	BaseSnapshot string            // Git SHA this output was built against
	Files        map[string]string // relPath -> content (for files to write)
	Deletions    []string          // relPaths to delete
	SRSRefs      []string
	MeeseeksModel string
	ReviewerModel string
	AttemptNumber int
	MaxAttempts   int
	CostUSD      float64
}

// MergeResult holds the outcome of processing a merge item.
type MergeResult struct {
	Success      bool
	CommitSHA    string
	Error        string
	NeedsRequeue bool   // True if the task should be re-queued with fresh context
	ChangedFiles []string // Files changed since base snapshot (for context refresh)
}

// ValidateFunc runs integration checks against the merged state.
// Returns nil if all checks pass, or an error describing the failure.
type ValidateFunc func(taskID string) error

// ReindexFunc triggers a semantic index refresh for changed files.
type ReindexFunc func(changedFiles []string) error

// Queue serializes commits to prevent stale-context conflicts.
// Meeseeks execute in parallel, but commits are serialized through this queue.
// See Architecture Section 16.4.
type Queue struct {
	mu       sync.Mutex
	gitMgr   *git.Manager
	snapshot *git.SnapshotManager
	emitter  *events.Emitter

	// Callbacks for integration checks and re-indexing.
	// Injected by the engine to wire in the validation sandbox and indexer.
	ValidateFn ValidateFunc
	ReindexFn  ReindexFunc

	items    []*MergeItem
	processing bool
}

// NewQueue creates a merge queue.
func NewQueue(gitMgr *git.Manager, emitter *events.Emitter) *Queue {
	return &Queue{
		gitMgr:   gitMgr,
		snapshot: git.NewSnapshotManager(gitMgr),
		emitter:  emitter,
	}
}

// Submit adds an approved item to the merge queue for processing.
func (q *Queue) Submit(item *MergeItem) {
	q.mu.Lock()
	q.items = append(q.items, item)
	q.mu.Unlock()

	q.emitter.Emit(events.Event{
		Type:   events.EventMergeStarted,
		TaskID: item.TaskID,
		Details: map[string]interface{}{
			"base_snapshot": item.BaseSnapshot,
			"queue_depth":   q.Depth(),
		},
	})
}

// ProcessNext processes the next item in the queue.
// Returns the result and whether there was an item to process.
// Items are processed one at a time (serialized).
// See Architecture Section 16.4 for the 10-step process.
func (q *Queue) ProcessNext() (*MergeResult, bool) {
	q.mu.Lock()
	if len(q.items) == 0 || q.processing {
		q.mu.Unlock()
		return nil, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	q.processing = true
	q.mu.Unlock()

	result := q.processItem(item)

	q.mu.Lock()
	q.processing = false
	q.mu.Unlock()

	return result, true
}

// processItem executes the 10-step merge process from Architecture Section 16.4.
func (q *Queue) processItem(item *MergeItem) *MergeResult {
	result := &MergeResult{}

	// Step 1: Receive approved output (already done via Submit).

	// Step 2: Validate base_snapshot against current HEAD.
	status, err := q.snapshot.ValidateSnapshot(item.BaseSnapshot)
	if err != nil {
		result.Error = fmt.Sprintf("snapshot validation: %v", err)
		result.NeedsRequeue = true
		return result
	}

	// Step 3: If stale, attempt merge or re-queue.
	if status == git.SnapshotStale {
		// The output was built against an older commit. Since we can't
		// do a true three-way merge of the Meeseeks output (it's not a
		// git branch), we re-queue the task with updated context.
		changed, _ := q.snapshot.ChangedFilesSince(item.BaseSnapshot)
		result.NeedsRequeue = true
		result.ChangedFiles = changed
		result.Error = fmt.Sprintf("base snapshot stale: HEAD has advanced since %s (%d files changed)",
			item.BaseSnapshot[:minLen(len(item.BaseSnapshot), 7)], len(changed))

		q.emitter.Emit(events.Event{
			Type:   events.EventMergeCompleted,
			TaskID: item.TaskID,
			Details: map[string]interface{}{
				"result":        "requeue_stale",
				"changed_files": len(changed),
			},
		})
		return result
	}

	if status == git.SnapshotDiverged {
		result.NeedsRequeue = true
		result.Error = "base snapshot diverged from HEAD"
		return result
	}

	// Step 4: Apply Meeseeks output to working copy of HEAD.
	// Save current HEAD so we can revert on failure.
	headBefore, err := q.gitMgr.HeadSHA()
	if err != nil {
		result.Error = fmt.Sprintf("get HEAD: %v", err)
		return result
	}

	// Apply file writes.
	if len(item.Files) > 0 {
		if err := q.gitMgr.ApplyFiles(item.Files); err != nil {
			result.Error = fmt.Sprintf("apply files: %v", err)
			q.gitMgr.ResetHard(headBefore)
			result.NeedsRequeue = true
			return result
		}
	}

	// Apply deletions.
	if len(item.Deletions) > 0 {
		if err := q.gitMgr.DeleteFiles(item.Deletions); err != nil {
			result.Error = fmt.Sprintf("delete files: %v", err)
			q.gitMgr.ResetHard(headBefore)
			result.NeedsRequeue = true
			return result
		}
	}

	// Step 5: Run integration checks in validation sandbox.
	if q.ValidateFn != nil {
		if err := q.ValidateFn(item.TaskID); err != nil {
			// Step 6: Integration fails -> revert, re-queue.
			q.gitMgr.ResetHard(headBefore)
			result.NeedsRequeue = true
			result.Error = fmt.Sprintf("integration check failed: %v", err)

			q.emitter.Emit(events.Event{
				Type:   events.EventMergeCompleted,
				TaskID: item.TaskID,
				Details: map[string]interface{}{
					"result": "requeue_integration_failure",
					"error":  err.Error(),
				},
			})
			return result
		}
	}

	// Step 7: Integration passes -> commit, update HEAD.
	commitSHA, err := q.gitMgr.Commit(&git.CommitMetadata{
		TaskID:        item.TaskID,
		TaskTitle:     item.TaskTitle,
		SRSRefs:       item.SRSRefs,
		MeeseeksModel: item.MeeseeksModel,
		ReviewerModel: item.ReviewerModel,
		AttemptNumber: item.AttemptNumber,
		MaxAttempts:   item.MaxAttempts,
		CostUSD:       item.CostUSD,
		BaseSnapshot:  item.BaseSnapshot,
	})
	if err != nil {
		q.gitMgr.ResetHard(headBefore)
		result.Error = fmt.Sprintf("commit: %v", err)
		result.NeedsRequeue = true
		return result
	}

	// Step 8: Re-index via semantic indexer.
	if q.ReindexFn != nil {
		changedFiles := make([]string, 0, len(item.Files)+len(item.Deletions))
		for path := range item.Files {
			changedFiles = append(changedFiles, path)
		}
		changedFiles = append(changedFiles, item.Deletions...)
		_ = q.ReindexFn(changedFiles) // Best-effort; don't fail the merge
	}

	// Steps 9-10 (release locks, unblock tasks) are handled by the engine's
	// WorkQueue.CompleteTask(), not by the merge queue directly.

	result.Success = true
	result.CommitSHA = commitSHA

	q.emitter.Emit(events.Event{
		Type:   events.EventMergeCompleted,
		TaskID: item.TaskID,
		Details: map[string]interface{}{
			"result":     "committed",
			"commit_sha": commitSHA,
		},
	})

	return result
}

// Depth returns the number of items waiting in the queue.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// IsProcessing returns true if the queue is currently processing an item.
func (q *Queue) IsProcessing() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.processing
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}
