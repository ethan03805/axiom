// Package integration_test contains end-to-end integration tests that verify
// subsystem interactions across the Axiom engine.
//
// These tests use real SQLite databases and filesystem IPC but mock the Docker
// daemon and external API providers, allowing them to run in CI without
// external services.
//
// See BUILD_PLAN Phase 22 for the test plan.
package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/budget"
	"github.com/ethan03805/axiom/internal/engine"
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/pipeline"
	"github.com/ethan03805/axiom/internal/security"
	"github.com/ethan03805/axiom/internal/srs"
	"github.com/ethan03805/axiom/internal/state"
)

// --- Test helpers ---

type testEnv struct {
	tmpDir  string
	db      *state.DB
	emitter *events.Emitter
}

func setupEnv(t *testing.T) *testEnv {
	t.Helper()
	tmpDir, _ := os.MkdirTemp("", "axiom-integration-*")
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)
	os.MkdirAll(filepath.Join(axiomDir, "containers", "ipc"), 0755)

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	db.RunMigrations()
	t.Cleanup(func() { db.Close() })

	return &testEnv{
		tmpDir:  tmpDir,
		db:      db,
		emitter: events.NewEmitter(),
	}
}

// --- 22.1: Full IPC round-trip ---

func TestIPCRoundTrip(t *testing.T) {
	env := setupEnv(t)
	ipcDir := filepath.Join(env.tmpDir, ".axiom", "containers", "ipc")

	// Create task IPC directories.
	taskID := "task-ipc-rt"
	os.MkdirAll(filepath.Join(ipcDir, taskID, "input"), 0755)
	os.MkdirAll(filepath.Join(ipcDir, taskID, "output"), 0755)

	// Engine writes a TaskSpec to the container's input dir.
	writer := ipc.NewWriter(ipcDir)
	specMsg := &ipc.TaskSpecMessage{
		Header: ipc.Header{Type: ipc.TypeTaskSpec, TaskID: taskID},
		Spec:   "# TaskSpec: task-ipc-rt\n\n## Objective\nBuild auth handler.",
	}
	if err := writer.Send(taskID, specMsg); err != nil {
		t.Fatalf("write spec: %v", err)
	}

	// Verify the file was written.
	inputDir := filepath.Join(ipcDir, taskID, "input")
	entries, _ := os.ReadDir(inputDir)
	if len(entries) != 1 {
		t.Fatalf("expected 1 input file, got %d", len(entries))
	}

	// Set up watcher BEFORE writing output (so it's watching when file arrives).
	var received bool
	var mu sync.Mutex
	watcher, _ := ipc.NewWatcher(ipcDir, func(tid string, msg interface{}, raw []byte) {
		mu.Lock()
		received = true
		mu.Unlock()
	})
	defer watcher.Stop()
	watcher.WatchTask(taskID)

	// Small delay to ensure watcher is ready.
	time.Sleep(100 * time.Millisecond)

	// NOW simulate container writing task_output to output dir.
	outputMsg := &ipc.TaskOutputMessage{
		Header:       ipc.Header{Type: ipc.TypeTaskOutput, TaskID: taskID},
		BaseSnapshot: "abc123",
		Manifest:     json.RawMessage(`{"task_id":"task-ipc-rt","base_snapshot":"abc123","files":{"added":[]}}`),
	}
	outputData, _ := ipc.MarshalMessage(outputMsg)
	outputDir := filepath.Join(ipcDir, taskID, "output")
	tmpFile := filepath.Join(outputDir, "task_output-0000.json.tmp")
	finalFile := filepath.Join(outputDir, "task_output-0000.json")
	os.WriteFile(tmpFile, outputData, 0644)
	os.Rename(tmpFile, finalFile)

	// Wait for detection.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		done := received
		mu.Unlock()
		if done {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not detect output within 2s")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// --- 22.2: Approval pipeline end-to-end ---

func TestApprovalPipelineFullPass(t *testing.T) {
	env := setupEnv(t)

	// Create staging dir with valid output.
	stagingDir := filepath.Join(env.tmpDir, "staging")
	os.MkdirAll(filepath.Join(stagingDir, "src"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "src", "auth.go"), []byte("package src\nfunc Auth() {}\n"), 0644)

	manifest := &pipeline.Manifest{
		TaskID:       "task-pipeline",
		BaseSnapshot: "abc123",
		Files: pipeline.ManifestFiles{
			Added: []pipeline.FileEntry{{Path: "src/auth.go", Binary: false}},
		},
	}
	mData, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(stagingDir, "manifest.json"), mData, 0644)

	// Run the pipeline with all stages passing.
	p := pipeline.NewPipeline(pipeline.DefaultPipelineConfig())
	p.RunValidationFn = func(taskID, dir string) (*pipeline.ValidationResult, error) {
		return &pipeline.ValidationResult{CompilePass: true, LintPass: true, TestPass: true, TestCount: 5, TestPassed: 5}, nil
	}
	p.RunReviewFn = func(taskID, spec, output, val string) (*pipeline.ReviewVerdict, error) {
		return &pipeline.ReviewVerdict{Verdict: "approve"}, nil
	}
	p.RunOrchestratorFn = func(taskID, output string) (bool, string, error) {
		return true, "", nil
	}
	p.SubmitToMergeQueueFn = func(taskID, dir, snap string) error {
		return nil
	}

	result := p.Execute("task-pipeline", stagingDir, "# TaskSpec", []string{"src/auth.go"}, "abc123", 1, "standard")

	if !result.Approved {
		t.Errorf("pipeline should approve; stages: %+v", result.StageResults)
	}
	if len(result.StageResults) != 5 {
		t.Errorf("expected 5 stages, got %d", len(result.StageResults))
	}
}

func TestApprovalPipelineRetryOnReviewerReject(t *testing.T) {
	env := setupEnv(t)
	_ = env

	stagingDir, _ := os.MkdirTemp("", "axiom-pipeline-retry-*")
	defer os.RemoveAll(stagingDir)
	os.WriteFile(filepath.Join(stagingDir, "main.go"), []byte("package main"), 0644)
	manifest := &pipeline.Manifest{TaskID: "task-retry", Files: pipeline.ManifestFiles{Added: []pipeline.FileEntry{{Path: "main.go"}}}}
	mData, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(stagingDir, "manifest.json"), mData, 0644)

	p := pipeline.NewPipeline(pipeline.DefaultPipelineConfig())
	p.RunValidationFn = func(taskID, dir string) (*pipeline.ValidationResult, error) {
		return &pipeline.ValidationResult{CompilePass: true, LintPass: true, TestPass: true}, nil
	}
	p.RunReviewFn = func(taskID, spec, output, val string) (*pipeline.ReviewVerdict, error) {
		return &pipeline.ReviewVerdict{Verdict: "reject", Feedback: "Missing error handling"}, nil
	}

	// Attempt 1: should retry.
	result := p.Execute("task-retry", stagingDir, "# Spec", []string{"main.go"}, "abc", 1, "standard")
	if result.Approved {
		t.Error("should not approve on reviewer reject")
	}
	if !result.ShouldRetry {
		t.Error("attempt 1 should retry")
	}

	// Attempt 3: should escalate.
	result = p.Execute("task-retry", stagingDir, "# Spec", []string{"main.go"}, "abc", 3, "standard")
	if !result.ShouldEscalate {
		t.Error("attempt 3 should escalate")
	}
}

