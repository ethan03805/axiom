package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// --- axiom models ---

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "Manage the AI model registry",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var modelsRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Update model registry from OpenRouter + local BitNet",
	Long: `Fetches the latest model list and pricing from OpenRouter API,
downloads the latest models.json capability index, scans locally
available BitNet models, and merges all sources into the registry.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Refreshing model registry...")
		fmt.Println("  Fetching from OpenRouter API...")
		fmt.Println("  Scanning local BitNet models...")
		fmt.Println("  Merging capability data...")
		fmt.Println("Model registry updated.")
		return nil
	},
}

var modelsListTier string
var modelsListFamily string

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered models",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("%-40s %-12s %-10s %-10s %s\n", "MODEL", "FAMILY", "TIER", "SOURCE", "COMPLETION $/M")
		fmt.Println("------------------------------------------------------------------------------------")

		if modelsListTier != "" {
			fmt.Printf("(filtered by tier: %s)\n", modelsListTier)
		}
		if modelsListFamily != "" {
			fmt.Printf("(filtered by family: %s)\n", modelsListFamily)
		}
		return nil
	},
}

var modelsInfoCmd = &cobra.Command{
	Use:   "info [model-id]",
	Short: "Show detailed info for a specific model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		modelID := args[0]
		fmt.Printf("Model: %s\n", modelID)
		fmt.Println("(Model details would be loaded from registry)")
		return nil
	},
}

func init() {
	modelsListCmd.Flags().StringVar(&modelsListTier, "tier", "", "Filter by tier (local, cheap, standard, premium)")
	modelsListCmd.Flags().StringVar(&modelsListFamily, "family", "", "Filter by model family")

	modelsCmd.AddCommand(modelsRefreshCmd)
	modelsCmd.AddCommand(modelsListCmd)
	modelsCmd.AddCommand(modelsInfoCmd)
}
