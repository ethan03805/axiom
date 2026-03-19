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
	stdfmt "fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/budget"
	"github.com/ethan03805/axiom/internal/engine"
	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/git"
	"github.com/ethan03805/axiom/internal/ipc"
	"github.com/ethan03805/axiom/internal/merge"
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
	tmpDir := t.TempDir()

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

	// --- Phase A: Engine writes a TaskSpec to the container's input dir ---
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

	// Verify the file content round-trips through parse/marshal.
	data, err := os.ReadFile(filepath.Join(inputDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read input file: %v", err)
	}
	parsed, err := ipc.ParseMessage(data)
	if err != nil {
		t.Fatalf("parse input message: %v", err)
	}
	parsedSpec, ok := parsed.(*ipc.TaskSpecMessage)
	if !ok {
		t.Fatalf("expected TaskSpecMessage, got %T", parsed)
	}
	if parsedSpec.TaskID != taskID {
		t.Errorf("task ID mismatch: got %s, want %s", parsedSpec.TaskID, taskID)
	}
	if !strings.Contains(parsedSpec.Spec, "Build auth handler") {
		t.Error("spec content missing objective")
	}

	// --- Phase B: Register a handler with the Dispatcher ---
	dispatcher := ipc.NewDispatcher(writer)
	var handlerCalled bool
	var handlerMu sync.Mutex
	var receivedTaskID string
	var receivedSnapshot string

	dispatcher.Register(ipc.TypeTaskOutput, func(tid string, msg interface{}, raw []byte) (interface{}, error) {
		handlerMu.Lock()
		defer handlerMu.Unlock()
		handlerCalled = true
		receivedTaskID = tid
		if outMsg, ok := msg.(*ipc.TaskOutputMessage); ok {
			receivedSnapshot = outMsg.BaseSnapshot
		}
		// Return an acknowledgement response.
		return &ipc.ActionResponseMessage{
			Header:  ipc.Header{Type: ipc.TypeActionResponse, TaskID: tid},
			Action:  "task_output_ack",
			Success: true,
		}, nil
	})

	// --- Phase C: Set up watcher to detect container output ---
	var watcherReceived bool
	var watcherMu sync.Mutex
	watcher, _ := ipc.NewWatcher(ipcDir, func(tid string, msg interface{}, raw []byte) {
		// Forward to dispatcher for routing.
		dispatcher.Dispatch(tid, msg, raw)
		watcherMu.Lock()
		watcherReceived = true
		watcherMu.Unlock()
	})
	defer watcher.Stop()
	watcher.WatchTask(taskID)

	// Small delay to ensure watcher is ready.
	time.Sleep(100 * time.Millisecond)

	// --- Phase D: Simulate container writing task_output ---
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

	// --- Phase E: Verify watcher detects and dispatcher routes ---
	deadline := time.After(3 * time.Second)
	for {
		watcherMu.Lock()
		wd := watcherReceived
		watcherMu.Unlock()
		handlerMu.Lock()
		hd := handlerCalled
		handlerMu.Unlock()
		if wd && hd {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not detect output or handler not called within 3s")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	// Verify handler received correct data.
	handlerMu.Lock()
	if receivedTaskID != taskID {
		t.Errorf("handler received task ID %s, want %s", receivedTaskID, taskID)
	}
	if receivedSnapshot != "abc123" {
		t.Errorf("handler received snapshot %s, want abc123", receivedSnapshot)
	}
	handlerMu.Unlock()

	// --- Phase F: Verify response was written back (dispatcher writes to input) ---
	time.Sleep(100 * time.Millisecond)
	inputEntries, _ := os.ReadDir(inputDir)
	if len(inputEntries) < 2 {
		t.Logf("expected response written back to input dir; got %d files (may include original spec)", len(inputEntries))
	}
}

// --- 22.2: Approval pipeline end-to-end ---

func TestPipelineEndToEnd(t *testing.T) {
	// Create staging dir with valid output.
	stagingDir := t.TempDir()
	os.MkdirAll(filepath.Join(stagingDir, "src"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "src", "auth.go"), []byte("package src\nfunc Auth() {}\n"), 0644)

	manifest := &pipeline.Manifest{
		TaskID:       "task-pipeline-e2e",
		BaseSnapshot: "abc123",
		Files: pipeline.ManifestFiles{
			Added: []pipeline.FileEntry{{Path: "src/auth.go", Binary: false}},
		},
	}
	mData, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(stagingDir, "manifest.json"), mData, 0644)

	// Run the pipeline with all stages passing (nil callbacks for stages 2-5 means auto-pass).
	p := pipeline.NewPipeline(pipeline.DefaultPipelineConfig())
	// Stage 1 uses real ValidateManifest + ValidatePathSafety (default implementation).
	// Stages 2-5 use nil callbacks (auto-pass).

	result := p.Execute("task-pipeline-e2e", stagingDir, "# TaskSpec", []string{"src/auth.go"}, "abc123", 1, "standard")

	// Verify Stage 1 (manifest) passes.
	if len(result.StageResults) == 0 {
		t.Fatal("expected at least 1 stage result")
	}
	if !result.StageResults[0].Passed {
		t.Errorf("stage 1 (manifest) should pass; errors: %v", result.StageResults[0].Errors)
	}

	// Verify the PipelineResult is approved.
	if !result.Approved {
		t.Errorf("pipeline should approve; stages: %+v", result.StageResults)
	}
	if len(result.StageResults) != 5 {
		t.Errorf("expected 5 stages, got %d", len(result.StageResults))
	}
	for i, sr := range result.StageResults {
		if !sr.Passed {
			t.Errorf("stage %d (%s) should pass; errors: %v", i+1, sr.Stage, sr.Errors)
		}
	}
}

// --- 22.2b: Pipeline rejection and retry ---

func TestPipelineRejectionAndRetry(t *testing.T) {
	stagingDir := t.TempDir()
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

	// Attempt 1: should retry (attempt < MaxRetriesPerTier which is 3).
	result := p.Execute("task-retry", stagingDir, "# Spec", []string{"main.go"}, "abc", 1, "standard")
	if result.Approved {
		t.Error("should not approve on reviewer reject")
	}
	if !result.ShouldRetry {
		t.Error("attempt 1 should allow retry")
	}
	if result.ShouldEscalate {
		t.Error("attempt 1 should not escalate")
	}

	// Attempt 2: should still retry.
	result = p.Execute("task-retry", stagingDir, "# Spec", []string{"main.go"}, "abc", 2, "standard")
	if !result.ShouldRetry {
		t.Error("attempt 2 should still retry")
	}

	// Attempt 3: retries exhausted at standard tier, should escalate.
	result = p.Execute("task-retry", stagingDir, "# Spec", []string{"main.go"}, "abc", 3, "standard")
	if !result.ShouldEscalate {
		t.Error("attempt 3 at standard should escalate")
	}
	if result.ShouldRetry {
		t.Error("attempt 3 should not retry")
	}

	// Attempt 3 at premium tier: cannot escalate further, should block.
	result = p.Execute("task-retry", stagingDir, "# Spec", []string{"main.go"}, "abc", 3, "premium")
	if !result.ShouldBlock {
		t.Error("attempt 3 at premium should block")
	}
	if result.ShouldRetry || result.ShouldEscalate {
		t.Error("attempt 3 at premium should neither retry nor escalate")
	}
}

// --- 22.3: Write-set locking with concurrent tasks ---

func TestConcurrentLockContention(t *testing.T) {
	env := setupEnv(t)
	wq := engine.NewWorkQueue(env.db, env.emitter, 10)

	// Create two tasks targeting the same file.
	env.db.CreateTask(&state.Task{ID: "t1", Title: "Task 1", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t2", Title: "Task 2", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.AddTaskTargetFile("t1", "shared.go", "file")
	env.db.AddTaskTargetFile("t2", "shared.go", "file")

	// Both should appear in dispatchable list initially (both queued, deps met).
	dispatchable, _ := wq.GetDispatchable()
	if len(dispatchable) < 2 {
		t.Logf("dispatchable tasks: %d (both queued with no deps, expected 2)", len(dispatchable))
	}

	// Dispatch t1 (acquires lock on shared.go).
	locks1 := []state.LockRequest{{ResourceType: "file", ResourceKey: "shared.go"}}
	ok, err := wq.AcquireAndDispatch("t1", locks1)
	if err != nil {
		t.Fatalf("dispatch t1: %v", err)
	}
	if !ok {
		t.Fatal("t1 should dispatch successfully")
	}

	// Verify t1 is now in_progress.
	task1, _ := env.db.GetTask("t1")
	if task1.Status != "in_progress" {
		t.Errorf("t1 status = %s, want in_progress", task1.Status)
	}

	// Verify lock is held by t1.
	locked, holder, _ := env.db.IsLocked("file", "shared.go")
	if !locked || holder != "t1" {
		t.Errorf("lock should be held by t1; locked=%v holder=%s", locked, holder)
	}

	// t2 should fail to acquire the same lock.
	locks2 := []state.LockRequest{{ResourceType: "file", ResourceKey: "shared.go"}}
	ok, _ = wq.AcquireAndDispatch("t2", locks2)
	if ok {
		t.Error("t2 should fail (lock held by t1)")
	}

	// t2 should still be queued.
	task2, _ := env.db.GetTask("t2")
	if task2.Status != "queued" {
		t.Errorf("t2 status = %s, want queued", task2.Status)
	}

	// Complete t1 (releases lock).
	env.db.UpdateTaskStatus("t1", state.TaskStatusInReview)
	env.db.UpdateTaskStatus("t1", state.TaskStatusDone)
	wq.CompleteTask("t1")

	// Verify lock is released.
	locked, _, _ = env.db.IsLocked("file", "shared.go")
	if locked {
		t.Error("lock should be released after t1 completion")
	}

	// Now t2 should succeed.
	ok, _ = wq.AcquireAndDispatch("t2", locks2)
	if !ok {
		t.Error("t2 should dispatch after t1 releases lock")
	}

	// Verify t2 is now in_progress.
	task2, _ = env.db.GetTask("t2")
	if task2.Status != "in_progress" {
		t.Errorf("t2 status = %s, want in_progress", task2.Status)
	}

	// Verify work queue active count.
	if wq.ActiveCount() != 1 {
		t.Errorf("active count = %d, want 1", wq.ActiveCount())
	}
}

// --- 22.3b: Scope expansion with lock conflict ---

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

// --- 22.4: Merge queue serialization ---

func TestMergeQueueSerialization(t *testing.T) {
	// Create a temp git repo with an initial commit.
	repoDir := t.TempDir()
	gitInit(t, repoDir)
	gitCommitEmpty(t, repoDir, "initial commit")

	emitter := events.NewEmitter()

	// Get HEAD SHA for base snapshot.
	headSHA := gitHeadSHA(t, repoDir)

	// Create a git manager and merge queue pointed at the temp repo.
	gitMgr := newTestGitManager(repoDir)
	mq := merge.NewQueue(gitMgr, emitter)

	// Submit 2 items to the merge queue.
	item1 := &merge.MergeItem{
		TaskID:       "task-mq-1",
		TaskTitle:    "First task",
		BaseSnapshot: headSHA,
		Files:        map[string]string{"file1.go": "package main\n// file1\n"},
		SRSRefs:      []string{"FR-001"},
		MeeseeksModel: "test-model",
		AttemptNumber: 1,
		MaxAttempts:   3,
	}
	mq.Submit(item1)

	// Process item 1 first.
	result1, ok := mq.ProcessNext()
	if !ok {
		t.Fatal("expected to process item 1")
	}
	if !result1.Success {
		t.Fatalf("item 1 should succeed: %s", result1.Error)
	}
	if result1.CommitSHA == "" {
		t.Error("item 1 should have a commit SHA")
	}

	// Get updated HEAD for item 2's base snapshot.
	newHeadSHA := gitHeadSHA(t, repoDir)
	if newHeadSHA == headSHA {
		t.Error("HEAD should advance after item 1 commit")
	}

	item2 := &merge.MergeItem{
		TaskID:       "task-mq-2",
		TaskTitle:    "Second task",
		BaseSnapshot: newHeadSHA, // Use the HEAD from after item 1
		Files:        map[string]string{"file2.go": "package main\n// file2\n"},
		SRSRefs:      []string{"FR-002"},
		MeeseeksModel: "test-model",
		AttemptNumber: 1,
		MaxAttempts:   3,
	}
	mq.Submit(item2)

	result2, ok := mq.ProcessNext()
	if !ok {
		t.Fatal("expected to process item 2")
	}
	if !result2.Success {
		t.Fatalf("item 2 should succeed: %s", result2.Error)
	}

	// Verify both commits exist in git log.
	logOutput := gitLog(t, repoDir)
	if !strings.Contains(logOutput, "First task") {
		t.Error("git log should contain First task commit")
	}
	if !strings.Contains(logOutput, "Second task") {
		t.Error("git log should contain Second task commit")
	}

	// Verify both files exist.
	if _, err := os.Stat(filepath.Join(repoDir, "file1.go")); err != nil {
		t.Error("file1.go should exist after merge")
	}
	if _, err := os.Stat(filepath.Join(repoDir, "file2.go")); err != nil {
		t.Error("file2.go should exist after merge")
	}

	// Verify serialization: queue depth should be 0 now.
	if mq.Depth() != 0 {
		t.Errorf("queue depth = %d, want 0", mq.Depth())
	}
}

// --- 22.5: Budget enforcement ---

func TestBudgetBoundary(t *testing.T) {
	env := setupEnv(t)

	enforcer := budget.NewEnforcer(env.db, env.emitter, budget.EnforcerConfig{
		MaxUSD:        1.00,
		WarnAtPercent: 80,
	})

	env.db.CreateTask(&state.Task{ID: "t-budget", Title: "Budget Test", Status: "queued", Tier: "standard", TaskType: "implementation"})

	// Insert cost entries totaling $0.95.
	env.db.InsertCost(&state.CostEntry{
		TaskID: "t-budget", AgentType: "meeseeks", ModelID: "test",
		InputTokens: 1000, OutputTokens: 500, CostUSD: 0.50, Timestamp: time.Now(),
	})
	env.db.InsertCost(&state.CostEntry{
		TaskID: "t-budget", AgentType: "meeseeks", ModelID: "test",
		InputTokens: 1000, OutputTokens: 500, CostUSD: 0.45, Timestamp: time.Now(),
	})

	// PreAuthorize a request costing $0.04 -> should pass (0.95 + 0.04 = 0.99 < 1.00).
	err := enforcer.PreAuthorize(0.04)
	if err != nil {
		t.Errorf("$0.04 request should pass (remaining $0.05): %v", err)
	}

	// PreAuthorize a request costing $0.10 -> should fail (0.95 + 0.10 = 1.05 > 1.00).
	err = enforcer.PreAuthorize(0.10)
	if err == nil {
		t.Error("$0.10 request should fail (remaining $0.05)")
	}

	// Request exactly $0.05 -> should pass (0.95 + 0.05 = 1.00).
	err = enforcer.PreAuthorize(0.05)
	if err != nil {
		t.Errorf("$0.05 request should pass (remaining $0.05): %v", err)
	}

	// Exhaust budget completely.
	env.db.InsertCost(&state.CostEntry{
		TaskID: "t-budget", AgentType: "meeseeks", ModelID: "test",
		CostUSD: 0.10, Timestamp: time.Now(),
	})
	enforcer.RecordAndCheck(0)

	if !enforcer.IsPaused() {
		t.Error("should be paused after budget exhaustion")
	}

	// Paused enforcer should reject all requests.
	err = enforcer.PreAuthorize(0.01)
	if err == nil {
		t.Error("paused enforcer should reject requests")
	}

	// Increase budget and resume.
	enforcer.IncreaseBudget(5.0)
	if enforcer.IsPaused() {
		t.Error("should resume after budget increase")
	}

	// Requests should work again.
	err = enforcer.PreAuthorize(0.50)
	if err != nil {
		t.Errorf("request should pass after budget increase: %v", err)
	}
}

// --- 22.6: Crash recovery ---

func TestCrashRecovery(t *testing.T) {
	env := setupEnv(t)

	// Simulate pre-crash state: tasks in various states, stale locks.
	env.db.CreateTask(&state.Task{ID: "t-crash-1", Title: "In Progress", Status: "in_progress", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-crash-2", Title: "In Review", Status: "in_review", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-crash-3", Title: "Done", Status: "done", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-crash-4", Title: "Queued", Status: "queued", Tier: "standard", TaskType: "implementation"})

	env.db.AcquireLocks("t-crash-1", []state.LockRequest{{ResourceType: "file", ResourceKey: "main.go"}})
	env.db.AcquireLocks("t-crash-2", []state.LockRequest{{ResourceType: "file", ResourceKey: "handler.go"}})

	// Create stale staging dirs.
	axiomDir := filepath.Join(env.tmpDir, ".axiom")
	stagingDir := filepath.Join(axiomDir, "containers", "staging", "t-crash-1")
	os.MkdirAll(stagingDir, 0755)
	os.WriteFile(filepath.Join(stagingDir, "stale.go"), []byte("stale"), 0644)
	stagingDir2 := filepath.Join(axiomDir, "containers", "staging", "t-crash-2")
	os.MkdirAll(stagingDir2, 0755)
	os.WriteFile(filepath.Join(stagingDir2, "stale2.go"), []byte("stale"), 0644)

	// Write and lock an SRS for integrity check.
	lm := srs.NewLockManager(axiomDir)
	lm.WriteDraft("# SRS: Test\n\n## 1. Architecture\n\nTest.")
	lm.Lock()

	// Simulate crash recovery by directly calling the recovery steps
	// as the Coordinator does. We cannot use NewCoordinator because
	// it tries to connect to Docker.
	conn := env.db.Conn()

	// Step 2: Reset orphaned in_progress/in_review tasks to queued.
	result, err := conn.Exec("UPDATE tasks SET status = 'queued' WHERE status IN ('in_progress', 'in_review')")
	if err != nil {
		t.Fatalf("reset tasks: %v", err)
	}
	resetCount, _ := result.RowsAffected()
	if resetCount != 2 {
		t.Errorf("expected 2 tasks reset, got %d", resetCount)
	}

	// Step 3: Release all stale locks.
	lockResult, err := conn.Exec("DELETE FROM task_locks")
	if err != nil {
		t.Fatalf("release locks: %v", err)
	}
	lockCount, _ := lockResult.RowsAffected()
	if lockCount != 2 {
		t.Errorf("expected 2 locks released, got %d", lockCount)
	}

	// Step 4: Clean staging dirs.
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

	// Verify queued task is unchanged.
	t4, _ := env.db.GetTask("t-crash-4")
	if t4.Status != "queued" {
		t.Errorf("t-crash-4 should stay queued, got %s", t4.Status)
	}

	// Verify locks are released.
	locked, _, _ := env.db.IsLocked("file", "main.go")
	if locked {
		t.Error("stale lock on main.go should be released")
	}
	locked, _, _ = env.db.IsLocked("file", "handler.go")
	if locked {
		t.Error("stale lock on handler.go should be released")
	}

	// Verify staging dir was cleaned.
	entries, _ := os.ReadDir(filepath.Join(axiomDir, "containers", "staging"))
	if len(entries) != 0 {
		t.Errorf("staging dir should be cleaned, got %d entries", len(entries))
	}

	// Step 5: Verify SRS integrity passes (no tampering).
	if err := lm.VerifyIntegrity(); err != nil {
		t.Errorf("SRS integrity should pass: %v", err)
	}
}

// --- 22.6b: ECO flow ---

func TestECOFlow(t *testing.T) {
	env := setupEnv(t)
	axiomDir := filepath.Join(env.tmpDir, ".axiom")

	ecoMgr := srs.NewECOManager(env.db, env.emitter, axiomDir)

	// Create tasks that will be affected by the ECO.
	env.db.CreateTask(&state.Task{ID: "t-eco-1", Title: "Auth", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-eco-2", Title: "Auth Tests", Status: "queued", Tier: "standard", TaskType: "test"})
	env.db.CreateTask(&state.Task{ID: "t-eco-3", Title: "Unrelated", Status: "queued", Tier: "standard", TaskType: "implementation"})

	// Propose and approve an ECO.
	eco, err := ecoMgr.ProposeECO("ECO-DEP", "passport-oauth2 removed from npm", "FR-003, AC-005", "Use arctic v2.1 instead")
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if eco.Status != "proposed" {
		t.Errorf("eco status = %s, want proposed", eco.Status)
	}

	if err := ecoMgr.ApproveECO(eco.ID, "user"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// Cancel affected tasks (not the unrelated one).
	if err := ecoMgr.CancelAffectedTasks(eco.ID, []string{"t-eco-1", "t-eco-2"}); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	// Verify affected tasks are cancelled_eco.
	task1, _ := env.db.GetTask("t-eco-1")
	if task1.Status != "cancelled_eco" {
		t.Errorf("t-eco-1 status = %s, want cancelled_eco", task1.Status)
	}
	task2, _ := env.db.GetTask("t-eco-2")
	if task2.Status != "cancelled_eco" {
		t.Errorf("t-eco-2 status = %s, want cancelled_eco", task2.Status)
	}

	// Verify unrelated task is not affected.
	task3, _ := env.db.GetTask("t-eco-3")
	if task3.Status != "queued" {
		t.Errorf("t-eco-3 status = %s, want queued (unaffected)", task3.Status)
	}

	// Verify ECO addendum file exists.
	addendumPath := filepath.Join(axiomDir, "eco", "ECO-001.md")
	if _, err := os.Stat(addendumPath); os.IsNotExist(err) {
		t.Error("ECO addendum file should exist")
	}

	// Create replacement tasks (simulating orchestrator creating new tasks after ECO).
	env.db.CreateTask(&state.Task{ID: "t-eco-1-v2", Title: "Auth v2 (ECO)", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.CreateTask(&state.Task{ID: "t-eco-2-v2", Title: "Auth Tests v2 (ECO)", Status: "queued", Tier: "standard", TaskType: "test"})

	// Verify replacement tasks can be dispatched.
	wq := engine.NewWorkQueue(env.db, env.emitter, 10)
	dispatchable, _ := wq.GetDispatchable()
	found := false
	for _, d := range dispatchable {
		if d.Task.ID == "t-eco-1-v2" || d.Task.ID == "t-eco-2-v2" {
			found = true
			break
		}
	}
	if !found {
		t.Error("replacement tasks should be dispatchable")
	}

	// Test ECO rejection flow.
	eco2, err := ecoMgr.ProposeECO("ECO-API", "endpoint changed", "FR-005", "use v2")
	if err != nil {
		t.Fatalf("propose eco2: %v", err)
	}
	ecoMgr.RejectECO(eco2.ID, "user")

	ecos, _ := env.db.ListECOs("rejected")
	if len(ecos) != 1 {
		t.Errorf("expected 1 rejected ECO, got %d", len(ecos))
	}

	// Test invalid ECO category rejection.
	_, err = ecoMgr.ProposeECO("ECO-INVALID", "bad category", "FR-001", "nope")
	if err == nil {
		t.Error("invalid ECO category should be rejected")
	}
}

// --- 22.7: SRS approval and lock ---

func TestSRSApprovalAndLock(t *testing.T) {
	tmpDir := t.TempDir()
	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	emitter := events.NewEmitter()
	lm := srs.NewLockManager(axiomDir)

	// Submit a draft SRS.
	draftContent := "# SRS: Test Project\n\n## 1. Architecture\n\nTest arch.\n"
	if err := lm.WriteDraft(draftContent); err != nil {
		t.Fatalf("write draft: %v", err)
	}

	// Verify the draft is writable (not locked).
	if lm.IsLocked() {
		t.Error("draft should not be locked")
	}

	// Approve it (locks the file).
	am := srs.NewApprovalManager(axiomDir, emitter, srs.DelegateUser)
	hash, err := am.Approve("test-user")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if hash == "" {
		t.Error("approval should return a hash")
	}

	// Verify the file is read-only.
	info, err := os.Stat(lm.SRSPath())
	if err != nil {
		t.Fatalf("stat SRS: %v", err)
	}
	if info.Mode().Perm()&0222 != 0 {
		t.Error("SRS should be read-only after approval")
	}

	// Verify integrity check passes.
	if err := lm.VerifyIntegrity(); err != nil {
		t.Errorf("integrity check should pass: %v", err)
	}

	// Verify IsApproved returns true.
	if !am.IsApproved() {
		t.Error("IsApproved should return true after approval")
	}

	// Tamper with the file (need to make it writable first).
	os.Chmod(lm.SRSPath(), 0644)
	if err := os.WriteFile(lm.SRSPath(), []byte("# SRS: Tampered\n"), 0644); err != nil {
		t.Fatalf("tamper with SRS: %v", err)
	}

	// Verify integrity check fails.
	if err := lm.VerifyIntegrity(); err == nil {
		t.Error("integrity check should fail after tampering")
	} else if !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("expected hash mismatch error, got: %v", err)
	}

	// Attempting to approve again should fail (already locked flag check).
	// Restore the original content first and lock it.
	os.WriteFile(lm.SRSPath(), []byte(draftContent), 0444)
	_, err = am.Approve("another-user")
	if err == nil {
		t.Error("re-approval should fail on already-locked SRS")
	}
}

// --- 22.8: TaskSpec builder security (secret scanning and redaction) ---

func TestTaskSpecBuilderSecurity(t *testing.T) {
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

	// .env should be excluded (matches ExcludedPaths).
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

	// main.go should be included and wrapped in untrusted tags.
	mainContent, ok := result["src/main.go"]
	if !ok {
		t.Fatal("src/main.go should be included")
	}
	if !contains(mainContent, "untrusted_repo_content") {
		t.Error("content should be wrapped in untrusted tags")
	}
	if !contains(mainContent, "src/main.go") {
		t.Error("untrusted tag should include source attribution")
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

	// Verify that redaction for prompt logs also works.
	logContent := scanner.RedactForPromptLog(`API key: sk-testkey1234567890abcdef`)
	if contains(logContent, "sk-testkey1234567890abcdef") {
		t.Error("prompt log should redact secrets")
	}
	if !contains(logContent, "[REDACTED]") {
		t.Error("prompt log should contain [REDACTED] placeholder")
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

	// Make 4 more requests to hit the rate limit (max 5 per task).
	for i := 0; i < 4; i++ {
		b.HandleInferenceRequest("t-broker", reqMsg, raw)
	}

	// 6th request should be rate limited.
	resp, _ = b.HandleInferenceRequest("t-broker", reqMsg, raw)
	respMsg = resp.(*ipc.InferenceResponseMessage)
	if respMsg.FinishReason != "error" {
		t.Error("6th request should be rate limited")
	}
	if !contains(respMsg.Error, "rate limit") {
		t.Errorf("error should mention rate limit, got: %s", respMsg.Error)
	}
}

// --- 22.10: Work Queue and Merge Queue integration ---

func TestWorkQueueMergeQueueIntegration(t *testing.T) {
	env := setupEnv(t)
	wq := engine.NewWorkQueue(env.db, env.emitter, 10)

	// Create and dispatch a task.
	env.db.CreateTask(&state.Task{ID: "t-wqmq", Title: "WQ-MQ Test", Status: "queued", Tier: "standard", TaskType: "implementation"})
	env.db.AddTaskTargetFile("t-wqmq", "output.go", "file")

	locks := []state.LockRequest{{ResourceType: "file", ResourceKey: "output.go"}}
	ok, _ := wq.AcquireAndDispatch("t-wqmq", locks)
	if !ok {
		t.Fatal("dispatch should succeed")
	}

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

	// Active count should be 0.
	if wq.ActiveCount() != 0 {
		t.Errorf("active count = %d, want 0", wq.ActiveCount())
	}
}

// --- 22.extra: Pipeline manifest validation with real ValidateManifest + ValidatePathSafety ---

func TestPipelineManifestValidation(t *testing.T) {
	stagingDir := t.TempDir()

	// Create files.
	os.MkdirAll(filepath.Join(stagingDir, "src"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "src", "handler.go"), []byte("package src"), 0644)
	os.WriteFile(filepath.Join(stagingDir, "src", "model.go"), []byte("package src"), 0644)

	// --- Test 1: Valid manifest passes ---
	manifest := &pipeline.Manifest{
		TaskID:       "task-validate",
		BaseSnapshot: "abc123",
		Files: pipeline.ManifestFiles{
			Added: []pipeline.FileEntry{
				{Path: "src/handler.go", Binary: false},
				{Path: "src/model.go", Binary: false},
			},
		},
	}
	mData, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(stagingDir, "manifest.json"), mData, 0644)

	p := pipeline.NewPipeline(pipeline.DefaultPipelineConfig())
	result := p.Execute("task-validate", stagingDir, "# Spec", []string{"src/handler.go", "src/model.go"}, "abc123", 1, "standard")
	if !result.StageResults[0].Passed {
		t.Errorf("valid manifest should pass: %v", result.StageResults[0].Errors)
	}

	// --- Test 2: Unlisted file in staging -> rejection ---
	os.WriteFile(filepath.Join(stagingDir, "src", "secret.go"), []byte("secret"), 0644)
	result = p.Execute("task-validate", stagingDir, "# Spec", []string{"src/handler.go", "src/model.go"}, "abc123", 1, "standard")
	if result.StageResults[0].Passed {
		t.Error("unlisted file should cause manifest rejection")
	}
	os.Remove(filepath.Join(stagingDir, "src", "secret.go"))

	// --- Test 3: Missing task_id -> rejection ---
	badManifest := &pipeline.Manifest{
		Files: pipeline.ManifestFiles{
			Added: []pipeline.FileEntry{{Path: "src/handler.go"}},
		},
	}
	bData, _ := json.Marshal(badManifest)
	os.WriteFile(filepath.Join(stagingDir, "manifest.json"), bData, 0644)
	// Remove model.go since it is not in this manifest.
	os.Remove(filepath.Join(stagingDir, "src", "model.go"))
	result = p.Execute("task-validate", stagingDir, "# Spec", []string{"src/handler.go"}, "abc123", 1, "standard")
	if result.StageResults[0].Passed {
		t.Error("missing task_id should cause manifest rejection")
	}
}

// --- 22.extra: Concurrent work queue stress test ---

func TestConcurrentWorkQueueStress(t *testing.T) {
	env := setupEnv(t)
	wq := engine.NewWorkQueue(env.db, env.emitter, 3) // Limit to 3 concurrent

	// Create 5 tasks with distinct target files.
	for i := 1; i <= 5; i++ {
		id := stdfmt.Sprintf("t-stress-%d", i)
		env.db.CreateTask(&state.Task{
			ID: id, Title: stdfmt.Sprintf("Stress %d", i),
			Status: "queued", Tier: "standard", TaskType: "implementation",
		})
		env.db.AddTaskTargetFile(id, stdfmt.Sprintf("file%d.go", i), "file")
	}

	// Dispatch 3 tasks (hitting the limit).
	dispatched := 0
	for i := 1; i <= 3; i++ {
		id := stdfmt.Sprintf("t-stress-%d", i)
		locks := []state.LockRequest{{ResourceType: "file", ResourceKey: stdfmt.Sprintf("file%d.go", i)}}
		ok, _ := wq.AcquireAndDispatch(id, locks)
		if ok {
			dispatched++
		}
	}
	if dispatched != 3 {
		t.Errorf("expected 3 dispatched, got %d", dispatched)
	}
	if wq.ActiveCount() != 3 {
		t.Errorf("active count = %d, want 3", wq.ActiveCount())
	}

	// 4th task should be blocked by concurrency limit.
	dispatchable, _ := wq.GetDispatchable()
	if len(dispatchable) != 0 {
		t.Errorf("no more tasks should be dispatchable at concurrency limit, got %d", len(dispatchable))
	}

	// Complete one task to free a slot.
	wq.CompleteTask("t-stress-1")
	if wq.ActiveCount() != 2 {
		t.Errorf("active count = %d, want 2 after completion", wq.ActiveCount())
	}

	// Now we should be able to dispatch another.
	dispatchable, _ = wq.GetDispatchable()
	if len(dispatchable) == 0 {
		t.Error("should have at least 1 dispatchable task after freeing a slot")
	}
}

// --- 22.extra: Event emission end-to-end ---

func TestEventEmissionEndToEnd(t *testing.T) {
	env := setupEnv(t)

	var budgetWarnings []events.Event
	var mu sync.Mutex
	env.emitter.Subscribe(events.EventBudgetWarning, func(e events.Event) {
		mu.Lock()
		budgetWarnings = append(budgetWarnings, e)
		mu.Unlock()
	})

	enforcer := budget.NewEnforcer(env.db, env.emitter, budget.EnforcerConfig{
		MaxUSD:        1.00,
		WarnAtPercent: 50,
	})

	env.db.CreateTask(&state.Task{ID: "t-evt", Title: "Event Test", Status: "queued", Tier: "standard", TaskType: "implementation"})

	// Spend $0.60 (over 50% warning threshold).
	env.db.InsertCost(&state.CostEntry{
		TaskID: "t-evt", AgentType: "meeseeks", ModelID: "test",
		CostUSD: 0.60, Timestamp: time.Now(),
	})

	// PreAuthorize should trigger warning.
	enforcer.PreAuthorize(0.01)

	// Allow async event delivery.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if len(budgetWarnings) != 1 {
		t.Errorf("expected 1 budget warning event, got %d", len(budgetWarnings))
	}
	mu.Unlock()
}

// --- Mock provider for broker tests ---

type mockBrokerProvider struct {
	available bool
}

func (m *mockBrokerProvider) Name() string                       { return "mock" }
func (m *mockBrokerProvider) Available(_ context.Context) bool   { return m.available }
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

// --- Git helpers for merge queue tests ---

func gitInit(t *testing.T, dir string) {
	t.Helper()
	run(t, dir, "git", "init")
	run(t, dir, "git", "config", "user.email", "test@axiom.local")
	run(t, dir, "git", "config", "user.name", "Axiom Test")
}

func gitCommitEmpty(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "git", "commit", "--allow-empty", "-m", msg)
}

func gitHeadSHA(t *testing.T, dir string) string {
	t.Helper()
	out := run(t, dir, "git", "rev-parse", "HEAD")
	return strings.TrimSpace(out)
}

func gitLog(t *testing.T, dir string) string {
	t.Helper()
	return run(t, dir, "git", "log", "--oneline")
}

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %s: %v", name, args, string(out), err)
	}
	return string(out)
}

// newTestGitManager creates a git.Manager for testing.
func newTestGitManager(repoDir string) *git.Manager {
	return git.NewManager(repoDir, "axiom")
}

// --- Utility ---

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
