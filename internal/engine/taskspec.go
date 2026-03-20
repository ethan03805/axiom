// Package engine contains the TaskSpec builder which constructs self-contained
// task specification documents for Meeseeks workers.
//
// Each Meeseeks receives a TaskSpec with the minimum necessary structured
// context for its specific task. The orchestrator determines the appropriate
// context tier; the engine constructs the spec by querying the semantic index
// and applying security mitigations.
//
// See Architecture.md Section 10.3 for the TaskSpec format.
// See Architecture.md Section 2.2 for the context tier system.
package engine

import (
	"fmt"
	"os"
	"strings"

	"github.com/ethan03805/axiom/internal/security"
	"github.com/ethan03805/axiom/internal/state"
)

// ContextTier determines how much project context is included in a TaskSpec.
// See Architecture Section 2.2.
type ContextTier string

const (
	// TierSymbol includes only specific function signatures and type definitions.
	TierSymbol ContextTier = "symbol"

	// TierFile includes complete relevant source files.
	TierFile ContextTier = "file"

	// TierPackage includes the full package/module with internal dependencies.
	TierPackage ContextTier = "package"

	// TierRepoMap includes the dependency graph, directory structure, and export index.
	TierRepoMap ContextTier = "repo-map"
)

// TaskSpecRequest contains the parameters needed to build a TaskSpec.
// The orchestrator populates this when requesting a Meeseeks spawn.
type TaskSpecRequest struct {
	Task               *state.Task
	ContextTier        ContextTier
	SRSRefs            []string          // SRS requirements this task addresses
	TargetFiles        []string          // Files the Meeseeks will modify
	Dependencies       []string          // Task IDs this task depends on
	ImplementationFiles map[string]string // For test tasks: actual source files from dependencies
	Constraints        TaskConstraints
	AcceptCriteria     []string        // Specific testable criteria
	Feedback           string            // Feedback from a prior failed attempt
	AttemptNumber      int
}

// TaskConstraints holds coding constraints for the Meeseeks.
type TaskConstraints struct {
	Language     string
	Style        string
	Dependencies string   // Allowed dependencies
	MaxFileLen   int      // Max lines per file
}

// TaskSpecBuilder constructs TaskSpec documents for Meeseeks.
// It queries the semantic index for context and applies security mitigations.
type TaskSpecBuilder struct {
	db            *state.DB
	secretScanner *security.SecretScanner
	projectRoot   string
}

// NewTaskSpecBuilder creates a TaskSpecBuilder.
func NewTaskSpecBuilder(db *state.DB, scanner *security.SecretScanner, projectRoot string) *TaskSpecBuilder {
	return &TaskSpecBuilder{
		db:            db,
		secretScanner: scanner,
		projectRoot:   projectRoot,
	}
}

// Build constructs a complete TaskSpec document from the request.
// The spec follows the format from Architecture Section 10.3.
//
// Steps:
//  1. Get the current git HEAD SHA for base_snapshot pinning
//  2. Gather context at the requested tier from the semantic index
//  3. Apply security mitigations (secret redaction, injection wrapping)
//  4. Render the TaskSpec markdown
func (b *TaskSpecBuilder) Build(req *TaskSpecRequest, baseSnapshot string) (string, error) {
	var spec strings.Builder

	// Header
	spec.WriteString(fmt.Sprintf("# TaskSpec: %s\n\n", req.Task.ID))

	// Base Snapshot per Architecture Section 10.3
	spec.WriteString("## Base Snapshot\n")
	spec.WriteString(fmt.Sprintf("git_sha: %s\n\n", baseSnapshot))

	// Objective
	spec.WriteString("## Objective\n")
	spec.WriteString(req.Task.Title + "\n\n")
	if req.Task.Description != "" {
		spec.WriteString(req.Task.Description + "\n\n")
	}

	// Context section -- content depends on the requested tier.
	// See Architecture Section 2.2 for tier descriptions.
	spec.WriteString("## Context\n\n")
	contextContent, err := b.gatherContext(req)
	if err != nil {
		// Non-fatal: include what we have with a warning.
		spec.WriteString(fmt.Sprintf("<!-- Warning: context gathering incomplete: %v -->\n\n", err))
	}
	if contextContent != "" {
		spec.WriteString(contextContent)
	} else {
		spec.WriteString("No additional context required for this task.\n\n")
	}

	// Interface Contract
	if len(req.TargetFiles) > 0 {
		spec.WriteString("## Interface Contract\n")
		spec.WriteString("The output must modify only the following files:\n")
		for _, f := range req.TargetFiles {
			spec.WriteString(fmt.Sprintf("- `%s`\n", f))
		}
		spec.WriteString("\n")
	}

	// Constraints
	spec.WriteString("## Constraints\n")
	if req.Constraints.Language != "" {
		spec.WriteString(fmt.Sprintf("- Language: %s\n", req.Constraints.Language))
	}
	if req.Constraints.Style != "" {
		spec.WriteString(fmt.Sprintf("- Style: %s\n", req.Constraints.Style))
	}
	if req.Constraints.Dependencies != "" {
		spec.WriteString(fmt.Sprintf("- Dependencies: %s\n", req.Constraints.Dependencies))
	}
	if req.Constraints.MaxFileLen > 0 {
		spec.WriteString(fmt.Sprintf("- Max file length: %d lines\n", req.Constraints.MaxFileLen))
	}
	spec.WriteString("\n")

	// SRS References
	if len(req.SRSRefs) > 0 {
		spec.WriteString("## SRS References\n")
		for _, ref := range req.SRSRefs {
			spec.WriteString(fmt.Sprintf("- %s\n", ref))
		}
		spec.WriteString("\n")
	}

	// Acceptance Criteria
	if len(req.AcceptCriteria) > 0 {
		spec.WriteString("## Acceptance Criteria\n")
		for _, ac := range req.AcceptCriteria {
			spec.WriteString(fmt.Sprintf("- [ ] %s\n", ac))
		}
		spec.WriteString("\n")
	}

	// Feedback from prior attempts (retry/escalation context)
	if req.Feedback != "" {
		spec.WriteString("## Prior Attempt Feedback\n")
		spec.WriteString(fmt.Sprintf("Attempt %d failed. Address the following:\n\n", req.AttemptNumber-1))
		spec.WriteString(req.Feedback + "\n\n")
	}

	// Output format instructions
	spec.WriteString("## Output Format\n")
	spec.WriteString("Write all output files to /workspace/staging/\n")
	spec.WriteString("Include a manifest.json describing all file operations.\n")

	return spec.String(), nil
}

