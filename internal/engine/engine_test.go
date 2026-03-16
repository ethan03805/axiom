package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethan03805/axiom/internal/state"
)

func setupTestEngine(t *testing.T) *Engine {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-engine-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Create the .axiom directory for the DB
	axiomDir := filepath.Join(tmpDir, ".axiom")
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		t.Fatalf("create axiom dir: %v", err)
	}

	// Change to temp dir so engine finds .axiom/axiom.db
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	cfg := DefaultConfig()
	eng, err := New(cfg)
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}

	return eng
}

func TestEngineStartStop(t *testing.T) {
	eng := setupTestEngine(t)

	if err := eng.Start(); err != nil {
		t.Fatalf("start engine: %v", err)
	}

	// Starting again should fail
	if err := eng.Start(); err == nil {
		t.Error("expected error starting already-running engine")
	}

	if err := eng.Stop(); err != nil {
		t.Fatalf("stop engine: %v", err)
	}

	// Stopping again should fail
	if err := eng.Stop(); err == nil {
		t.Error("expected error stopping already-stopped engine")
	}
}

func TestCrashRecovery(t *testing.T) {
	eng := setupTestEngine(t)

	db := eng.DB()

	// Create tasks in various states to simulate a crash
	tasks := []*state.Task{
		{ID: "crash-1", Title: "In Progress Task", Status: "in_progress", Tier: "tier-1", TaskType: "implementation"},
		{ID: "crash-2", Title: "In Review Task", Status: "in_review", Tier: "tier-1", TaskType: "implementation"},
		{ID: "crash-3", Title: "Queued Task", Status: "queued", Tier: "tier-1", TaskType: "implementation"},
		{ID: "crash-4", Title: "Done Task", Status: "done", Tier: "tier-1", TaskType: "implementation"},
	}

	for _, task := range tasks {
		if err := db.CreateTask(task); err != nil {
			t.Fatalf("create task %s: %v", task.ID, err)
		}
	}

	// Acquire some locks
	if err := db.AcquireLocks("crash-1", []state.LockRequest{
		{ResourceType: "file", ResourceKey: "main.go"},
	}); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	// Run crash recovery
	if err := eng.CrashRecovery(); err != nil {
		t.Fatalf("crash recovery: %v", err)
	}

	// Verify in_progress was reset to queued
	t1, err := db.GetTask("crash-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if t1.Status != "queued" {
		t.Errorf("expected crash-1 to be queued after recovery, got %s", t1.Status)
	}

	// Verify in_review was reset to queued
	t2, err := db.GetTask("crash-2")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if t2.Status != "queued" {
		t.Errorf("expected crash-2 to be queued after recovery, got %s", t2.Status)
	}

	// Verify queued stayed queued
	t3, err := db.GetTask("crash-3")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if t3.Status != "queued" {
		t.Errorf("expected crash-3 to stay queued, got %s", t3.Status)
	}

	// Verify done stayed done
	t4, err := db.GetTask("crash-4")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if t4.Status != "done" {
		t.Errorf("expected crash-4 to stay done, got %s", t4.Status)
	}

	// Verify locks were released
	locked, _, err := db.IsLocked("file", "main.go")
	if err != nil {
		t.Fatalf("check lock: %v", err)
	}
	if locked {
		t.Error("expected lock to be released after crash recovery")
	}
}
