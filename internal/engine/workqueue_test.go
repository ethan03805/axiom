package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

func setupTestWorkQueue(t *testing.T) (*WorkQueue, *state.DB) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-wq-test-*")
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
	wq := NewWorkQueue(db, emitter, 10)

	return wq, db
}

func TestGetDispatchableNoDeps(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	// Create two queued tasks with no dependencies.
	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "queued", Tier: "standard", TaskType: "implementation"})

	tasks, err := wq.GetDispatchable()
	if err != nil {
		t.Fatalf("get dispatchable: %v", err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 dispatchable tasks, got %d", len(tasks))
	}
}

func TestGetDispatchableWithDeps(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	// t1 (queued, no deps), t2 (queued, depends on t1)
	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.AddTaskDependency("t2", "t1")

	tasks, err := wq.GetDispatchable()
	if err != nil {
		t.Fatalf("get dispatchable: %v", err)
	}
	// Only t1 should be dispatchable (t2 depends on t1 which is not done).
	if len(tasks) != 1 {
		t.Fatalf("expected 1 dispatchable, got %d", len(tasks))
	}
	if tasks[0].Task.ID != "t1" {
		t.Errorf("expected t1, got %s", tasks[0].Task.ID)
	}
}

func TestGetDispatchableRespectsLocks(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	// t1 and t2 both target main.go
	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.AddTaskTargetFile("t1", "main.go", "file")
	db.AddTaskTargetFile("t2", "main.go", "file")

	// Acquire lock for t1 (simulate it being dispatched).
	db.AcquireLocks("t1", []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}})

	tasks, err := wq.GetDispatchable()
	if err != nil {
		t.Fatalf("get dispatchable: %v", err)
	}
	// t1 holds its own lock on main.go. CheckAllLocksAvailable is now task-aware:
	// self-held locks are treated as available so a re-queued task can be
	// re-dispatched (BUG-043 fix). t2 is blocked by t1's lock on main.go.
	if len(tasks) != 1 {
		t.Fatalf("expected 1 dispatchable (t1 with self-held lock), got %d", len(tasks))
	}
	if tasks[0].Task.ID != "t1" {
		t.Errorf("expected t1 to be dispatchable with self-held lock, got %s", tasks[0].Task.ID)
	}

	// Now test that a lock held by a DIFFERENT task blocks correctly.
	// Lock main.go to a different task (t3).
	db.ReleaseLocks("t1")
	db.CreateTask(&state.Task{ID: "t3", Title: "Task 3", Status: "in_progress", Tier: "standard", TaskType: "implementation"})
	db.AcquireLocks("t3", []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}})

	tasks, err = wq.GetDispatchable()
	if err != nil {
		t.Fatalf("get dispatchable: %v", err)
	}

	// t1 should be dispatchable (its target file main.go is locked but by t3,
	// and t1 also targets main.go, so t1 should be blocked).
	// Actually t1 targets main.go, and main.go is locked by t3,
	// so t1 should NOT be dispatchable. Only tasks with no lock conflicts.
	// Let's check which tasks have target files...

	// t1 targets main.go (added above), t2 targets main.go
	// t3 holds lock on main.go
	// So neither t1 nor t2 should be dispatchable.
	if len(tasks) != 0 {
		t.Errorf("expected 0 dispatchable (both blocked by t3 lock), got %d", len(tasks))
	}
}

func TestGetDispatchableConcurrencyLimit(t *testing.T) {
	wq, db := setupTestWorkQueue(t)
	wq.maxMeeseeks = 1
	wq.activeCount = 1 // Already at limit.

	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})

	tasks, err := wq.GetDispatchable()
	if err != nil {
		t.Fatalf("get dispatchable: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 dispatchable at concurrency limit, got %d", len(tasks))
	}
}

