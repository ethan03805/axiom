// Package skill implements the Skill System that generates runtime-specific
// instruction files teaching orchestrators how to use Axiom.
//
// See Architecture.md Section 25 for the full specification.
package skill

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Runtime identifies a supported orchestrator runtime.
// See Architecture Section 25.2.
type Runtime string

const (
	RuntimeClaw       Runtime = "claw"
	RuntimeClaudeCode Runtime = "claude-code"
	RuntimeCodex      Runtime = "codex"
	RuntimeOpenCode   Runtime = "opencode"
)

// OutputPaths maps each runtime to its expected output file.
// See Architecture Section 25.2.
var OutputPaths = map[Runtime]string{
	RuntimeClaw:       "axiom-skill.md",
	RuntimeClaudeCode: ".claude/CLAUDE.md",
	RuntimeCodex:      "codex-instructions.md",
	RuntimeOpenCode:   "opencode-instructions.md",
}

// TemplateData holds the dynamic content injected into skill templates.
type TemplateData struct {
	ProjectName    string
	ProjectSlug    string
	BudgetUSD      float64
	MaxMeeseeks    int
	Runtime        string
	APIPort        int
	BitNetEnabled  bool
	BitNetPort     int
	DockerImage    string
	BranchPrefix   string
	ModelTiers     string // Summary of model tiers
	IPCEndpoint    string // IPC or API endpoint info
}

// Generator produces runtime-specific skill files from templates.
// See Architecture Section 25.4.
type Generator struct {
	projectRoot string
	templates   map[Runtime]*template.Template
}

// NewGenerator creates a skill Generator for the project.
func NewGenerator(projectRoot string) *Generator {
	g := &Generator{
		projectRoot: projectRoot,
		templates:   make(map[Runtime]*template.Template),
	}

	// Register built-in templates.
	for _, rt := range []Runtime{RuntimeClaw, RuntimeClaudeCode, RuntimeCodex, RuntimeOpenCode} {
		tmpl := template.Must(template.New(string(rt)).Parse(getTemplate(rt)))
		g.templates[rt] = tmpl
	}

	return g
}

// Generate produces a skill file for the given runtime with the provided data.
// Returns the output path and any error.
func (g *Generator) Generate(runtime Runtime, data *TemplateData) (string, error) {
	tmpl, ok := g.templates[runtime]
	if !ok {
		return "", fmt.Errorf("unsupported runtime: %s", runtime)
	}

	outputPath, ok := OutputPaths[runtime]
	if !ok {
		return "", fmt.Errorf("no output path for runtime: %s", runtime)
	}

	fullPath := filepath.Join(g.projectRoot, outputPath)

	// Ensure parent directory exists.
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("create output dir: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return "", fmt.Errorf("create output file: %w", err)
	}
	defer f.Close()

	data.Runtime = string(runtime)
	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}

	return fullPath, nil
}

// SupportedRuntimes returns all supported runtime identifiers.
func SupportedRuntimes() []Runtime {
	return []Runtime{RuntimeClaw, RuntimeClaudeCode, RuntimeCodex, RuntimeOpenCode}
}

// IsValidRuntime checks if a runtime string is supported.
func IsValidRuntime(rt string) bool {
	switch Runtime(rt) {
	case RuntimeClaw, RuntimeClaudeCode, RuntimeCodex, RuntimeOpenCode:
		return true
	}
	return false
}

// getTemplate returns the built-in template for a runtime.
// Each template includes all 13 content items from Architecture Section 25.3.
func getTemplate(rt Runtime) string {
	// The core content is shared across all runtimes.
	// Runtime-specific wrappers adjust the framing.
	switch rt {
	case RuntimeClaw:
		return clawTemplate
	case RuntimeClaudeCode:
		return claudeCodeTemplate
	case RuntimeCodex:
		return codexTemplate
	case RuntimeOpenCode:
		return openCodeTemplate
	default:
		return ""
	}
}

// --- Templates ---
// Each includes all 13 topics from Architecture Section 25.3.

