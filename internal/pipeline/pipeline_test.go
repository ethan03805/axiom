package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupStagingDir creates a staging directory with a valid manifest and files.
func setupStagingDir(t *testing.T, taskID string, files map[string]string) string {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-pipeline-*")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	var added []FileEntry
	for name, content := range files {
		dir := filepath.Dir(filepath.Join(tmpDir, name))
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644)
		added = append(added, FileEntry{Path: name, Binary: false})
	}

	manifest := &Manifest{
		TaskID:       taskID,
		BaseSnapshot: "abc123",
		Files:        ManifestFiles{Added: added},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	return tmpDir
}

func TestPipelineAllStagesPass(t *testing.T) {
	stagingDir := setupStagingDir(t, "task-pass", map[string]string{
		"src/main.go": "package main",
	})

	p := NewPipeline(DefaultPipelineConfig())

	// Wire up all stages to pass.
	p.RunValidationFn = func(taskID, dir string) (*ValidationResult, error) {
		return &ValidationResult{CompilePass: true, LintPass: true, TestPass: true, TestCount: 5, TestPassed: 5}, nil
	}
	p.RunReviewFn = func(taskID, spec, output, valSummary string) (*ReviewVerdict, error) {
		return &ReviewVerdict{Verdict: "approve"}, nil
	}
	p.RunOrchestratorFn = func(taskID, output string) (bool, string, error) {
		return true, "", nil
	}
	p.SubmitToMergeQueueFn = func(taskID, dir, snapshot string) error {
		return nil
	}

	result := p.Execute("task-pass", stagingDir, "# TaskSpec", []string{"src/main.go"}, "abc123", 1, "standard")

	if !result.Approved {
		t.Error("expected pipeline to approve")
		for _, sr := range result.StageResults {
			if !sr.Passed {
				t.Errorf("stage %s failed: %v", sr.Stage, sr.Errors)
			}
		}
	}
	if len(result.StageResults) != 5 {
		t.Errorf("expected 5 stage results, got %d", len(result.StageResults))
	}
	for _, sr := range result.StageResults {
		if !sr.Passed {
			t.Errorf("stage %s should have passed", sr.Stage)
		}
	}
}

func TestPipelineManifestValidationFails(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-pipeline-mfail-*")
	defer os.RemoveAll(tmpDir)

	// Write a manifest referencing a file that doesn't exist.
	manifest := &Manifest{
		TaskID: "task-mfail",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "missing.go", Binary: false}},
		},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	p := NewPipeline(DefaultPipelineConfig())
	result := p.Execute("task-mfail", tmpDir, "# TaskSpec", []string{"missing.go"}, "abc123", 1, "standard")

	if result.Approved {
		t.Error("expected pipeline to reject due to manifest validation")
	}
	if !result.ShouldRetry {
		t.Error("expected shouldRetry for first attempt failure")
	}
	if len(result.StageResults) != 1 {
		t.Errorf("expected 1 stage result (should stop at stage 1), got %d", len(result.StageResults))
	}
	if result.StageResults[0].Stage != "manifest_validation" {
		t.Errorf("expected manifest_validation stage, got %s", result.StageResults[0].Stage)
	}
}

func TestPipelineValidationSandboxFails(t *testing.T) {
	stagingDir := setupStagingDir(t, "task-vfail", map[string]string{
		"src/main.go": "package main // syntax error {{{",
	})

	p := NewPipeline(DefaultPipelineConfig())
	p.RunValidationFn = func(taskID, dir string) (*ValidationResult, error) {
		return &ValidationResult{
			CompilePass:  false,
			CompileError: "syntax error at line 1",
			LintPass:     true,
			TestPass:     true,
		}, nil
	}

	result := p.Execute("task-vfail", stagingDir, "# TaskSpec", []string{"src/main.go"}, "abc123", 1, "standard")

	if result.Approved {
		t.Error("expected rejection due to compilation failure")
	}
	if len(result.StageResults) != 2 {
		t.Errorf("expected 2 stages (manifest + validation), got %d", len(result.StageResults))
	}
	if result.StageResults[1].Stage != "validation_sandbox" {
		t.Errorf("expected validation_sandbox stage, got %s", result.StageResults[1].Stage)
	}
	if result.StageResults[1].Passed {
		t.Error("validation stage should have failed")
	}
}