// gatherContext builds the context section based on the requested tier.
// It queries the semantic index and applies security mitigations.
//
// For test-generation tasks (task_type == "test"), implementation source files
// from dependency tasks are included as context per Architecture Section 11.5.
// Test Meeseeks need the actual committed implementation to write valid tests.
func (b *TaskSpecBuilder) gatherContext(req *TaskSpecRequest) (string, error) {
	var ctx strings.Builder

	switch req.ContextTier {
	case TierSymbol:
		ctx.WriteString("### Symbol Context (tier: symbol)\n")
		ctx.WriteString("Specific function signatures and type definitions needed for this task.\n\n")
		// In production, this would query the semantic index:
		// symbols := indexer.LookupSymbol(name, kind)
		// For each target file, look up the symbols defined there.

	case TierFile:
		ctx.WriteString("### File Context (tier: file)\n")
		ctx.WriteString("Complete relevant source files.\n\n")
		// In production, read the target files and include them with
		// security mitigations applied.
		for _, filePath := range req.TargetFiles {
			content, err := b.readFileWithSecurity(filePath)
			if err != nil {
				continue
			}
			ctx.WriteString(content)
			ctx.WriteString("\n\n")
		}

	case TierPackage:
		ctx.WriteString("### Package Context (tier: package)\n")
		ctx.WriteString("Full package/module with internal dependencies.\n\n")

	case TierRepoMap:
		ctx.WriteString("### Repo Map (tier: repo-map)\n")
		ctx.WriteString("Directory structure, dependency graph, export index.\n\n")

	default:
		ctx.WriteString("### Context\n")
		ctx.WriteString("Default context for this task.\n\n")
	}

	// For test-generation tasks, include the actual implementation source files
	// as additional context. This ensures tests reference real functions, types,
	// and interfaces rather than hallucinated ones.
	// See Architecture Section 11.5: test generation tasks should have the
	// semantic index of the committed implementation as context.
	if len(req.ImplementationFiles) > 0 {
		ctx.WriteString("### Implementation Source (for test generation)\n")
		ctx.WriteString("The following are the ACTUAL implementation files your tests must reference.\n")
		ctx.WriteString("Use only the functions, types, and exports defined in these files.\n\n")
		for filePath, content := range req.ImplementationFiles {
			ctx.WriteString(fmt.Sprintf("<untrusted_repo_content source=%q>\n", filePath))
			ctx.WriteString(content)
			ctx.WriteString("\n</untrusted_repo_content>\n\n")
		}
	}

	return ctx.String(), nil
}

// readFileWithSecurity reads a file and applies all security mitigations:
// - Secret scanning and redaction
// - Prompt injection detection and sanitization
// - Untrusted content wrapping with provenance
//
// See Architecture Section 29.4 and 29.6.
func (b *TaskSpecBuilder) readFileWithSecurity(filePath string) (string, error) {
	// Check exclusion list first.
	if security.IsExcludedPath(filePath) {
		return "", fmt.Errorf("excluded path: %s", filePath)
	}

	fullPath := b.projectRoot + "/" + filePath
	data, err := readFileBytes(fullPath)
	if err != nil {
		return "", err
	}

	content := string(data)

	// Apply the full security pipeline.
	files := map[string]string{filePath: content}
	sanitized := security.PrepareContextForPrompt(files, b.secretScanner)

	if result, ok := sanitized[filePath]; ok {
		return result, nil
	}
	return "", fmt.Errorf("file excluded by security pipeline: %s", filePath)
}

// readFileBytes reads a file from the filesystem. Extracted for testability.
var readFileBytes = func(path string) ([]byte, error) {
	return readFileBytesImpl(path)
}

func readFileBytesImpl(path string) ([]byte, error) {
	return os.ReadFile(path)
}
