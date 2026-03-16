package pipeline

import (
	"fmt"
	"strings"
	"time"
)

// StageResult holds the outcome of a single pipeline stage.
type StageResult struct {
	Stage    string
	Passed   bool
	Errors   []string
	Feedback string        // Structured feedback for retry
	Duration time.Duration
}

// PipelineResult holds the final result of running all pipeline stages.
type PipelineResult struct {
	TaskID        string
	Approved      bool
	StageResults  []*StageResult
	AttemptNumber int
	Tier          string
	ShouldRetry   bool   // True if the task should be retried at the same tier
	ShouldEscalate bool  // True if retries are exhausted and escalation is needed
	ShouldBlock   bool   // True if escalation is exhausted
	Feedback      string // Aggregated feedback for retry/escalation
}

// PipelineConfig holds the configuration for pipeline execution.
type PipelineConfig struct {
	MaxRetriesPerTier int // Maximum retries at the same model tier (default 3)
	MaxEscalations    int // Maximum model tier escalations (default 2)
	MaxFileSize       int64 // Maximum file size in bytes (default 1MB)
}

// DefaultPipelineConfig returns the default pipeline configuration.
// See Architecture Section 5.1 step 7 and Section 30.1.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{
		MaxRetriesPerTier: 3,
		MaxEscalations:    2,
		MaxFileSize:       1 * 1024 * 1024,
	}
}

// ValidationResult holds the outcome of running the validation sandbox.
type ValidationResult struct {
	CompilePass  bool
	CompileError string
	LintPass     bool
	LintError    string
	LintWarnings []string
	TestPass     bool
	TestError    string
	TestCount    int
	TestPassed   int
	SecurityPass bool
	SecurityError string
}

// Passed returns true if all validation checks passed.
func (v *ValidationResult) Passed() bool {
	return v.CompilePass && v.LintPass && v.TestPass
}

// Summary returns a human-readable summary of the validation result.
func (v *ValidationResult) Summary() string {
	var parts []string
	if v.CompilePass {
		parts = append(parts, "Compilation: PASS")
	} else {
		parts = append(parts, "Compilation: FAIL - "+v.CompileError)
	}
	if v.LintPass {
		warnings := ""
		if len(v.LintWarnings) > 0 {
			warnings = fmt.Sprintf(" (%d warnings)", len(v.LintWarnings))
		}
		parts = append(parts, "Linting: PASS"+warnings)
	} else {
		parts = append(parts, "Linting: FAIL - "+v.LintError)
	}
	if v.TestPass {
		parts = append(parts, fmt.Sprintf("Unit Tests: PASS (%d/%d)", v.TestPassed, v.TestCount))
	} else {
		parts = append(parts, "Unit Tests: FAIL - "+v.TestError)
	}
	return strings.Join(parts, "\n")
}

// ReviewVerdict represents the outcome of a reviewer evaluation.
type ReviewVerdict struct {
	Verdict    string // "approve" | "reject"
	Feedback   string
	Evaluation string // Per-criterion evaluation
}

// Pipeline orchestrates the 5-stage approval process from Meeseeks output
// to merge queue submission. Each stage must pass before proceeding.
//
// See Architecture Section 14.2 for the complete stage specification.
//
// The Pipeline is designed to be called by the engine's execution loop.
// External dependencies (container spawning, IPC, etc.) are injected via
// callback interfaces to keep the pipeline logic testable.
type Pipeline struct {
	config PipelineConfig

	// Stage callbacks. The engine wires these to the actual subsystems.
	ValidateManifestFn  func(stagingDir string, taskTargetFiles []string) ([]string, error)
	RunValidationFn     func(taskID, stagingDir string) (*ValidationResult, error)
	RunReviewFn         func(taskID, taskSpec, output, validationSummary string) (*ReviewVerdict, error)
	RunOrchestratorFn   func(taskID, output string) (bool, string, error)
	SubmitToMergeQueueFn func(taskID, stagingDir, baseSnapshot string) error
}

// NewPipeline creates a Pipeline with the given configuration.
func NewPipeline(config PipelineConfig) *Pipeline {
	return &Pipeline{config: config}
}

