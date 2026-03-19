package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

func TestNewCoordinator(t *testing.T) {
	// Create a temp project directory.
	tmpDir := t.TempDir()
	axiomDir := filepath.Join(tmpDir, ".axiom")
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	config.Project.Name = "test-project"
	config.Project.Slug = "test-project"

	coord, err := NewCoordinator(config, tmpDir)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	defer coord.Stop()

	// Verify subsystems are initialized.
	if coord.DB() == nil {
		t.Error("DB should not be nil")
	}
	if coord.Emitter() == nil {
		t.Error("Emitter should not be nil")
	}
	if coord.Config() == nil {
		t.Error("Config should not be nil")
	}
	if coord.InferenceBroker() == nil {
		t.Error("InferenceBroker should not be nil")
	}
	if coord.MergeQueue() == nil {
		t.Error("MergeQueue should not be nil")
	}
	if coord.SRSApproval() == nil {
		t.Error("SRSApproval should not be nil")
	}
	if coord.ECOManager() == nil {
		t.Error("ECOManager should not be nil")
	}
	if coord.BudgetTracker() == nil {
		t.Error("BudgetTracker should not be nil")
	}
	if coord.SecretScanner() == nil {
		t.Error("SecretScanner should not be nil")
	}
	if coord.GitManager() == nil {
		t.Error("GitManager should not be nil")
	}
	if coord.IPCWriter() == nil {
		t.Error("IPCWriter should not be nil")
	}
}

func TestCoordinatorEventPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	axiomDir := filepath.Join(tmpDir, ".axiom")
	if err := os.MkdirAll(axiomDir, 0755); err != nil {
		t.Fatal(err)
	}

	config := DefaultConfig()
	coord, err := NewCoordinator(config, tmpDir)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	defer coord.Stop()

	// Emit an event. The subscriber persists it synchronously (SubscribeAll
	// calls the handler inline for each emit).
	coord.Emitter().Emit(events.Event{
		Type:      events.EventTaskCreated,
		TaskID:    "test-001",
		AgentType: "engine",
		Details: map[string]interface{}{
			"key": "value",
			"num": 42,
		},
	})

	// Wait for async event dispatch.
	time.Sleep(50 * time.Millisecond)

	// Verify the event was persisted.
	evts, err := coord.DB().ListEvents(state.EventFilter{TaskID: "test-001"})
	if err != nil {
		t.Fatal(err)
	}
	if len(evts) == 0 {
		t.Fatal("expected at least 1 persisted event")
	}

	// Verify the details are JSON, not Go %v format.
	details := evts[0].Details
	if details == "" {
		t.Fatal("expected non-empty details")
	}
	// JSON should start with { and not contain "map["
	if len(details) > 0 && details[0] != '{' {
		t.Errorf("expected JSON details starting with '{', got: %s", details[:min(50, len(details))])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestCoordinatorCrashRecovery(t *testing.T) {
	tmpDir := t.TempDir()
	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	// Create staging directories to be cleaned up.
	stagingBase := filepath.Join(axiomDir, "containers", "staging")
	os.MkdirAll(filepath.Join(stagingBase, "task-001"), 0755)
	os.MkdirAll(filepath.Join(stagingBase, "task-002"), 0755)

	config := DefaultConfig()
	coord, err := NewCoordinator(config, tmpDir)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	defer coord.Stop()

	// Create a task in in_progress state (should be reset by crash recovery).
	db := coord.DB()
	task := &state.Task{
		ID:       "task-orphan-001",
		Title:    "Orphaned Task",
		Status:   "in_progress",
		Tier:     "standard",
		TaskType: "implementation",
	}
	if err := db.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Create a lock (should be released by crash recovery).
	db.AcquireLocks("task-orphan-001", []state.LockRequest{
		{ResourceType: "file", ResourceKey: "src/main.go"},
	})

	// Run crash recovery.
	if err := coord.crashRecovery(); err != nil {
		t.Fatalf("crashRecovery: %v", err)
	}

	// Verify task was reset to queued.
	recovered, err := db.GetTask("task-orphan-001")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Status != "queued" {
		t.Errorf("expected status 'queued', got '%s'", recovered.Status)
	}

	// Verify locks were released.
	locks, err := db.GetLockedResources()
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 0 {
		t.Errorf("expected 0 locks, got %d", len(locks))
	}

	// Verify staging directories were cleaned.
	entries, _ := os.ReadDir(stagingBase)
	if len(entries) != 0 {
		t.Errorf("expected staging cleaned, found %d entries", len(entries))
	}
}

func TestCoordinatorPauseResume(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".axiom"), 0755)

	config := DefaultConfig()
	coord, err := NewCoordinator(config, tmpDir)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	defer coord.Stop()

	coord.Pause()
	coord.mu.Lock()
	paused := coord.paused
	coord.mu.Unlock()
	if !paused {
		t.Error("expected paused=true after Pause()")
	}

	coord.Resume()
	coord.mu.Lock()
	paused = coord.paused
	coord.mu.Unlock()
	if paused {
		t.Error("expected paused=false after Resume()")
	}
}

func TestCoordinatorCompletionPercentage(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, ".axiom"), 0755)

	config := DefaultConfig()
	coord, err := NewCoordinator(config, tmpDir)
	if err != nil {
		t.Fatalf("NewCoordinator: %v", err)
	}
	defer coord.Stop()

	db := coord.DB()

	// No tasks: 0%
	if pct := coord.completionPercentage(); pct != 0 {
		t.Errorf("expected 0%%, got %.1f%%", pct)
	}

	// Create 4 tasks, 2 done.
	for i, status := range []string{"done", "done", "queued", "in_progress"} {
		db.CreateTask(&state.Task{
			ID:       fmt.Sprintf("task-%03d", i),
			Title:    fmt.Sprintf("Task %d", i),
			Status:   status,
			Tier:     "standard",
			TaskType: "implementation",
		})
	}

	pct := coord.completionPercentage()
	if pct != 50 {
		t.Errorf("expected 50%%, got %.1f%%", pct)
	}
}