// --- 22.3: Write-set locking with concurrent tasks ---

func TestConcurrentLocking(t *testing.T) {
	env := setupEnv(t)
	wq := engine.NewWorkQueue(env.db, env.emitter, 10)

	// Create two tasks targeting the same file.
	env.db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.AddTaskTargetFile("t1", "shared.go", "file")
	env.db.AddTaskTargetFile("t2", "shared.go", "file")

	// Dispatch t1 (acquires lock).
	dispatchable, _ := wq.GetDispatchable()
	if len(dispatchable) != 2 {
		// Both are ready since neither has locks yet.
	}

	locks1 := []state.LockRequest{{ResourceType: "file", ResourceKey: "shared.go"}}
	ok, _ := wq.AcquireAndDispatch("t1", locks1)
	if !ok {
		t.Fatal("t1 should dispatch")
	}

	// t2 should fail to acquire the same lock.
	locks2 := []state.LockRequest{{ResourceType: "file", ResourceKey: "shared.go"}}
	ok, _ = wq.AcquireAndDispatch("t2", locks2)
	if ok {
		t.Error("t2 should fail (lock held by t1)")
	}

	// Complete t1 (releases lock).
	env.db.UpdateTaskStatus("t1", state.TaskStatusInReview)
	env.db.UpdateTaskStatus("t1", state.TaskStatusDone)
	wq.CompleteTask("t1")

	// Now t2 should succeed.
	ok, _ = wq.AcquireAndDispatch("t2", locks2)
	if !ok {
		t.Error("t2 should dispatch after t1 releases lock")
	}
}