// Execute runs the full 5-stage approval pipeline for a task's output.
//
// Parameters:
//   - taskID: the task being processed
//   - stagingDir: path to the Meeseeks' staging directory
//   - taskSpec: the original TaskSpec content (for ReviewSpec generation)
//   - taskTargetFiles: the task's declared target files (for scope checking)
//   - baseSnapshot: git SHA the TaskSpec was built against
//   - attemptNumber: current attempt number (for retry tracking)
//   - tier: current model tier (for escalation tracking)
//
// Returns a PipelineResult indicating whether the output was approved
// and whether retry/escalation/blocking is needed.
func (p *Pipeline) Execute(
	taskID, stagingDir, taskSpec string,
	taskTargetFiles []string,
	baseSnapshot string,
	attemptNumber int, tier string,
) *PipelineResult {
	result := &PipelineResult{
		TaskID:        taskID,
		AttemptNumber: attemptNumber,
		Tier:          tier,
	}

	// --- Stage 1: Extraction & Manifest Validation ---
	stage1Start := time.Now()
	manifestErrors, err := p.runManifestValidation(stagingDir, taskTargetFiles)
	stage1Result := &StageResult{
		Stage:    "manifest_validation",
		Duration: time.Since(stage1Start),
	}
	if err != nil {
		stage1Result.Passed = false
		stage1Result.Errors = []string{err.Error()}
		stage1Result.Feedback = "Manifest validation error: " + err.Error()
		result.StageResults = append(result.StageResults, stage1Result)
		p.setRetryOrEscalate(result, stage1Result.Feedback)
		return result
	}
	if len(manifestErrors) > 0 {
		stage1Result.Passed = false
		stage1Result.Errors = manifestErrors
		stage1Result.Feedback = "Manifest validation failed:\n- " + strings.Join(manifestErrors, "\n- ")
		result.StageResults = append(result.StageResults, stage1Result)
		p.setRetryOrEscalate(result, stage1Result.Feedback)
		return result
	}
	stage1Result.Passed = true
	result.StageResults = append(result.StageResults, stage1Result)

	// --- Stage 2: Validation Sandbox ---
	stage2Start := time.Now()
	stage2Result := &StageResult{
		Stage:    "validation_sandbox",
		Duration: time.Since(stage2Start),
	}
	if p.RunValidationFn != nil {
		valResult, err := p.RunValidationFn(taskID, stagingDir)
		stage2Result.Duration = time.Since(stage2Start)
		if err != nil {
			stage2Result.Passed = false
			stage2Result.Errors = []string{err.Error()}
			stage2Result.Feedback = "Validation sandbox error: " + err.Error()
			result.StageResults = append(result.StageResults, stage2Result)
			p.setRetryOrEscalate(result, stage2Result.Feedback)
			return result
		}
		if !valResult.Passed() {
			stage2Result.Passed = false
			stage2Result.Feedback = "Validation failed:\n" + valResult.Summary()
			stage2Result.Errors = []string{valResult.Summary()}
			result.StageResults = append(result.StageResults, stage2Result)
			p.setRetryOrEscalate(result, stage2Result.Feedback)
			return result
		}
		stage2Result.Passed = true
	} else {
		stage2Result.Passed = true // Skip if no validation function configured
	}
	result.StageResults = append(result.StageResults, stage2Result)

	// --- Stage 3: Reviewer Evaluation ---
	stage3Start := time.Now()
	stage3Result := &StageResult{
		Stage: "reviewer_evaluation",
	}
	if p.RunReviewFn != nil {
		validationSummary := ""
		if len(result.StageResults) >= 2 {
			validationSummary = "All checks passed"
		}
		verdict, err := p.RunReviewFn(taskID, taskSpec, stagingDir, validationSummary)
		stage3Result.Duration = time.Since(stage3Start)
		if err != nil {
			stage3Result.Passed = false
			stage3Result.Errors = []string{err.Error()}
			stage3Result.Feedback = "Review error: " + err.Error()
			result.StageResults = append(result.StageResults, stage3Result)
			p.setRetryOrEscalate(result, stage3Result.Feedback)
			return result
		}
		if verdict.Verdict != "approve" {
			stage3Result.Passed = false
			stage3Result.Feedback = "Reviewer rejected:\n" + verdict.Feedback
			stage3Result.Errors = []string{verdict.Feedback}
			result.StageResults = append(result.StageResults, stage3Result)
			p.setRetryOrEscalate(result, stage3Result.Feedback)
			return result
		}
		stage3Result.Passed = true
	} else {
		stage3Result.Passed = true
		stage3Result.Duration = time.Since(stage3Start)
	}
	result.StageResults = append(result.StageResults, stage3Result)

	// --- Stage 4: Orchestrator Validation ---
	stage4Start := time.Now()
	stage4Result := &StageResult{
		Stage: "orchestrator_validation",
	}
	if p.RunOrchestratorFn != nil {
		approved, feedback, err := p.RunOrchestratorFn(taskID, stagingDir)
		stage4Result.Duration = time.Since(stage4Start)
		if err != nil {
			stage4Result.Passed = false
			stage4Result.Errors = []string{err.Error()}
			stage4Result.Feedback = "Orchestrator validation error: " + err.Error()
			result.StageResults = append(result.StageResults, stage4Result)
			p.setRetryOrEscalate(result, stage4Result.Feedback)
			return result
		}
		if !approved {
			stage4Result.Passed = false
			stage4Result.Feedback = "Orchestrator rejected:\n" + feedback
			stage4Result.Errors = []string{feedback}
			result.StageResults = append(result.StageResults, stage4Result)
			p.setRetryOrEscalate(result, stage4Result.Feedback)
			return result
		}
		stage4Result.Passed = true
	} else {
		stage4Result.Passed = true
		stage4Result.Duration = time.Since(stage4Start)
	}
	result.StageResults = append(result.StageResults, stage4Result)

	// --- Stage 5: Submit to Merge Queue ---
	stage5Start := time.Now()
	stage5Result := &StageResult{
		Stage: "merge_queue",
	}
	if p.SubmitToMergeQueueFn != nil {
		if err := p.SubmitToMergeQueueFn(taskID, stagingDir, baseSnapshot); err != nil {
			stage5Result.Duration = time.Since(stage5Start)
			stage5Result.Passed = false
			stage5Result.Errors = []string{err.Error()}
			stage5Result.Feedback = "Merge queue submission failed: " + err.Error()
			result.StageResults = append(result.StageResults, stage5Result)
			p.setRetryOrEscalate(result, stage5Result.Feedback)
			return result
		}
	}
	stage5Result.Duration = time.Since(stage5Start)
	stage5Result.Passed = true
	result.StageResults = append(result.StageResults, stage5Result)

	// All stages passed.
	result.Approved = true
	return result
}

