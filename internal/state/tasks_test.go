package state

import (
	"os"
	"testing"
	"time"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()
	tmpFile, err := os.CreateTemp("", "axiom-test-*.db")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	tmpFile.Close()
	t.Cleanup(func() { os.Remove(tmpFile.Name()) })

	db, err := NewDB(tmpFile.Name())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := db.RunMigrations(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	return db
}

func makeTask(id, status string) *Task {
	return &Task{
		ID:        id,
		Title:     "Task " + id,
		Status:    status,
		Tier:      "tier-1",
		TaskType:  "implementation",
		CreatedAt: time.Now(),
	}
}

func TestCreateAndGetTask(t *testing.T) {
	db := setupTestDB(t)

	task := makeTask("task-1", "queued")
	task.Description = "A test task"
	task.ParentID = ""

	if err := db.CreateTask(task); err != nil {
		t.Fatalf("create task: %v", err)
	}

	got, err := db.GetTask("task-1")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}

	if got.ID != "task-1" {
		t.Errorf("expected ID task-1, got %s", got.ID)
	}
	if got.Title != "Task task-1" {
		t.Errorf("expected title 'Task task-1', got %s", got.Title)
	}
	if got.Status != "queued" {
		t.Errorf("expected status queued, got %s", got.Status)
	}
	if got.Description != "A test task" {
		t.Errorf("expected description 'A test task', got %s", got.Description)
	}
}

func TestCreateTaskBatch(t *testing.T) {
	db := setupTestDB(t)

	tasks := []*Task{
		makeTask("batch-1", "queued"),
		makeTask("batch-2", "queued"),
		makeTask("batch-3", "queued"),
	}

	if err := db.CreateTaskBatch(tasks); err != nil {
		t.Fatalf("create batch: %v", err)
	}

	for _, task := range tasks {
		got, err := db.GetTask(task.ID)
		if err != nil {
			t.Errorf("get task %s: %v", task.ID, err)
		}
		if got.Title != task.Title {
			t.Errorf("expected title %s, got %s", task.Title, got.Title)
		}
	}
}

