package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethan03805/axiom/internal/engine"
	"github.com/ethan03805/axiom/internal/skill"
	"github.com/ethan03805/axiom/skills"
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

		// Determine project root by looking for .axiom/ directory.
		// This is the user's initialized Axiom project, not the source repo.
		projectRoot, err := findProjectRoot()
		if err != nil {
			return fmt.Errorf("find project root: %w", err)
		}

		// Build TemplateData from config if available, otherwise defaults.
		data := buildTemplateData(projectRoot)

		rt := skill.Runtime(skillRuntime)

		// Create generator and load templates from embedded filesystem.
		// Templates are bundled into the binary via Go embed, not loaded
		// from the source tree's skills/ directory.
		gen := skill.NewGenerator(projectRoot)
		if err := gen.LoadTemplatesFromFS(skills.Templates); err != nil {
			return fmt.Errorf("load templates: %w", err)
		}

		// Write the skill file to the project root (cwd or .axiom/ parent).
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
// a directory that contains a .axiom/ subdirectory (an initialized Axiom
// project). If none is found, it falls back to the current working directory.
func findProjectRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}

	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".axiom")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root without finding .axiom/.
			// Fall back to cwd.
			return cwd, nil
		}
		dir = parent
	}
}

// buildTemplateData constructs a TemplateData from .axiom/config.toml if
// available, otherwise uses Architecture Appendix A defaults.
func buildTemplateData(projectRoot string) skill.TemplateData {
	// Try loading config from the project.
	configPath := filepath.Join(projectRoot, ".axiom", "config.toml")
	cfg, err := engine.LoadConfigFrom(configPath)
	if err != nil {
		cfg = engine.DefaultConfig()
	}

	// Derive slug from config or project root directory name.
	slug := cfg.Project.Slug
	if slug == "" {
		slug = filepath.Base(projectRoot)
	}
	name := cfg.Project.Name
	if name == "" {
		name = slug
	}

	return skill.TemplateData{
		ProjectName:       name,
		ProjectSlug:       slug,
		BudgetUSD:         cfg.Budget.MaxUSD,
		BudgetMax:         fmt.Sprintf("%.2f", cfg.Budget.MaxUSD),
		MaxMeeseeks:       cfg.Concurrency.MaxMeeseeks,
		APIPort:           cfg.API.Port,
		BitNetEnabled:     cfg.BitNet.Enabled,
		BitNetPort:        cfg.BitNet.Port,
		DockerImage:       cfg.Docker.Image,
		BranchPrefix:      cfg.Git.BranchPrefix,
		ModelTiers:        "",
		ModelTiersSummary: "",
		IPCEndpoint:       "filesystem IPC at /workspace/ipc/",
	}
}