// runManifestValidation performs Stage 1 using the configured function or default.
func (p *Pipeline) runManifestValidation(stagingDir string, taskTargetFiles []string) ([]string, error) {
	if p.ValidateManifestFn != nil {
		return p.ValidateManifestFn(stagingDir, taskTargetFiles)
	}

	// Default implementation using the manifest and router packages.
	manifest, err := ParseManifest(stagingDir)
	if err != nil {
		return nil, err
	}

	// Validate manifest completeness.
	errs := ValidateManifest(manifest, stagingDir)

	// Validate path safety.
	allowedPaths := BuildAllowedPaths(taskTargetFiles)
	routerConfig := RouterConfig{MaxFileSize: p.config.MaxFileSize}
	if routerConfig.MaxFileSize == 0 {
		routerConfig.MaxFileSize = DefaultRouterConfig().MaxFileSize
	}
	pathErrs := ValidatePathSafety(manifest, stagingDir, allowedPaths, routerConfig)
	errs = append(errs, pathErrs...)

	return errs, nil
}

// setRetryOrEscalate determines whether the pipeline failure should result
// in a retry (same tier), escalation (next tier), or blocking.
// See Architecture Section 30.1 for the escalation policy.
func (p *Pipeline) setRetryOrEscalate(result *PipelineResult, feedback string) {
	result.Feedback = feedback

	// Calculate how many retries have been done at this tier.
	// attemptNumber counts from 1 within the current tier.
	if result.AttemptNumber < p.config.MaxRetriesPerTier {
		result.ShouldRetry = true
		return
	}

	// Retries exhausted at this tier. Check escalation.
	tierOrder := []string{"local", "cheap", "standard", "premium"}
	currentIdx := -1
	for i, t := range tierOrder {
		if t == result.Tier {
			currentIdx = i
			break
		}
	}

	if currentIdx >= 0 && currentIdx < len(tierOrder)-1 {
		// Can still escalate to a higher tier.
		result.ShouldEscalate = true
		return
	}

	// Cannot escalate further. Block the task.
	result.ShouldBlock = true
}

// GenerateReviewSpec builds a ReviewSpec combining the original TaskSpec,
// Meeseeks output, manifest, and validation results.
// See Architecture Section 11.7 for the ReviewSpec format.
func GenerateReviewSpec(taskID, taskSpec, manifestJSON, validationSummary string) string {
	return fmt.Sprintf(`# ReviewSpec: %s

## Original TaskSpec
%s

## Meeseeks Output
See staged files in the output directory.
Manifest:
%s

## Automated Check Results
%s

## Review Instructions
Evaluate the Meeseeks' output against the original TaskSpec.
For each acceptance criterion, determine if it is satisfied.

Check for:
- Correctness against acceptance criteria
- Interface contract compliance
- Obvious bugs, edge cases, or security issues
- Code quality and style compliance

Respond in the following format:

### Verdict: APPROVE | REJECT

### Criterion Evaluation
- [ ] Each criterion: PASS | FAIL - explanation

### Feedback (if REJECT)
Specific, actionable feedback with line numbers.
`, taskID, taskSpec, manifestJSON, validationSummary)
}

// BatchReviewSpec generates a single ReviewSpec for a batch of related
// local-tier tasks. Used for batched review per Architecture Section 14.3.
func GenerateBatchReviewSpec(taskIDs []string, taskSpecs, outputs map[string]string, validationSummary string) string {
	var sections strings.Builder
	for _, id := range taskIDs {
		sections.WriteString(fmt.Sprintf("\n### Task: %s\n\n", id))
		sections.WriteString("#### TaskSpec\n")
		sections.WriteString(taskSpecs[id])
		sections.WriteString("\n\n#### Output\n")
		sections.WriteString(outputs[id])
		sections.WriteString("\n\n---\n")
	}

	return fmt.Sprintf(`# Batched ReviewSpec

## Tasks in Batch
%s

## Combined Outputs
%s

## Automated Check Results
%s

## Review Instructions
Review all tasks in the batch as a coherent unit.
All tasks are functionally related. If ANY task fails review,
the entire batch is returned for revision.

### Verdict: APPROVE | REJECT
### Feedback (if REJECT)
`, strings.Join(taskIDs, ", "), sections.String(), validationSummary)
}