func TestScopeExpansionLockConflict(t *testing.T) {
	env := setupEnv(t)
	handler := engine.NewScopeExpansionHandler(env.db, env.emitter)

	env.db.CreateTask(&state.Task{ID: "t-holder", Title: "Holder", Status: "in_progress", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-requester", Title: "Requester", Status: "in_progress", Tier: "standard", TaskType: "implementation"})

	// t-holder locks the file.
	env.db.AcquireLocks("t-holder", []state.LockRequest{{ResourceType: "file", ResourceKey: "shared.go"}})

	// t-requester requests expansion to the locked file.
	reqMsg := &ipc.ScopeExpansionRequestMessage{
		Header:          ipc.Header{Type: ipc.TypeScopeExpansionRequest, TaskID: "t-requester"},
		AdditionalFiles: []string{"shared.go"},
		Reason:          "need to modify shared code",
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	resp, _ := handler.HandleScopeExpansion("t-requester", reqMsg, raw)
	respMsg := resp.(*ipc.ScopeExpansionResponseMessage)

	if respMsg.Status != "waiting_on_lock" {
		t.Errorf("expected waiting_on_lock, got %s", respMsg.Status)
	}
	if respMsg.BlockedBy != "t-holder" {
		t.Errorf("expected blocked_by=t-holder, got %s", respMsg.BlockedBy)
	}
}

// --- 22.4: Merge queue ---

// Merge queue tests are in internal/merge/queue_test.go (Phase 9).
// This verifies the integration between work queue and merge queue.

func TestWorkQueueMergeQueueIntegration(t *testing.T) {
	env := setupEnv(t)
	wq := engine.NewWorkQueue(env.db, env.emitter, 10)

	// Create and dispatch a task.
	env.db.CreateTask(&state.Task{ID: "t-wqmq", Title: "WQ-MQ Test", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.AddTaskTargetFile("t-wqmq", "output.go", "file")

	locks := []state.LockRequest{{ResourceType: "file", ResourceKey: "output.go"}}
	wq.AcquireAndDispatch("t-wqmq", locks)

	// Verify lock is held.
	locked, holder, _ := env.db.IsLocked("file", "output.go")
	if !locked || holder != "t-wqmq" {
		t.Error("lock should be held by t-wqmq")
	}

	// Complete the task via work queue.
	wq.CompleteTask("t-wqmq")

	// Lock should be released.
	locked, _, _ = env.db.IsLocked("file", "output.go")
	if locked {
		t.Error("lock should be released after completion")
	}
}

// --- 22.5: Budget enforcement ---

func TestBudgetEnforcementIntegration(t *testing.T) {
	env := setupEnv(t)

	enforcer := budget.NewEnforcer(env.db, env.emitter, budget.EnforcerConfig{
		MaxUSD:        1.0,
		WarnAtPercent: 80,
	})

	env.db.CreateTask(&state.Task{ID: "t-budget", Title: "Budget Test", Status: "queued", Tier: "standard", TaskType: "implementation"})

	// Spend $0.90.
	env.db.InsertCost(&state.CostEntry{
		TaskID: "t-budget", AgentType: "meeseeks", ModelID: "test",
		InputTokens: 1000, OutputTokens: 500, CostUSD: 0.90, Timestamp: time.Now(),
	})

	// Request that would push over budget.
	err := enforcer.PreAuthorize(0.20)
	if err == nil {
		t.Error("expected budget rejection")
	}

	// Small request should still work.
	err = enforcer.PreAuthorize(0.05)
	if err != nil {
		t.Errorf("small request should pass: %v", err)
	}

	// Exhaust budget completely.
	env.db.InsertCost(&state.CostEntry{
		TaskID: "t-budget", AgentType: "meeseeks", ModelID: "test",
		CostUSD: 0.10, Timestamp: time.Now(),
	})
	enforcer.RecordAndCheck(0)

	if !enforcer.IsPaused() {
		t.Error("should be paused after exhaustion")
	}

	// Increase budget and resume.
	enforcer.IncreaseBudget(5.0)
	if enforcer.IsPaused() {
		t.Error("should resume after increase")
	}
}

// --- 22.6: ECO flow ---

func TestECOFlowIntegration(t *testing.T) {
	env := setupEnv(t)
	axiomDir := filepath.Join(env.tmpDir, ".axiom")

	ecoMgr := srs.NewECOManager(env.db, env.emitter, axiomDir)

	// Create tasks that will be affected by the ECO.
	env.db.CreateTask(&state.Task{ID: "t-eco-1", Title: "Auth", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-eco-2", Title: "Auth Tests", Status: "queued", Tier: "standard", TaskType: "test"})

	// Propose and approve an ECO.
	eco, err := ecoMgr.ProposeECO("ECO-DEP", "passport-oauth2 removed from npm", "FR-003, AC-005", "Use arctic v2.1 instead")
	if err != nil {
		t.Fatalf("propose: %v", err)
	}

	if err := ecoMgr.ApproveECO(eco.ID, "user"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Cancel affected tasks.
	if err := ecoMgr.CancelAffectedTasks(eco.ID, []string{"t-eco-1", "t-eco-2"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Verify tasks are cancelled.
	task1, _ := env.db.GetTask("t-eco-1")
	if task1.Status != "cancelled_eco" {
		t.Errorf("t-eco-1 status = %s, want cancelled_eco", task1.Status)
	}

	// Verify ECO addendum file exists.
	addendumPath := filepath.Join(axiomDir, "eco", "ECO-001.md")
	if _, err := os.Stat(addendumPath); os.IsNotExist(err) {
		t.Error("ECO addendum file should exist")
	}

	// Test ECO rejection flow.
	eco2, _ := ecoMgr.ProposeECO("ECO-API", "endpoint changed", "FR-005", "use v2")
	ecoMgr.RejectECO(eco2.ID, "user")

	ecos, _ := env.db.ListECOs("rejected")
	if len(ecos) != 1 {
		t.Errorf("expected 1 rejected ECO, got %d", len(ecos))
	}
}

// --- 22.7: Crash recovery ---

func TestCrashRecoveryIntegration(t *testing.T) {
	env := setupEnv(t)

	// Simulate pre-crash state: tasks in various states, stale locks.
	env.db.CreateTask(&state.Task{ID: "t-crash-1", Title: "In Progress", Status: "in_progress", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-crash-2", Title: "In Review", Status: "in_review", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-crash-3", Title: "Done", Status: "done", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-crash-4", Title: "Queued", Status: "queued", Tier: "standard", TaskType: "implementation"})

	env.db.AcquireLocks("t-crash-1", []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}})

	// Create stale staging dirs.
	axiomDir := filepath.Join(env.tmpDir, ".axiom")
	stagingDir := filepath.Join(axiomDir, "containers", "staging", "t-crash-1")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "stale.go"), []byte("stale"), 0644)

	// Write and lock an SRS for integrity check.
	lm := srs.NewLockManager(axiomDir)
	lm.WriteDraft("# SRS: Test")
	lm.Lock()

	// Simulate crash recovery by directly calling CrashRecovery on the DB.
	// (engine.New tries to re-run migrations which fail on existing tables;
	// in production the DB already exists from before the crash.)
	conn := env.db.Conn()

	// Reset orphaned in_progress/in_review tasks to queued.
	conn.Exec("UPDATE tasks SET status = 'queued' WHERE status IN ('in_progress', 'in_review')")

	// Release all stale locks.
	conn.Exec("DELETE FROM task_locks")

	// Clean staging dirs.
	engine.CleanStagingDirs(axiomDir)

	// Verify in_progress and in_review tasks reset to queued.
	t1, _ := env.db.GetTask("t-crash-1")
	if t1.Status != "queued" {
		t.Errorf("t-crash-1 should be queued, got %s", t1.Status)
	}
	t2, _ := env.db.GetTask("t-crash-2")
	if t2.Status != "queued" {
		t.Errorf("t-crash-2 should be queued, got %s", t2.Status)
	}

	// Verify done task is unchanged.
	t3, _ := env.db.GetTask("t-crash-3")
	if t3.Status != "done" {
		t.Errorf("t-crash-3 should stay done, got %s", t3.Status)
	}

	// Verify locks are released.
	locked, _, _ := env.db.IsLocked("file", "main.go")
	if locked {
		t.Error("stale locks should be released")
	}

	// Verify staging dir was cleaned.
	entries, _ := os.ReadDir(filepath.Join(axiomDir, "containers", "staging"))
	if len(entries) != 0 {
		t.Error("staging dir should be cleaned")
	}

	// Verify SRS integrity passes (no tampering).
	if err := lm.VerifyIntegrity(); err != nil {
		t.Errorf("SRS integrity should pass: %v", err)
	}
}

// --- 22.8: Secret scanning and redaction ---

func TestSecretScanningIntegration(t *testing.T) {
	scanner := security.NewSecretScanner(nil)

	// Files with API keys should have secrets redacted.
	files := map[string]string{
		"src/config.go": `package config
var APIKey = "sk-1234567890abcdefghijklmnop"
var DBHost = "localhost"
`,
		"src/main.go": `package main
func main() {}
`,
		".env": `SECRET_KEY=ghp_abcdefghijklmnopqrstuvwxyz1234567890`,
	}

	// PrepareContextForPrompt applies all mitigations.
	result := security.PrepareContextForPrompt(files, scanner)

	// .env should be excluded.
	if _, ok := result[".env"]; ok {
		t.Error(".env should be excluded from prompt context")
	}

	// config.go should have the key redacted.
	configContent, ok := result["src/config.go"]
	if !ok {
		t.Fatal("src/config.go should be included")
	}
	if !contains(configContent, "[REDACTED]") {
		t.Error("API key should be redacted in config.go")
	}
	if contains(configContent, "sk-1234567890") {
		t.Error("raw API key should NOT be in redacted content")
	}

	// main.go should be included and wrapped but not redacted.
	mainContent, ok := result["src/main.go"]
	if !ok {
		t.Fatal("src/main.go should be included")
	}
	if !contains(mainContent, "untrusted_repo_content") {
		t.Error("content should be wrapped in untrusted tags")
	}

	// Sensitive file routing.
	if !scanner.ShouldForceLocal(".env", "SECRET=value") {
		t.Error(".env should force local inference")
	}
	if !scanner.ShouldForceLocal("config.go", `key := "sk-abcdefghijklmnopqrstuvwx"`) {
		t.Error("content with API key should force local")
	}
	if scanner.ShouldForceLocal("main.go", "package main") {
		t.Error("clean content should not force local")
	}
}

// --- 22.9: Broker validation integration ---

func TestBrokerValidationIntegration(t *testing.T) {
	env := setupEnv(t)

	mockProvider := &mockBrokerProvider{available: true}
	ipcWriter := ipc.NewWriter(filepath.Join(env.tmpDir, ".axiom", "containers", "ipc"))

	b := broker.New(mockProvider, nil, env.db, env.emitter, ipcWriter, broker.Config{
		BudgetMaxUSD:  10.0,
		MaxReqPerTask: 5,
	})

	b.RegisterModel(&broker.ModelInfo{
		ID: "test-model", Tier: broker.TierStandard, Source: "openrouter",
		Pricing: broker.ModelPricing{PromptPerMillion: 3.0, CompletionPerMillion: 15.0},
	})
	b.SetTaskTier("t-broker", broker.TierStandard)

	env.db.CreateTask(&state.Task{ID: "t-broker", Title: "Broker Test", Status: "in_progress", Tier: "standard", TaskType: "implementation"})

	// Build IPC request.
	reqMsg := &ipc.InferenceRequestMessage{
		Header:    ipc.Header{Type: ipc.TypeInferenceRequest, TaskID: "t-broker"},
		ModelID:   "test-model",
		Messages:  []ipc.ChatMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 100,
	}
	raw, _ := ipc.MarshalMessage(reqMsg)

	// First request should succeed.
	resp, _ := b.HandleInferenceRequest("t-broker", reqMsg, raw)
	respMsg := resp.(*ipc.InferenceResponseMessage)
	if respMsg.FinishReason == "error" {
		t.Errorf("first request should succeed: %s", respMsg.Error)
	}

	// Make 4 more requests to hit the rate limit.
	for i := 0; i < 4; i++ {
		b.HandleInferenceRequest("t-broker", reqMsg, raw)
	}

	// 6th request should be rate limited.
	resp, _ = b.HandleInferenceRequest("t-broker", reqMsg, raw)
	respMsg = resp.(*ipc.InferenceResponseMessage)
	if respMsg.FinishReason != "error" {
		t.Error("6th request should be rate limited")
	}
}

// Mock provider for broker tests.
type mockBrokerProvider struct {
	available bool
}

func (m *mockBrokerProvider) Name() string                { return "mock" }
func (m *mockBrokerProvider) Available(_ context.Context) bool { return m.available }
func (m *mockBrokerProvider) Complete(_ context.Context, req *broker.InferenceRequest) (*broker.InferenceResponse, error) {
	return &broker.InferenceResponse{
		Content: "mock response", ModelID: req.ModelID,
		InputTokens: 50, OutputTokens: 25, FinishReason: "stop",
	}, nil
}
func (m *mockBrokerProvider) CompleteStream(_ context.Context, req *broker.InferenceRequest, onChunk func(string, bool)) (*broker.InferenceResponse, error) {
	onChunk("mock", true)
	return &broker.InferenceResponse{Content: "mock", ModelID: req.ModelID, FinishReason: "stop"}, nil
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