func TestPipelineReviewerRejects(t *testing.T) {
	stagingDir := setupStagingDir(t, "task-rrej", map[string]string{
		"src/main.go": "package main",
	})

	p := NewPipeline(DefaultPipelineConfig())
	p.RunValidationFn = func(taskID, dir string) (*ValidationResult, error) {
		return &ValidationResult{CompilePass: true, LintPass: true, TestPass: true}, nil
	}
	p.RunReviewFn = func(taskID, spec, output, valSummary string) (*ReviewVerdict, error) {
		return &ReviewVerdict{
			Verdict:  "reject",
			Feedback: "Missing error handling on line 42",
		}, nil
	}

	result := p.Execute("task-rrej", stagingDir, "# TaskSpec", []string{"src/main.go"}, "abc123", 1, "standard")

	if result.Approved {
		t.Error("expected rejection by reviewer")
	}
	if len(result.StageResults) != 3 {
		t.Errorf("expected 3 stages, got %d", len(result.StageResults))
	}
	if result.StageResults[2].Stage != "reviewer_evaluation" {
		t.Errorf("expected reviewer_evaluation stage, got %s", result.StageResults[2].Stage)
	}
	if !strings.Contains(result.Feedback, "Missing error handling") {
		t.Errorf("feedback should contain reviewer feedback, got: %s", result.Feedback)
	}
}

func TestPipelineOrchestratorRejects(t *testing.T) {
	stagingDir := setupStagingDir(t, "task-orej", map[string]string{
		"src/main.go": "package main",
	})

	p := NewPipeline(DefaultPipelineConfig())
	p.RunValidationFn = func(taskID, dir string) (*ValidationResult, error) {
		return &ValidationResult{CompilePass: true, LintPass: true, TestPass: true}, nil
	}
	p.RunReviewFn = func(taskID, spec, output, valSummary string) (*ReviewVerdict, error) {
		return &ReviewVerdict{Verdict: "approve"}, nil
	}
	p.RunOrchestratorFn = func(taskID, output string) (bool, string, error) {
		return false, "Does not satisfy FR-001", nil
	}

	result := p.Execute("task-orej", stagingDir, "# TaskSpec", []string{"src/main.go"}, "abc123", 1, "standard")

	if result.Approved {
		t.Error("expected orchestrator rejection")
	}
	if len(result.StageResults) != 4 {
		t.Errorf("expected 4 stages, got %d", len(result.StageResults))
	}
}