var sharedContent = `
## Axiom Workflow

Axiom follows a strict workflow: prompt -> SRS -> approval -> autonomous execution.
Once the SRS is approved, scope is immutable. Only Engineering Change Orders (ECOs)
may modify environmental details.

## Trusted Engine vs. Untrusted Agent Plane

The Trusted Engine (Go control plane) executes ALL privileged operations: filesystem
writes, git commits, container spawning, budget enforcement, model API calls.
LLM agents (including you) propose actions through structured requests. The engine
validates, authorizes, and executes them. You CANNOT directly access the filesystem,
Docker, git, or model APIs.

## Available Request Types

You may submit these requests via {{if eq .Runtime "claw"}}the REST API{{else}}IPC{{end}}:
- submit_srs: Submit generated SRS for user approval
- submit_eco: Propose an Engineering Change Order
- create_task / create_task_batch: Add tasks to the task tree
- spawn_meeseeks / spawn_reviewer / spawn_sub_orchestrator: Request container spawning
- approve_output / reject_output: Validate Meeseeks output
- query_index: Query the semantic code index
- query_status / query_budget: Get current state
- request_inference: Submit an inference request to the broker

## TaskSpec Format

Every Meeseeks receives a self-contained TaskSpec with:
- Base Snapshot (git SHA)
- Objective (single sentence)
- Context at the minimum necessary tier (symbol, file, package, repo-map)
- Interface Contract
- Constraints (language, style, dependencies)
- Acceptance Criteria
- Output Format: files to /workspace/staging/ + manifest.json

## ReviewSpec Format

Reviewers receive: original TaskSpec + Meeseeks output + manifest + validation results.
They return APPROVE or REJECT with per-criterion evaluation and feedback.

## Context Tiers

Select the MINIMUM tier sufficient for each task:
1. Symbol-level: function signatures, type definitions
2. File-level: complete source files
3. Package-level: full package with dependencies
4. Repo map: dependency graph, directory structure
5. Indexed query: dynamic context from semantic index

## Model Tiers

| Tier | Use Cases |
|------|-----------|
| Local (BitNet) | Renames, imports, config, boilerplate |
| Cheap | Simple functions, small modifications |
| Standard | Most coding tasks, refactoring |
| Premium | Complex algorithms, APIs, critical code |

Project budget: ${{printf "%.2f" .BudgetUSD}} | Max concurrent Meeseeks: {{.MaxMeeseeks}}

## Budget Management

- Budget is enforced per-request before execution
- Track spending via query_budget
- Prefer cheaper models and BitNet when budget is tight
- Reduce concurrency to slow spend rate if needed

## Task Decomposition

- Break tasks to smallest sensible size
- Small enough for the model tier, large enough for code coherence
- Tasks that are inherently interconnected should stay as single tasks
- Every task references SRS requirements (FR-xxx, AC-xxx)
- Test tasks are SEPARATE from implementation, using a DIFFERENT model family

## Communication Model

Strictly hierarchical: you communicate only with the engine.
Meeseeks do not communicate with each other unless you explicitly
request lateral channels via the engine.

## ECO Process

Valid categories: ECO-DEP (dependency), ECO-API (API change), ECO-SEC (security),
ECO-PLT (platform), ECO-LIC (license), ECO-PRV (provider limitation).
ECOs are for environmental changes ONLY, not scope changes.

## Error Handling

- Failed tasks retry up to 3 times at the same tier
- After retry exhaustion: escalate to next tier (max 2 escalations)
- After escalation exhaustion: task is BLOCKED, requires restructuring
- You may restructure blocked tasks, provide additional context, or file ECOs

## Test Authorship Separation

Tests MUST be authored by a different Meeseeks from a different model family
than the implementation. This prevents circular validation.
Test tasks depend on implementation tasks (created after implementation merges).
`

var clawTemplate = `# Axiom Skill: Orchestrator Instructions

You are orchestrating an Axiom project as a Claw-based orchestrator.
You connect to Axiom via the REST API at http://localhost:{{.APIPort}}.

Project: {{.ProjectName}} ({{.ProjectSlug}})
Docker image: {{.DockerImage}}
Branch: {{.BranchPrefix}}/{{.ProjectSlug}}
` + sharedContent

var claudeCodeTemplate = `# Axiom Orchestrator Instructions for Claude Code

You are orchestrating an Axiom project as an embedded Claude Code orchestrator.
All inference goes through the engine's Inference Broker via IPC.

Project: {{.ProjectName}} ({{.ProjectSlug}})
Docker image: {{.DockerImage}}
Branch: {{.BranchPrefix}}/{{.ProjectSlug}}

IMPORTANT: Do NOT execute code directly. Submit all actions through IPC.
Do NOT write files to the project filesystem. Propose file writes through
the engine's approval pipeline.
` + sharedContent

var codexTemplate = `# Axiom Orchestrator Instructions for Codex

You are orchestrating an Axiom project as an embedded Codex orchestrator.
All inference goes through the engine's Inference Broker via IPC.

Project: {{.ProjectName}} ({{.ProjectSlug}})
Docker image: {{.DockerImage}}
Branch: {{.BranchPrefix}}/{{.ProjectSlug}}
` + sharedContent

var openCodeTemplate = `# Axiom Orchestrator Instructions for OpenCode

You are orchestrating an Axiom project as an embedded OpenCode orchestrator.
All inference goes through the engine's Inference Broker via IPC.

Project: {{.ProjectName}} ({{.ProjectSlug}})
Docker image: {{.DockerImage}}
Branch: {{.BranchPrefix}}/{{.ProjectSlug}}
` + sharedContent

// ContentTopics returns the 13 required content items for verification.
func ContentTopics() []string {
	return []string{
		"Axiom Workflow",
		"Trusted Engine vs. Untrusted Agent Plane",
		"Available Request Types",
		"TaskSpec Format",
		"ReviewSpec Format",
		"Context Tiers",
		"Model Tiers",
		"Budget Management",
		"Task Decomposition",
		"Communication Model",
		"ECO Process",
		"Error Handling",
		"Test Authorship Separation",
	}
}

// VerifyContent checks that generated content includes all 13 required topics.
func VerifyContent(content string) []string {
	var missing []string
	for _, topic := range ContentTopics() {
		if !strings.Contains(content, topic) {
			missing = append(missing, topic)
		}
	}
	return missing
}
