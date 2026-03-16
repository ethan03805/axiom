package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/state"
)

func setupTestDB(t *testing.T) (*state.DB, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-orch-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db, tmpDir
}

func TestEmbeddedPhaseTransitions(t *testing.T) {
	e := &Embedded{phase: PhaseBootstrap}

	if e.CurrentPhase() != PhaseBootstrap {
		t.Errorf("expected bootstrap, got %s", e.CurrentPhase())
	}

	e.TransitionToExecution()
	if e.CurrentPhase() != PhaseExecution {
		t.Errorf("expected execution, got %s", e.CurrentPhase())
	}

	e.Pause()
	if e.CurrentPhase() != PhasePaused {
		t.Errorf("expected paused, got %s", e.CurrentPhase())
	}

	e.Resume()
	if e.CurrentPhase() != PhaseExecution {
		t.Errorf("expected execution after resume, got %s", e.CurrentPhase())
	}

	e.Complete()
	if e.CurrentPhase() != PhaseCompleted {
		t.Errorf("expected completed, got %s", e.CurrentPhase())
	}
}

func TestBootstrapContextGreenfield(t *testing.T) {
	e := &Embedded{
		config: EmbeddedConfig{Runtime: RuntimeClaudeCode},
	}

	ctx := e.buildBootstrapContext("Build me a REST API", true)

	if ctx["project_type"] != "greenfield" {
		t.Errorf("expected greenfield, got %s", ctx["project_type"])
	}
	if ctx["prompt"] != "Build me a REST API" {
		t.Error("prompt not in context")
	}
	// Greenfield should NOT have index access.
	if ctx["index_access"] != nil {
		t.Error("greenfield should not have index_access")
	}
}

func TestBootstrapContextExisting(t *testing.T) {
	e := &Embedded{
		config: EmbeddedConfig{Runtime: RuntimeCodex},
	}

	ctx := e.buildBootstrapContext("Add user authentication", false)

	if ctx["project_type"] != "existing" {
		t.Errorf("expected existing, got %s", ctx["project_type"])
	}
	if ctx["index_access"] != true {
		t.Error("existing project should have index_access=true")
	}
}

func TestActionHandlerCreateTask(t *testing.T) {
	db, _ := setupTestDB(t)
	emitter := events.NewEmitter()
	handler := NewActionHandler(db, emitter)

	params, _ := json.Marshal(state.Task{
		ID:       "task-new",
		Title:    "New Task",
		Status:   "queued",
		Tier:     "standard",
		TaskType: "implementation",
	})
	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "create_task",
		Parameters: params,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, err := handler.HandleAction("orch-1", reqMsg, raw)
	if err != nil {
		t.Fatalf("handle: %v", err)
	}

	respMsg := resp.(*ipc.ActionResponseMessage)
	if !respMsg.Success {
		t.Fatalf("expected success, got error: %s", respMsg.Error)
	}

	// Verify task was created in DB.
	task, err := db.GetTask("task-new")
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if task.Title != "New Task" {
		t.Errorf("title = %s", task.Title)
	}
}

func TestActionHandlerCreateTaskBatch(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	tasks := []*state.Task{
		{ID: "batch-1", Title: "Batch 1", Status: "queued", Tier: "standard", TaskType: "implementation"},
		{ID: "batch-2", Title: "Batch 2", Status: "queued", Tier: "standard", TaskType: "implementation"},
	}
	params, _ := json.Marshal(tasks)
	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "create_task_batch",
		Parameters: params,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleAction("orch-1", reqMsg, raw)
	respMsg := resp.(*ipc.ActionResponseMessage)
	if !respMsg.Success {
		t.Fatalf("batch create failed: %s", respMsg.Error)
	}

	// Verify both tasks exist.
	t1, _ := db.GetTask("batch-1")
	t2, _ := db.GetTask("batch-2")
	if t1 == nil || t2 == nil {
		t.Error("batch tasks not created")
	}
}

