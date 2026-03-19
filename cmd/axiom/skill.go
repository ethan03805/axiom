package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethan03805/axiom/internal/skill"
	"github.com/spf13/cobra"
)

// --- axiom skill ---

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage runtime-specific skill files",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var skillRuntime string

var skillGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate skill file for the specified runtime",
	Long: `Generates a runtime-specific instruction file that teaches an orchestrator
how to use Axiom. Supported runtimes: claw, claude-code, codex, opencode.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !skill.IsValidRuntime(skillRuntime) {
			return fmt.Errorf("unsupported runtime: %s (valid: claw, claude-code, codex, opencode)", skillRuntime)
		}

		// Determine project root. Walk upward from cwd looking for a skills/
		// directory (the repository root). Fall back to cwd.
		projectRoot, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("find project root: %w", err)
		}

		// Build TemplateData from defaults (config loading will be wired in
		// when internal/engine/config.go is implemented).
		data := buildTemplateData(projectRoot)

		rt := skill.Runtime(skillRuntime)

		// Create generator and load templates from the skills/ directory.
		gen := skill.NewGenerator(projectRoot)
		if err := gen.LoadTemplates(); err != nil {
			return fmt.Errorf("load templates: %w", err)
		}

		// Write the skill file to the project root.
		outputPath, err := gen.WriteSkillFile(rt, data, projectRoot)
		if err != nil {
			return fmt.Errorf("generate skill file: %w", err)
		}

		fmt.Printf("Generated skill file for %s runtime.\n", skillRuntime)
		fmt.Printf("Output: %s\n", outputPath)
		return nil
	},
}

func init() {
	skillGenerateCmd.Flags().StringVar(&skillRuntime, "runtime", "", "Target runtime (claw, claude-code, codex, opencode)")
	skillGenerateCmd.MarkFlagRequired("runtime")

	skillCmd.AddCommand(skillGenerateCmd)
}

// findProjectRoot walks upward from the current working directory looking for
// a directory that contains a skills/ subdirectory. This identifies the
// repository root where the templates live. If none is found, it falls back to
// the current working directory.
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "skills")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding skills/.
			// Fall back to cwd.
			return cwd, nil
		}
		dir = parent
	}
}

// buildTemplateData constructs a TemplateData with sensible defaults.
// When the config system (internal/engine/config.go) is fully wired, this
// will read from .axiom/config.toml. For now it uses Architecture Appendix A
// defaults.
func buildTemplateData(projectRoot string) skill.TemplateData {
	// Derive slug from project root directory name.
	slug := filepath.Base(projectRoot)

	return skill.TemplateData{
		ProjectName:       slug,
		ProjectSlug:       slug,
		BudgetUSD:         10.00,
		BudgetMax:         "10.00",
		MaxMeeseeks:       10,
		APIPort:           3000,
		BitNetEnabled:     true,
		BitNetPort:        3002,
		DockerImage:       "axiom-meeseeks-multi:latest",
		BranchPrefix:      "axiom",
		ModelTiers:        "",
		ModelTiersSummary: "",
		IPCEndpoint:       "filesystem IPC at /workspace/ipc/",
	}
}
