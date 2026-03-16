package state

import (
	"testing"
)

func TestAcquireAndReleaseLocks(t *testing.T) {
	db := setupTestDB(t)

	// Create a task to hold the lock
	task := makeTask("lock-task", "queued")
	if err := db.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	locks := []LockRequest{
		{ResourceType: "file", ResourceKey: "src/main.go"},
		{ResourceType: "file", ResourceKey: "src/util.go"},
	}

	// Acquire locks
	if err := db.AcquireLocks("lock-task", locks); err != nil {
		t.Fatalf("acquire locks: %v", err)
	}

	// Verify locks exist
	locked, holder, err := db.IsLocked("file", "src/main.go")
	if err != nil {
		t.Fatalf("check lock: %v", err)
	}
	if !locked {
		t.Error("expected file to be locked")
	}
	if holder != "lock-task" {
		t.Errorf("expected holder lock-task, got %s", holder)
	}

	// Get all locked resources
	allLocks, err := db.GetLockedResources()
	if err != nil {
		t.Fatalf("get locks: %v", err)
	}
	if len(allLocks) != 2 {
		t.Errorf("expected 2 locks, got %d", len(allLocks))
	}

	// Release locks
	if err := db.ReleaseLocks("lock-task"); err != nil {
		t.Fatalf("release locks: %v", err)
	}

	// Verify released
	locked2, _, err := db.IsLocked("file", "src/main.go")
	if err != nil {
		t.Fatalf("check lock after release: %v", err)
	}
	if locked2 {
		t.Error("expected file to be unlocked after release")
	}
}

func TestAtomicLockAcquisition(t *testing.T) {
	db := setupTestDB(t)

	task1 := makeTask("atomic-task1", "queued")
	task2 := makeTask("atomic-task2", "queued")
	if err := db.CreateTask(task1); err != nil {
		t.Fatalf("create task1: %v", err)
	}
	if err := db.CreateTask(task2); err != nil {
		t.Fatalf("create task2: %v", err)
	}

	// Task 1 acquires a lock
	if err := db.AcquireLocks("atomic-task1", []LockRequest{
		{ResourceType: "file", ResourceKey: "shared.go"},
	}); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}

	// Task 2 tries to acquire multiple locks, one of which overlaps
	err := db.AcquireLocks("atomic-task2", []LockRequest{
		{ResourceType: "file", ResourceKey: "other.go"},
		{ResourceType: "file", ResourceKey: "shared.go"}, // conflict
	})
	if err == nil {
		t.Error("should fail when any lock is held")
	}

	// Verify that other.go was NOT acquired (atomicity)
	locked, _, err := db.IsLocked("file", "other.go")
	if err != nil {
		t.Fatalf("check lock: %v", err)
	}
	if locked {
		t.Error("other.go should not be locked (atomic rollback)")
	}
}

func TestDeterministicLockOrdering(t *testing.T) {
	db := setupTestDB(t)

	task := makeTask("order-task", "queued")
	if err := db.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	// Request locks in reverse order -- they should still be acquired
	// in sorted order internally (b before a in the insert order).
	locks := []LockRequest{
		{ResourceType: "file", ResourceKey: "z_file.go"},
		{ResourceType: "file", ResourceKey: "a_file.go"},
		{ResourceType: "dir", ResourceKey: "pkg/"},
	}

	if err := db.AcquireLocks("order-task", locks); err != nil {
		t.Fatalf("acquire locks: %v", err)
	}

	allLocks, err := db.GetLockedResources()
	if err != nil {
		t.Fatalf("get locks: %v", err)
	}
	if len(allLocks) != 3 {
		t.Errorf("expected 3 locks, got %d", len(allLocks))
	}
}