func TestActionHandlerQueryStatus(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	// Create some tasks.
	db.CreateTask(&state.Task{ID: "t1", Title: "T1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	db.CreateTask(&state.Task{ID: "t2", Title: "T2", Status: "done", Tier: "standard", TaskType: "implementation"})

	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "query_status",
		Parameters: json.RawMessage(`{}`),
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleAction("orch-1", reqMsg, raw)
	respMsg := resp.(*ipc.ActionResponseMessage)
	if !respMsg.Success {
		t.Fatalf("query_status failed: %s", respMsg.Error)
	}
}

func TestActionHandlerQueryBudget(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "query_budget",
		Parameters: json.RawMessage(`{}`),
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleAction("orch-1", reqMsg, raw)
	respMsg := resp.(*ipc.ActionResponseMessage)
	if !respMsg.Success {
		t.Fatalf("query_budget failed: %s", respMsg.Error)
	}
}

func TestActionHandlerSubmitSRS(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	var srsReceived string
	handler.OnSubmitSRS = func(taskID, content string) error {
		srsReceived = content
		return nil
	}

	params, _ := json.Marshal(map[string]string{
		"content": "# SRS: My Project\n\n## 1. Architecture\n...",
	})
	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "submit_srs",
		Parameters: params,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleAction("orch-1", reqMsg, raw)
	respMsg := resp.(*ipc.ActionResponseMessage)
	if !respMsg.Success {
		t.Fatalf("submit_srs failed: %s", respMsg.Error)
	}
	if srsReceived == "" {
		t.Error("SRS callback was not called")
	}
}

func TestActionHandlerSpawnMeeseeks(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	var spawnedTaskID, spawnedModel string
	handler.OnSpawnMeeseeks = func(taskID, modelID string) error {
		spawnedTaskID = taskID
		spawnedModel = modelID
		return nil
	}

	params, _ := json.Marshal(map[string]string{
		"task_id":  "task-042",
		"model_id": "anthropic/claude-4-sonnet",
	})
	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "spawn_meeseeks",
		Parameters: params,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleAction("orch-1", reqMsg, raw)
	respMsg := resp.(*ipc.ActionResponseMessage)
	if !respMsg.Success {
		t.Fatalf("spawn failed: %s", respMsg.Error)
	}
	if spawnedTaskID != "task-042" {
		t.Errorf("expected task-042, got %s", spawnedTaskID)
	}
	if spawnedModel != "anthropic/claude-4-sonnet" {
		t.Errorf("expected claude-4-sonnet, got %s", spawnedModel)
	}
}

func TestActionHandlerApproveReject(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	var approved, rejected bool
	handler.OnApproveOutput = func(taskID string) error { approved = true; return nil }
	handler.OnRejectOutput = func(taskID, feedback string) error { rejected = true; return nil }

	// Approve.
	params, _ := json.Marshal(map[string]string{"task_id": "t1"})
	reqMsg := &ipc.ActionRequestMessage{
		Header: ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action: "approve_output", Parameters: params,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)
	handler.HandleAction("orch-1", reqMsg, raw)
	if !approved {
		t.Error("approve callback not called")
	}

	// Reject.
	params, _ = json.Marshal(map[string]string{"task_id": "t2", "feedback": "fix bugs"})
	reqMsg = &ipc.ActionRequestMessage{
		Header: ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action: "reject_output", Parameters: params,
	}
	raw, _ = ipc.MarshalMessage(reqMsg)
	handler.HandleAction("orch-1", reqMsg, raw)
	if !rejected {
		t.Error("reject callback not called")
	}
}

func TestActionHandlerUnknownAction(t *testing.T) {
	db, _ := setupTestDB(t)
	handler := NewActionHandler(db, events.NewEmitter())

	reqMsg := &ipc.ActionRequestMessage{
		Header:     ipc.Header{Type: ipc.TypeActionRequest, TaskID: "orch-1"},
		Action:     "fly_to_moon",
		Parameters: json.RawMessage(`{}`),
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleAction("orch-1", reqMsg, raw)
	respMsg := resp.(*ipc.ActionResponseMessage)
	if respMsg.Success {
		t.Error("expected failure for unknown action")
	}
	if respMsg.Error == "" {
		t.Error("expected error message")
	}
}