func TestRetryLogic(t *testing.T) {
	config := PipelineConfig{MaxRetriesPerTier: 3, MaxEscalations: 2}
	p := NewPipeline(config)

	// Attempt 1: should retry.
	result := &PipelineResult{AttemptNumber: 1, Tier: "standard"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldRetry {
		t.Error("attempt 1 should retry")
	}

	// Attempt 2: should retry.
	result = &PipelineResult{AttemptNumber: 2, Tier: "standard"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldRetry {
		t.Error("attempt 2 should retry")
	}

	// Attempt 3: retries exhausted, should escalate.
	result = &PipelineResult{AttemptNumber: 3, Tier: "standard"}
	p.setRetryOrEscalate(result, "error")
	if result.ShouldRetry {
		t.Error("attempt 3 should not retry")
	}
	if !result.ShouldEscalate {
		t.Error("attempt 3 should escalate")
	}

	// At premium tier with exhausted retries: should block.
	result = &PipelineResult{AttemptNumber: 3, Tier: "premium"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldBlock {
		t.Error("premium with exhausted retries should block")
	}
}

func TestEscalationTierOrder(t *testing.T) {
	config := PipelineConfig{MaxRetriesPerTier: 1, MaxEscalations: 2}
	p := NewPipeline(config)

	// Local tier, retries exhausted: should escalate (can go to cheap).
	result := &PipelineResult{AttemptNumber: 1, Tier: "local"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldEscalate {
		t.Error("local should escalate")
	}

	// Cheap tier: should escalate to standard.
	result = &PipelineResult{AttemptNumber: 1, Tier: "cheap"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldEscalate {
		t.Error("cheap should escalate")
	}

	// Standard tier: should escalate to premium.
	result = &PipelineResult{AttemptNumber: 1, Tier: "standard"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldEscalate {
		t.Error("standard should escalate")
	}

	// Premium tier: cannot escalate, should block.
	result = &PipelineResult{AttemptNumber: 1, Tier: "premium"}
	p.setRetryOrEscalate(result, "error")
	if !result.ShouldBlock {
		t.Error("premium should block (no further escalation)")
	}
}

func TestGenerateReviewSpec(t *testing.T) {
	spec := GenerateReviewSpec("task-042", "# TaskSpec content", `{"files":{}}`, "All checks passed")

	if !strings.Contains(spec, "# ReviewSpec: task-042") {
		t.Error("ReviewSpec should contain task ID header")
	}
	if !strings.Contains(spec, "# TaskSpec content") {
		t.Error("ReviewSpec should contain original TaskSpec")
	}
	if !strings.Contains(spec, "All checks passed") {
		t.Error("ReviewSpec should contain validation summary")
	}
	if !strings.Contains(spec, "APPROVE | REJECT") {
		t.Error("ReviewSpec should contain verdict instructions")
	}
}

func TestGenerateBatchReviewSpec(t *testing.T) {
	taskIDs := []string{"t1", "t2"}
	specs := map[string]string{"t1": "spec1", "t2": "spec2"}
	outputs := map[string]string{"t1": "output1", "t2": "output2"}

	spec := GenerateBatchReviewSpec(taskIDs, specs, outputs, "All passed")

	if !strings.Contains(spec, "Batched ReviewSpec") {
		t.Error("should be a batched review spec")
	}
	if !strings.Contains(spec, "t1, t2") {
		t.Error("should list task IDs")
	}
	if !strings.Contains(spec, "entire batch is returned") {
		t.Error("should explain batch rejection rules")
	}
}

func TestMergeQueueFailure(t *testing.T) {
	stagingDir := setupStagingDir(t, "task-mqfail", map[string]string{
		"src/main.go": "package main",
	})

	p := NewPipeline(DefaultPipelineConfig())
	p.RunValidationFn = func(taskID, dir string) (*ValidationResult, error) {
		return &ValidationResult{CompilePass: true, LintPass: true, TestPass: true}, nil
	}
	p.RunReviewFn = func(taskID, spec, output, valSummary string) (*ReviewVerdict, error) {
		return &ReviewVerdict{Verdict: "approve"}, nil
	}
	p.RunOrchestratorFn = func(taskID, output string) (bool, string, error) {
		return true, "", nil
	}
	p.SubmitToMergeQueueFn = func(taskID, dir, snapshot string) error {
		return fmt.Errorf("merge conflict with HEAD")
	}

	result := p.Execute("task-mqfail", stagingDir, "# TaskSpec", []string{"src/main.go"}, "abc123", 1, "standard")

	if result.Approved {
		t.Error("expected rejection due to merge queue failure")
	}
	if len(result.StageResults) != 5 {
		t.Errorf("expected 5 stages, got %d", len(result.StageResults))
	}
	if result.StageResults[4].Passed {
		t.Error("merge queue stage should have failed")
	}
}

func TestValidationResultSummary(t *testing.T) {
	v := &ValidationResult{
		CompilePass:  true,
		LintPass:     true,
		LintWarnings: []string{"unused var"},
		TestPass:     true,
		TestCount:    12,
		TestPassed:   12,
	}

	summary := v.Summary()
	if !strings.Contains(summary, "Compilation: PASS") {
		t.Error("summary should show compile pass")
	}
	if !strings.Contains(summary, "1 warnings") {
		t.Error("summary should show lint warnings count")
	}
	if !strings.Contains(summary, "12/12") {
		t.Error("summary should show test counts")
	}
}