func TestUpdateTaskStatus_ValidTransitions(t *testing.T) {
	db := setupTestDB(t)

	transitions := []struct {
		from TaskStatus
		to   TaskStatus
	}{
		{TaskStatusQueued, TaskStatusInProgress},
		{TaskStatusInProgress, TaskStatusInReview},
		{TaskStatusInReview, TaskStatusDone},
	}

	for i, tt := range transitions {
		task := makeTask(
			"transition-"+string(rune('a'+i)),
			string(tt.from),
		)
		if err := db.CreateTask(task); err != nil {
			t.Fatalf("create task: %v", err)
		}

		if err := db.UpdateTaskStatus(task.ID, tt.to); err != nil {
			t.Errorf("transition %s -> %s should succeed, got: %v", tt.from, tt.to, err)
		}

		got, err := db.GetTask(task.ID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if got.Status != string(tt.to) {
			t.Errorf("expected status %s, got %s", tt.to, got.Status)
		}
	}

	// Test failed -> queued (retry)
	retryTask := makeTask("retry-task", "failed")
	if err := db.CreateTask(retryTask); err != nil {
		t.Fatalf("create retry task: %v", err)
	}
	if err := db.UpdateTaskStatus("retry-task", TaskStatusQueued); err != nil {
		t.Errorf("failed -> queued should succeed: %v", err)
	}

	// Test cancelled_eco from active state
	ecoTask := makeTask("eco-task", "queued")
	if err := db.CreateTask(ecoTask); err != nil {
		t.Fatalf("create eco task: %v", err)
	}
	if err := db.UpdateTaskStatus("eco-task", TaskStatusCancelledECO); err != nil {
		t.Errorf("queued -> cancelled_eco should succeed: %v", err)
	}
}

func TestUpdateTaskStatus_InvalidTransitions(t *testing.T) {
	db := setupTestDB(t)

	invalid := []struct {
		from TaskStatus
		to   TaskStatus
	}{
		{TaskStatusQueued, TaskStatusDone},
		{TaskStatusQueued, TaskStatusInReview},
		{TaskStatusDone, TaskStatusQueued},
		{TaskStatusDone, TaskStatusInProgress},
		{TaskStatusInProgress, TaskStatusDone},
		{TaskStatusCancelledECO, TaskStatusQueued},
	}

	for i, tt := range invalid {
		task := makeTask(
			"invalid-"+string(rune('a'+i)),
			string(tt.from),
		)
		if err := db.CreateTask(task); err != nil {
			t.Fatalf("create task: %v", err)
		}

		err := db.UpdateTaskStatus(task.ID, tt.to)
		if err == nil {
			t.Errorf("transition %s -> %s should fail, but succeeded", tt.from, tt.to)
		}
	}
}

func TestGetReadyTasks(t *testing.T) {
	db := setupTestDB(t)

	// Create tasks
	taskA := makeTask("ready-a", "queued")
	taskB := makeTask("ready-b", "queued")
	taskC := makeTask("ready-c", "queued")

	for _, task := range []*Task{taskA, taskB, taskC} {
		if err := db.CreateTask(task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	// B depends on A. A and C have no deps so should be ready.
	if err := db.AddTaskDependency("ready-b", "ready-a"); err != nil {
		t.Fatalf("add dep: %v", err)
	}

	ready, err := db.GetReadyTasks()
	if err != nil {
		t.Fatalf("get ready tasks: %v", err)
	}

	ids := make(map[string]bool)
	for _, task := range ready {
		ids[task.ID] = true
	}

	if !ids["ready-a"] {
		t.Error("task A should be ready")
	}
	if ids["ready-b"] {
		t.Error("task B should not be ready (depends on A)")
	}
	if !ids["ready-c"] {
		t.Error("task C should be ready")
	}

	// Mark A as done, now B should be ready
	if err := db.UpdateTaskStatus("ready-a", TaskStatusInProgress); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := db.UpdateTaskStatus("ready-a", TaskStatusInReview); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := db.UpdateTaskStatus("ready-a", TaskStatusDone); err != nil {
		t.Fatalf("update: %v", err)
	}

	ready2, err := db.GetReadyTasks()
	if err != nil {
		t.Fatalf("get ready tasks: %v", err)
	}

	ids2 := make(map[string]bool)
	for _, task := range ready2 {
		ids2[task.ID] = true
	}

	if !ids2["ready-b"] {
		t.Error("task B should now be ready (A is done)")
	}
	if !ids2["ready-c"] {
		t.Error("task C should still be ready")
	}
}

func TestCircularDependencyDetection(t *testing.T) {
	db := setupTestDB(t)

	tasks := []*Task{
		makeTask("circ-a", "queued"),
		makeTask("circ-b", "queued"),
		makeTask("circ-c", "queued"),
	}

	for _, task := range tasks {
		if err := db.CreateTask(task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	// A -> B -> C is valid
	if err := db.AddTaskDependency("circ-a", "circ-b"); err != nil {
		t.Fatalf("add dep A->B: %v", err)
	}
	if err := db.AddTaskDependency("circ-b", "circ-c"); err != nil {
		t.Fatalf("add dep B->C: %v", err)
	}

	// C -> A would create cycle: A -> B -> C -> A
	err := db.AddTaskDependency("circ-c", "circ-a")
	if err == nil {
		t.Error("circular dependency should be detected")
	}

	// Self-dependency
	err = db.AddTaskDependency("circ-a", "circ-a")
	if err == nil {
		t.Error("self-dependency should be rejected")
	}
}

func TestListTasks(t *testing.T) {
	db := setupTestDB(t)

	tasks := []*Task{
		makeTask("list-1", "queued"),
		makeTask("list-2", "queued"),
		makeTask("list-3", "in_progress"),
	}
	// Set task 3 to in_progress by creating it directly with that status
	for _, task := range tasks {
		if err := db.CreateTask(task); err != nil {
			t.Fatalf("create task: %v", err)
		}
	}

	// Filter by status
	result, err := db.ListTasks(TaskFilter{Status: TaskStatusQueued})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 queued tasks, got %d", len(result))
	}
}
