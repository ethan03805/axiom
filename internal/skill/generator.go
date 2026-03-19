// Package skill implements the Skill System that generates runtime-specific
// instruction files teaching orchestrators how to use Axiom.
//
// See Architecture.md Section 25 for the full specification.
package skill

import (
	"bytes"
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

// templateFiles maps each runtime to its template filename in the skills/ directory.
var templateFiles = map[Runtime]string{
	RuntimeClaw:       "claw.md.tmpl",
	RuntimeClaudeCode: "claude-code.md.tmpl",
	RuntimeCodex:      "codex.md.tmpl",
	RuntimeOpenCode:   "opencode.md.tmpl",
}

// TemplateData holds the dynamic content injected into skill templates.
type TemplateData struct {
	ProjectName       string
	ProjectSlug       string
	BudgetUSD         float64
	BudgetMax         string // Formatted budget string for templates (e.g. "10.00")
	MaxMeeseeks       int
	Runtime           string
	APIPort           int
	BitNetEnabled     bool
	BitNetPort        int
	DockerImage       string
	BranchPrefix      string
	ModelTiers        string // Summary of model tiers (legacy field)
	ModelTiersSummary string // Summary of model tiers used in .tmpl files
	IPCEndpoint       string // IPC or API endpoint info
}

// Generator produces runtime-specific skill files from templates.
// See Architecture Section 25.4.
type Generator struct {
	projectRoot string
	templates   map[Runtime]*template.Template
}

// NewGenerator creates a skill Generator for the project. It does not load
// templates; call LoadTemplates or LoadTemplatesFromDir after construction.
func NewGenerator(projectRoot string) *Generator {
	return &Generator{
		projectRoot: projectRoot,
		templates:   make(map[Runtime]*template.Template),
	}
}

// LoadTemplates reads template files from the skills/ directory inside the
// project root (i.e. the repository root where `skills/*.md.tmpl` live).
// This is the standard entry point for production use.
func (g *Generator) LoadTemplates() error {
	skillsDir := filepath.Join(g.projectRoot, "skills")
	return g.LoadTemplatesFromDir(skillsDir)
}

// LoadTemplatesFromDir reads template files from the given directory.
// Each runtime has a corresponding .md.tmpl file.
func (g *Generator) LoadTemplatesFromDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("skills directory not found: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("skills path is not a directory: %s", dir)
	}

	for rt, filename := range templateFiles {
		tmplPath := filepath.Join(dir, filename)
		data, err := os.ReadFile(tmplPath)
		if err != nil {
			return fmt.Errorf("read template %s: %w", filename, err)
		}

		tmpl, err := template.New(string(rt)).Parse(string(data))
		if err != nil {
			return fmt.Errorf("parse template %s: %w", filename, err)
		}

		g.templates[rt] = tmpl
	}

	return nil
}

// HasTemplates reports whether templates have been loaded.
func (g *Generator) HasTemplates() bool {
	return len(g.templates) > 0
}

// Generate selects the correct template for the given runtime, executes it
// with the provided data, and returns the rendered content as a string.
func (g *Generator) Generate(runtime Runtime, data TemplateData) (string, error) {
	tmpl, ok := g.templates[runtime]
	if !ok {
		return "", fmt.Errorf("no template loaded for runtime: %s", runtime)
	}

	// Populate computed fields so templates can reference them.
	data.Runtime = string(runtime)
	if data.BudgetMax == "" {
		data.BudgetMax = fmt.Sprintf("%.2f", data.BudgetUSD)
	}
	if data.ModelTiersSummary == "" && data.ModelTiers != "" {
		data.ModelTiersSummary = data.ModelTiers
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template for %s: %w", runtime, err)
	}

	return buf.String(), nil
}

// WriteSkillFile renders the template for the given runtime and writes it to
// the correct output path inside outputDir, per Architecture Section 25.2:
//   - Claw:       axiom-skill.md
//   - Claude Code: .claude/CLAUDE.md
//   - Codex:      codex-instructions.md
//   - OpenCode:   opencode-instructions.md
func (g *Generator) WriteSkillFile(runtime Runtime, data TemplateData, outputDir string) (string, error) {
	content, err := g.Generate(runtime, data)
	if err != nil {
		return "", err
	}

	relPath, ok := OutputPaths[runtime]
	if !ok {
		return "", fmt.Errorf("no output path defined for runtime: %s", runtime)
	}

	fullPath := filepath.Join(outputDir, relPath)

	// Ensure parent directory exists (e.g. .claude/ for Claude Code).
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write skill file: %w", err)
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

// ContentTopics returns the 13 required content items for verification.
func ContentTopics() []string {
	return []string{
		"Axiom Workflow",
		"Trusted Engine vs. Untrusted Agent Plane",
		"Available",
		"TaskSpec Format",
		"ReviewSpec Format",
		"Context Tiers",
		"Model",
		"Budget Management",
		"Task Decomposition",
		"Communication Model",
		"Engineering Change Orders",
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