func TestAcquireAndDispatch(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.AddTaskTargetFile("t1", "src/main.go", "file")

	locks := []state.LockRequest{{ResourceType: "file", ResourceKey: "src/main.go"}}

	ok, err := wq.AcquireAndDispatch("t1", locks)
	if err != nil {
		t.Fatalf("acquire and dispatch: %v", err)
	}
	if !ok {
		t.Error("expected successful dispatch")
	}

	// Verify task is now in_progress.
	task, _ := db.GetTask("t1")
	if task.Status != "in_progress" {
		t.Errorf("expected in_progress, got %s", task.Status)
	}

	// Verify lock is held.
	locked, holder, _ := db.IsLocked("file", "src/main.go")
	if !locked || holder != "t1" {
		t.Errorf("expected lock held by t1, locked=%v holder=%s", locked, holder)
	}

	// Verify active count incremented.
	if wq.ActiveCount() != 1 {
		t.Errorf("expected active count 1, got %d", wq.ActiveCount())
	}
}

func TestAcquireAndDispatchLockConflict(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "in_progress", Tier: "standard", TaskType: "implementation"})

	// t2 holds the lock.
	db.AcquireLocks("t2", []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}})

	// t1 tries to acquire the same lock.
	locks := []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}}
	ok, err := wq.AcquireAndDispatch("t1", locks)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if ok {
		t.Error("expected dispatch failure due to lock conflict")
	}

	// Task should still be queued.
	task, _ := db.GetTask("t1")
	if task.Status != "queued" {
		t.Errorf("expected queued, got %s", task.Status)
	}
}

func TestCompleteTaskReleasesLocks(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	locks := []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}}

	wq.AcquireAndDispatch("t1", locks)

	// Complete the task.
	if err := wq.CompleteTask("t1"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// Lock should be released.
	locked, _, _ := db.IsLocked("file", "main.go")
	if locked {
		t.Error("expected lock to be released after completion")
	}

	// Active count should decrement.
	if wq.ActiveCount() != 0 {
		t.Errorf("expected active count 0, got %d", wq.ActiveCount())
	}
}

func TestCompleteTaskRequeuesWaitingTasks(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	// t1 is in_progress holding a lock.
	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "in_progress", Tier: "standard", TaskType: "implementation"})
	db.AcquireLocks("t1", []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}})
	wq.mu.Lock()
	wq.activeCount = 1
	wq.mu.Unlock()

	// t2 is waiting_on_lock, blocked by t1.
	db.CreateTask(&state.Task{
		ID:          "t2",
		Title:       "Task 2",
		Status:      "in_progress", // Must go through valid transition
		Tier:        "standard",
		TaskType:    "implementation",
	})
	db.SetTaskWaitingOnLock("t2", "t1", []string{"main.go"})

	// Complete t1.
	if err := wq.CompleteTask("t1"); err != nil {
		t.Fatalf("complete: %v", err)
	}

	// t2 should now be queued.
	task2, _ := db.GetTask("t2")
	if task2.Status != "queued" {
		t.Errorf("expected t2 to be queued after unblock, got %s", task2.Status)
	}
}

func TestFailTaskReleasesLocks(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	locks := []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}}
	wq.AcquireAndDispatch("t1", locks)

	if err := wq.FailTask("t1"); err != nil {
		t.Fatalf("fail: %v", err)
	}

	locked, _, _ := db.IsLocked("file", "main.go")
	if locked {
		t.Error("expected lock to be released after failure")
	}
	if wq.ActiveCount() != 0 {
		t.Errorf("expected active count 0, got %d", wq.ActiveCount())
	}
}

func TestDependencySatisfiedUnblocksTask(t *testing.T) {
	wq, db := setupTestWorkQueue(t)

	// t1 (queued), t2 depends on t1 (queued)
	db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.AddTaskDependency("t2", "t1")

	// Initially only t1 is dispatchable.
	tasks, _ := wq.GetDispatchable()
	if len(tasks) != 1 || tasks[0].Task.ID != "t1" {
		t.Fatalf("expected only t1 dispatchable initially")
	}

	// Dispatch and complete t1.
	wq.AcquireAndDispatch("t1", nil)
	db.UpdateTaskStatus("t1", state.TaskStatusInReview)
	db.UpdateTaskStatus("t1", state.TaskStatusDone)
	wq.CompleteTask("t1")

	// Now t2 should be dispatchable.
	tasks, _ = wq.GetDispatchable()
	if len(tasks) != 1 || tasks[0].Task.ID != "t2" {
		ids := make([]string, len(tasks))
		for i, t := range tasks {
			ids[i] = t.Task.ID
		}
		t.Errorf("expected t2 dispatchable after t1 done, got %v", ids)
	}
}
