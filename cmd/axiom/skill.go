package main

import (
	"fmt"

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
		validRuntimes := map[string]string{
			"claw":        "axiom-skill.md",
			"claude-code": ".claude/CLAUDE.md",
			"codex":       "codex-instructions.md",
			"opencode":    "opencode-instructions.md",
		}

		output, ok := validRuntimes[skillRuntime]
		if !ok {
			return fmt.Errorf("unsupported runtime: %s (valid: claw, claude-code, codex, opencode)", skillRuntime)
		}

		fmt.Printf("Generating skill file for %s runtime...\n", skillRuntime)
		fmt.Printf("Output: %s\n", output)
		return nil
	},
}

func init() {
	skillGenerateCmd.Flags().StringVar(&skillRuntime, "runtime", "", "Target runtime (claw, claude-code, codex, opencode)")
	skillGenerateCmd.MarkFlagRequired("runtime")

	skillCmd.AddCommand(skillGenerateCmd)
}
