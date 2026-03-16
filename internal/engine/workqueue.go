package engine

import (
	"fmt"
	"sync"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// WorkQueue coordinates task dispatch by combining dependency resolution,
// write-set lock availability, and concurrency limits. It identifies tasks
// that are ready to execute and can acquire their required locks.
//
// The work queue is the engine's core scheduling loop described in
// Architecture Section 5.1 step 7 and BUILD_PLAN step 5.3.
type WorkQueue struct {
	db          *state.DB
	emitter     *events.Emitter
	maxMeeseeks int

	mu          sync.Mutex
	activeCount int // Current number of dispatched (in-progress) agent tasks
}

// NewWorkQueue creates a WorkQueue with the given concurrency limit.
func NewWorkQueue(db *state.DB, emitter *events.Emitter, maxMeeseeks int) *WorkQueue {
	return &WorkQueue{
		db:          db,
		emitter:     emitter,
		maxMeeseeks: maxMeeseeks,
	}
}

// DispatchableTask is a task that is ready to execute: all dependencies are
// met and all required locks are available.
type DispatchableTask struct {
	Task  *state.Task
	Locks []state.LockRequest
}

// GetDispatchable returns tasks that can be dispatched right now:
// 1. Status is "queued"
// 2. All dependencies are "done"
// 3. All required write-set locks are available
// 4. Within the concurrency limit
//
// Tasks are returned in creation order (earliest first).
// See Architecture Section 5.1 step 7 and Section 16.3.
func (wq *WorkQueue) GetDispatchable() ([]*DispatchableTask, error) {
	wq.mu.Lock()
	available := wq.maxMeeseeks - wq.activeCount
	wq.mu.Unlock()

	if available <= 0 {
		return nil, nil
	}

	// Get tasks whose dependencies are all satisfied.
	readyTasks, err := wq.db.GetReadyTasks()
	if err != nil {
		return nil, fmt.Errorf("get ready tasks: %w", err)
	}

	var dispatchable []*DispatchableTask

	for _, task := range readyTasks {
		if len(dispatchable) >= available {
			break
		}

		// Build lock requests from the task's target files.
		locks, err := wq.db.BuildLocksFromTargetFiles(task.ID)
		if err != nil {
			continue // Skip tasks with lock-building errors
		}

		// If no target files, the task can be dispatched without locks.
		if len(locks) == 0 {
			dispatchable = append(dispatchable, &DispatchableTask{
				Task:  task,
				Locks: nil,
			})
			continue
		}

		// Check if all locks are available.
		allAvailable, _, err := wq.db.CheckAllLocksAvailable(locks)
		if err != nil {
			continue
		}
		if !allAvailable {
			continue // Skip tasks with lock conflicts
		}

		dispatchable = append(dispatchable, &DispatchableTask{
			Task:  task,
			Locks: locks,
		})
	}

	return dispatchable, nil
}

// AcquireAndDispatch attempts to acquire locks for a task and transition
// it to in_progress. This is atomic: if lock acquisition fails (race with
// another dispatch), the task remains queued.
//
// Returns true if the task was successfully dispatched.
func (wq *WorkQueue) AcquireAndDispatch(taskID string, locks []state.LockRequest) (bool, error) {
	// Try to acquire locks atomically.
	if len(locks) > 0 {
		if err := wq.db.AcquireLocks(taskID, locks); err != nil {
			// Lock conflict -- another task grabbed the lock between
			// GetDispatchable and now. Task stays queued.
			return false, nil
		}
	}

	// Transition to in_progress.
	if err := wq.db.UpdateTaskStatus(taskID, state.TaskStatusInProgress); err != nil {
		// Status transition failed (shouldn't happen for queued -> in_progress).
		// Release the locks we just acquired.
		if len(locks) > 0 {
			_ = wq.db.ReleaseLocks(taskID)
		}
		return false, fmt.Errorf("update task status: %w", err)
	}

	wq.mu.Lock()
	wq.activeCount++
	wq.mu.Unlock()

	wq.emitter.Emit(events.Event{
		Type:      events.EventTaskStarted,
		TaskID:    taskID,
		AgentType: "engine",
		Details: map[string]interface{}{
			"locks_acquired": len(locks),
		},
	})

	return true, nil
}

// CompleteTask handles the completion of a task: releases locks, updates
// active count, and re-queues any tasks that were waiting on this task's locks.
// See Architecture Section 10.7 (Lock Conflict During Scope Expansion).
func (wq *WorkQueue) CompleteTask(taskID string) error {
	// Release write-set locks.
	if err := wq.db.ReleaseLocks(taskID); err != nil {
		return fmt.Errorf("release locks for %s: %w", taskID, err)
	}

	wq.mu.Lock()
	if wq.activeCount > 0 {
		wq.activeCount--
	}
	wq.mu.Unlock()

	// Check for tasks that were waiting on locks held by this task.
	// Transition them back to queued so they can be dispatched.
	waitingTasks, err := wq.db.GetWaitingOnLockTasks(taskID)
	if err != nil {
		return fmt.Errorf("get waiting tasks: %w", err)
	}

	for _, wt := range waitingTasks {
		if err := wq.db.UpdateTaskStatus(wt.ID, state.TaskStatusQueued); err != nil {
			// Log but don't fail the completion.
			wq.emitter.Emit(events.Event{
				Type:   events.EventTaskFailed,
				TaskID: wt.ID,
				Details: map[string]interface{}{
					"error": fmt.Sprintf("failed to re-queue waiting task: %v", err),
				},
			})
			continue
		}
		wq.emitter.Emit(events.Event{
			Type:      events.EventTaskCreated,
			TaskID:    wt.ID,
			AgentType: "engine",
			Details: map[string]interface{}{
				"action":         "re-queued_from_waiting_on_lock",
				"unblocked_by":   taskID,
			},
		})
	}

	return nil
}

// FailTask handles a task failure: releases locks and updates active count.
func (wq *WorkQueue) FailTask(taskID string) error {
	if err := wq.db.ReleaseLocks(taskID); err != nil {
		return fmt.Errorf("release locks for %s: %w", taskID, err)
	}

	wq.mu.Lock()
	if wq.activeCount > 0 {
		wq.activeCount--
	}
	wq.mu.Unlock()

	return nil
}

// ActiveCount returns the number of currently dispatched tasks.
func (wq *WorkQueue) ActiveCount() int {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	return wq.activeCount
}

// SetActiveCount sets the active count. Used during crash recovery
// to reconcile with actual container state.
func (wq *WorkQueue) SetActiveCount(count int) {
	wq.mu.Lock()
	defer wq.mu.Unlock()
	wq.activeCount = count
}
