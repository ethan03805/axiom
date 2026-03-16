package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

func setupTestScope(t *testing.T) (*ScopeExpansionHandler, *state.DB) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-scope-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		t.Fatalf("create axiom dir: %v", err)
	}

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	emitter := events.NewEmitter()
	handler := NewScopeExpansionHandler(db, emitter)

	return handler, db
}

func TestScopeExpansionApproved(t *testing.T) {
	handler, db := setupTestScope(t)

	// Create the requesting task.
	db.CreateTask(&state.Task{
		ID: "task-042", Title: "Auth Handler", Status: "in_progress",
		Tier: "standard", TaskType: "implementation",
	})

	// Build the IPC message matching Architecture Section 10.7 example.
	reqMsg := &ipc.ScopeExpansionRequestMessage{
		Header:          ipc.Header{Type: ipc.TypeScopeExpansionRequest, TaskID: "task-042"},
		AdditionalFiles: []string{"src/routes/api.go", "src/middleware/cors.go"},
		Reason:          "Need to update API route registration to match new handler signature",
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, err := handler.HandleScopeExpansion("task-042", reqMsg, raw)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	respMsg, ok := resp.(*ipc.ScopeExpansionResponseMessage)
	if !ok {
		t.Fatalf("expected ScopeExpansionResponseMessage, got %T", resp)
	}

	if respMsg.Status != "approved" {
		t.Errorf("status = %s, want approved", respMsg.Status)
	}
	if !respMsg.LocksAcquired {
		t.Error("expected locks_acquired = true")
	}
	if len(respMsg.ExpandedFiles) != 2 {
		t.Errorf("expected 2 expanded files, got %d", len(respMsg.ExpandedFiles))
	}

	// Verify locks were actually acquired.
	locked, holder, _ := db.IsLocked("file", "src/routes/api.go")
	if !locked || holder != "task-042" {
		t.Errorf("expected lock on src/routes/api.go by task-042, locked=%v holder=%s", locked, holder)
	}
	locked, holder, _ = db.IsLocked("file", "src/middleware/cors.go")
	if !locked || holder != "task-042" {
		t.Errorf("expected lock on src/middleware/cors.go by task-042, locked=%v holder=%s", locked, holder)
	}

	// Verify target files were updated.
	files, _ := db.GetTaskTargetFiles("task-042")
	if len(files) != 2 {
		t.Errorf("expected 2 target files, got %d", len(files))
	}
}

func TestScopeExpansionLockConflict(t *testing.T) {
	handler, db := setupTestScope(t)

	// Create two tasks. task-038 holds a lock on api.go.
	db.CreateTask(&state.Task{
		ID: "task-038", Title: "API Routes", Status: "in_progress",
		Tier: "standard", TaskType: "implementation",
	})
	db.CreateTask(&state.Task{
		ID: "task-042", Title: "Auth Handler", Status: "in_progress",
		Tier: "standard", TaskType: "implementation",
	})

	// task-038 holds the lock.
	db.AcquireLocks("task-038", []state.LockRequest{
		{ResourceType: "file", ResourceKey: "src/routes/api.go"},
	})

	// task-042 requests expansion to the locked file.
	reqMsg := &ipc.ScopeExpansionRequestMessage{
		Header:          ipc.Header{Type: ipc.TypeScopeExpansionRequest, TaskID: "task-042"},
		AdditionalFiles: []string{"src/routes/api.go", "src/middleware/cors.go"},
		Reason:          "Need to update API route registration",
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, err := handler.HandleScopeExpansion("task-042", reqMsg, raw)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	respMsg := resp.(*ipc.ScopeExpansionResponseMessage)

	// Should be waiting_on_lock with blocked_by = task-038.
	if respMsg.Status != "waiting_on_lock" {
		t.Errorf("status = %s, want waiting_on_lock", respMsg.Status)
	}
	if respMsg.BlockedBy != "task-038" {
		t.Errorf("blocked_by = %s, want task-038", respMsg.BlockedBy)
	}

	// Verify NO additional locks were acquired by task-042.
	locked, holder, _ := db.IsLocked("file", "src/routes/api.go")
	if holder == "task-042" {
		t.Error("task-042 should NOT hold the lock on conflict")
	}
	if !locked || holder != "task-038" {
		t.Errorf("task-038 should still hold the lock, locked=%v holder=%s", locked, holder)
	}

	// cors.go should also NOT be locked (atomic - no partial acquisition).
	locked, _, _ = db.IsLocked("file", "src/middleware/cors.go")
	if locked {
		t.Error("cors.go should not be locked on conflict (no partial lock)")
	}
}

func TestScopeExpansionEventEmission(t *testing.T) {
	handler, db := setupTestScope(t)

	db.CreateTask(&state.Task{
		ID: "task-evt", Title: "Event Test", Status: "in_progress",
		Tier: "standard", TaskType: "implementation",
	})

	var approvedEvent bool
	handler.emitter.Subscribe(events.EventScopeExpansionApproved, func(e events.Event) {
		approvedEvent = true
	})

	reqMsg := &ipc.ScopeExpansionRequestMessage{
		Header:          ipc.Header{Type: ipc.TypeScopeExpansionRequest, TaskID: "task-evt"},
		AdditionalFiles: []string{"src/utils.go"},
		Reason:          "Need utility function",
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	handler.HandleScopeExpansion("task-evt", reqMsg, raw)

	time.Sleep(100 * time.Millisecond)
	if !approvedEvent {
		t.Error("expected scope_expansion_approved event")
	}
}
