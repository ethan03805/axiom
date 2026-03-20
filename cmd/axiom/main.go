// Package main implements the Axiom CLI entrypoint.
// See Architecture.md Section 27 for the complete CLI reference.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

var rootCmd = &cobra.Command{
	Use:   "axiom",
	Short: "Axiom - AI agent orchestration platform for autonomous software development",
	Long: `Axiom coordinates multiple AI agents running in isolated Docker containers
to autonomously build software from a single user prompt.

It enforces a single-prompt -> SRS -> approval -> autonomous execution flow
with immutable scope, budget-aware orchestration, and full audit trails.

See https://github.com/ethan03805/axiom for documentation.`,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// Project commands (Section 27.1)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(pauseCmd)
	rootCmd.AddCommand(resumeCmd)
	rootCmd.AddCommand(cancelCmd)
	rootCmd.AddCommand(exportCmd)

	// Model commands (Section 27.2)
	rootCmd.AddCommand(modelsCmd)

	// BitNet commands (Section 27.3)
	rootCmd.AddCommand(bitnetCmd)

	// API & Tunnel commands (Section 27.4)
	rootCmd.AddCommand(apiCmd)
	rootCmd.AddCommand(tunnelCmd)

	// Skill commands (Section 27.5)
	rootCmd.AddCommand(skillCmd)

	// Index commands (Section 27.6)
	rootCmd.AddCommand(indexCmd)

	// SRS commands
	rootCmd.AddCommand(srsCmd)

	// Utility commands (Section 27.7)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(doctorCmd)
	rootCmd.AddCommand(configCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
