package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethan03805/axiom/internal/engine"
	"github.com/ethan03805/axiom/internal/registry"
	"github.com/spf13/cobra"
)

// registryDBPath returns the path to ~/.axiom/registry.db.
func registryDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "registry.db"
	}
	dir := filepath.Join(home, ".axiom")
	os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "registry.db")
}

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
available BitNet models, and merges all sources into the registry.
See Architecture Section 18.4.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := registry.NewRegistry(registryDBPath())
		if err != nil {
			return fmt.Errorf("open registry: %w", err)
		}
		defer reg.Close()

		// Load API key from config first, fall back to env var.
		// Architecture Section 19.5: API keys stored in ~/.axiom/config.toml.
		apiKey := ""
		if globalCfg, err := engine.LoadConfig(); err == nil && globalCfg.OpenRouter.APIKey != "" {
			apiKey = globalCfg.OpenRouter.APIKey
		}
		if envKey := os.Getenv("OPENROUTER_API_KEY"); envKey != "" {
			apiKey = envKey
		}
		fetcher := registry.NewOpenRouterFetcher(apiKey)
		if fetcher.Available() {
			fmt.Println("Fetching from OpenRouter API...")
			models, err := fetcher.FetchModels(context.Background())
			if err != nil {
				fmt.Printf("  Warning: OpenRouter fetch failed: %v\n", err)
			} else {
				for _, m := range models {
					_ = reg.Upsert(m)
				}
				fmt.Printf("  %d models fetched from OpenRouter\n", len(models))
			}
		} else {
			fmt.Println("  OpenRouter API key not set (OPENROUTER_API_KEY). Skipping.")
		}

		// Add local BitNet model entry.
		fmt.Println("Scanning local BitNet models...")
		_ = reg.Upsert(&registry.ModelInfo{
			ID:                   "local/falcon3-1b",
			Family:               "local",
			Source:               "bitnet",
			Tier:                 registry.TierLocal,
			ContextWindow:        2048,
			MaxOutput:            1024,
			PromptPerMillion:     0,
			CompletionPerMillion: 0,
			SupportsGrammar:      true,
		})
		fmt.Println("  Added local/falcon3-1b")

		// Merge curated capability data from models.json.
		// Look in multiple locations: next to binary, ~/.axiom/, then cwd.
		fmt.Println("Merging curated capability data...")
		curatedPath := findCuratedModelsJSON()
		if curatedPath != "" {
			data, err := os.ReadFile(curatedPath)
			if err == nil {
				var curatedFile struct {
					Models []*registry.ModelInfo `json:"models"`
				}
				if err := json.Unmarshal(data, &curatedFile); err == nil {
					if err := reg.MergeCuratedData(curatedFile.Models); err != nil {
						fmt.Printf("  Warning: merge curated data: %v\n", err)
					} else {
						fmt.Printf("  Merged %d curated model entries\n", len(curatedFile.Models))
					}
				}
			}
		}

		count, _ := reg.Count()
		fmt.Printf("Model registry updated. %d models total.\n", count)
		return nil
	},
}

var modelsListTier string
var modelsListFamily string

var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered models",
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := registry.NewRegistry(registryDBPath())
		if err != nil {
			return fmt.Errorf("open registry: %w", err)
		}
		defer reg.Close()

		models, err := reg.List(registry.ModelTier(modelsListTier), modelsListFamily)
		if err != nil {
			return fmt.Errorf("list models: %w", err)
		}

		if len(models) == 0 {
			fmt.Println("No models in registry. Run 'axiom models refresh' first.")
			return nil
		}

		fmt.Printf("%-40s %-15s %-10s %-10s %s\n", "MODEL", "FAMILY", "TIER", "SOURCE", "COMPLETION $/M")
		fmt.Println(strings.Repeat("-", 95))
		for _, m := range models {
			fmt.Printf("%-40s %-15s %-10s %-10s $%.2f\n",
				m.ID, m.Family, m.Tier, m.Source, m.CompletionPerMillion)
		}
		fmt.Printf("\n%d models\n", len(models))
		return nil
	},
}

var modelsInfoCmd = &cobra.Command{
	Use:   "info [model-id]",
	Short: "Show detailed info for a specific model",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		reg, err := registry.NewRegistry(registryDBPath())
		if err != nil {
			return fmt.Errorf("open registry: %w", err)
		}
		defer reg.Close()

		m, err := reg.Get(args[0])
		if err != nil {
			return fmt.Errorf("model not found: %w", err)
		}

		fmt.Printf("Model:           %s\n", m.ID)
		fmt.Printf("Family:          %s\n", m.Family)
		fmt.Printf("Source:          %s\n", m.Source)
		fmt.Printf("Tier:            %s\n", m.Tier)
		fmt.Printf("Context Window:  %d tokens\n", m.ContextWindow)
		fmt.Printf("Max Output:      %d tokens\n", m.MaxOutput)
		fmt.Printf("Prompt Cost:     $%.2f / million tokens\n", m.PromptPerMillion)
		fmt.Printf("Completion Cost: $%.2f / million tokens\n", m.CompletionPerMillion)
		fmt.Printf("Supports Tools:  %v\n", m.SupportsTools)
		fmt.Printf("Supports Vision: %v\n", m.SupportsVision)
		fmt.Printf("Supports Grammar: %v\n", m.SupportsGrammar)
		if len(m.Strengths) > 0 {
			fmt.Printf("Strengths:       %s\n", strings.Join(m.Strengths, ", "))
		}
		if len(m.Weaknesses) > 0 {
			fmt.Printf("Weaknesses:      %s\n", strings.Join(m.Weaknesses, ", "))
		}
		if len(m.RecommendedFor) > 0 {
			fmt.Printf("Recommended For: %s\n", strings.Join(m.RecommendedFor, ", "))
		}
		if len(m.NotRecommendedFor) > 0 {
			fmt.Printf("Not Recommended: %s\n", strings.Join(m.NotRecommendedFor, ", "))
		}
		if m.HistoricalSuccessRate != nil {
			fmt.Printf("Success Rate:    %.1f%%\n", *m.HistoricalSuccessRate*100)
		}
		if m.AvgCostPerTask != nil {
			fmt.Printf("Avg Cost/Task:   $%.4f\n", *m.AvgCostPerTask)
		}
		return nil
	},
}

// findCuratedModelsJSON searches for models.json in multiple locations:
// 1. Next to the running binary
// 2. ~/.axiom/models.json
// 3. Current working directory (fallback)
// Returns empty string if not found anywhere.
func findCuratedModelsJSON() string {
	// 1. Next to the binary.
	if exe, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "models.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// 2. Global config directory.
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".axiom", "models.json")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}

	// 3. Current working directory (fallback).
	if _, err := os.Stat("models.json"); err == nil {
		return "models.json"
	}

	return ""
}

func init() {
	modelsListCmd.Flags().StringVar(&modelsListTier, "tier", "", "Filter by tier (local, cheap, standard, premium)")
	modelsListCmd.Flags().StringVar(&modelsListFamily, "family", "", "Filter by model family")

	modelsCmd.AddCommand(modelsRefreshCmd)
	modelsCmd.AddCommand(modelsListCmd)
	modelsCmd.AddCommand(modelsInfoCmd)
}
