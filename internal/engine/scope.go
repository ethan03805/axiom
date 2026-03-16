package engine

import (
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

// ScopeExpansionHandler processes request_scope_expansion IPC messages from
// Meeseeks containers. When a Meeseeks discovers it needs to modify files
// outside its originally declared scope, it sends this request.
//
// The handler checks lock availability and either:
// - Acquires the additional locks and approves the expansion
// - Returns a waiting_on_lock response (the engine will destroy the container
//   and re-queue the task when locks become available)
//
// See Architecture Section 10.7.
type ScopeExpansionHandler struct {
	db      *state.DB
	emitter *events.Emitter
}

// NewScopeExpansionHandler creates a ScopeExpansionHandler.
func NewScopeExpansionHandler(db *state.DB, emitter *events.Emitter) *ScopeExpansionHandler {
	return &ScopeExpansionHandler{
		db:      db,
		emitter: emitter,
	}
}

// HandleScopeExpansion processes a scope expansion IPC request.
// Designed to be registered with the IPC Dispatcher.
//
// Returns a ScopeExpansionResponseMessage indicating approval, denial, or
// lock conflict. On lock conflict, the response includes the blocking task ID.
// The engine (not this handler) is responsible for destroying the container
// and re-queuing the task on lock conflict.
func (h *ScopeExpansionHandler) HandleScopeExpansion(taskID string, msg interface{}, raw []byte) (interface{}, error) {
	reqMsg, ok := msg.(*ipc.ScopeExpansionRequestMessage)
	if !ok {
		return &ipc.ScopeExpansionResponseMessage{
			Header:  ipc.Header{Type: ipc.TypeScopeExpansionResponse, TaskID: taskID},
			Status:  "denied",
			Message: "invalid message type",
		}, nil
	}

	// Build lock requests for the additional files.
	// Default to file-level locks for expanded scope.
	lockReqs := make([]state.LockRequest, len(reqMsg.AdditionalFiles))
	for i, f := range reqMsg.AdditionalFiles {
		lockReqs[i] = state.LockRequest{
			ResourceType: "file",
			ResourceKey:  f,
		}
	}

	// Check if all additional locks are available.
	allAvailable, blockedBy, err := h.db.CheckAllLocksAvailable(lockReqs)
	if err != nil {
		return &ipc.ScopeExpansionResponseMessage{
			Header:  ipc.Header{Type: ipc.TypeScopeExpansionResponse, TaskID: taskID},
			Status:  "denied",
			Message: "error checking lock availability: " + err.Error(),
		}, nil
	}

	if !allAvailable {
		// Lock conflict: respond with waiting_on_lock.
		// The engine will destroy this container and re-queue the task.
		// See Architecture Section 10.7 (Lock Conflict During Scope Expansion).
		h.emitter.Emit(events.Event{
			Type:   events.EventScopeExpansionDenied,
			TaskID: taskID,
			Details: map[string]interface{}{
				"reason":     "lock_conflict",
				"blocked_by": blockedBy,
				"files":      reqMsg.AdditionalFiles,
			},
		})

		return &ipc.ScopeExpansionResponseMessage{
			Header:    ipc.Header{Type: ipc.TypeScopeExpansionResponse, TaskID: taskID},
			Status:    "waiting_on_lock",
			BlockedBy: blockedBy,
			Message:   "Container will be destroyed and task re-queued when locks are available",
		}, nil
	}

	// All locks available. Acquire them.
	if err := h.db.AcquireLocks(taskID, lockReqs); err != nil {
		// Race condition: another task grabbed the lock between check and acquire.
		// Treat as lock conflict.
		return &ipc.ScopeExpansionResponseMessage{
			Header:  ipc.Header{Type: ipc.TypeScopeExpansionResponse, TaskID: taskID},
			Status:  "waiting_on_lock",
			Message: "Lock acquisition race: " + err.Error(),
		}, nil
	}

	// Update task_target_files with the expanded scope.
	for _, f := range reqMsg.AdditionalFiles {
		if err := h.db.AddTaskTargetFile(taskID, f, "file"); err != nil {
			// Non-fatal: the lock is acquired, target file tracking is best-effort.
			continue
		}
	}

	// Log the scope expansion.
	h.emitter.Emit(events.Event{
		Type:   events.EventScopeExpansionApproved,
		TaskID: taskID,
		Details: map[string]interface{}{
			"expanded_files": reqMsg.AdditionalFiles,
			"reason":         reqMsg.Reason,
		},
	})

	return &ipc.ScopeExpansionResponseMessage{
		Header:        ipc.Header{Type: ipc.TypeScopeExpansionResponse, TaskID: taskID},
		Status:        "approved",
		ExpandedFiles: reqMsg.AdditionalFiles,
		LocksAcquired: true,
	}, nil
}
