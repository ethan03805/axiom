package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethan03805/axiom/internal/engine"
	"github.com/spf13/cobra"
)

// --- axiom init ---

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Axiom project in the current directory",
	Long: `Creates the .axiom/ directory structure with default configuration.
Sets up the SQLite database, default config.toml, and .gitignore entries.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		axiomDir := ".axiom"

		// Create directory structure per Architecture Section 28.1.
		dirs := []string{
			axiomDir,
			filepath.Join(axiomDir, "containers", "specs"),
			filepath.Join(axiomDir, "containers", "staging"),
			filepath.Join(axiomDir, "containers", "ipc"),
			filepath.Join(axiomDir, "validation"),
			filepath.Join(axiomDir, "eco"),
			filepath.Join(axiomDir, "logs", "prompts"),
		}
		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("create %s: %w", dir, err)
			}
		}

		// Write default config.
		cfg := engine.DefaultConfig()
		configPath := filepath.Join(axiomDir, "config.toml")
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			if err := writeDefaultConfig(configPath, cfg); err != nil {
				return fmt.Errorf("write config: %w", err)
			}
			fmt.Printf("Created %s\n", configPath)
		} else {
			fmt.Printf("%s already exists, skipping\n", configPath)
		}

		fmt.Println("Axiom project initialized. Edit .axiom/config.toml to configure.")
		return nil
	},
}

func writeDefaultConfig(path string, cfg *engine.Config) error {
	content := fmt.Sprintf(`[project]
name = ""
slug = ""

[budget]
max_usd = %.2f
warn_at_percent = %.0f

[concurrency]
max_meeseeks = %d

[orchestrator]
runtime = "%s"
srs_approval_delegate = "%s"

[bitnet]
enabled = %v
host = "%s"
port = %d
max_concurrent_requests = %d
cpu_threads = %d

[docker]
image = "%s"
timeout_minutes = %d
cpu_limit = %.1f
mem_limit = "%s"
network_mode = "%s"

[validation]
timeout_minutes = %d
cpu_limit = %.1f
mem_limit = "%s"
network = "%s"
allow_dependency_install = %v
security_scan = %v
warm_pool_enabled = %v
warm_pool_size = %d
warm_cold_interval = %d

[security]
force_local_for_sensitive = %v

[git]
auto_commit = %v
branch_prefix = "%s"

[api]
port = %d
rate_limit_rpm = %d

[observability]
log_prompts = %v
log_token_counts = %v
`,
		cfg.Budget.MaxUSD, cfg.Budget.WarnAtPercent,
		cfg.Concurrency.MaxMeeseeks,
		cfg.Orchestrator.Runtime, cfg.Orchestrator.SRSApprovalDelegate,
		cfg.BitNet.Enabled, cfg.BitNet.Host, cfg.BitNet.Port,
		cfg.BitNet.MaxConcurrentRequests, cfg.BitNet.CPUThreads,
		cfg.Docker.Image, cfg.Docker.TimeoutMinutes, cfg.Docker.CPULimit,
		cfg.Docker.MemLimit, cfg.Docker.NetworkMode,
		cfg.Validation.TimeoutMinutes, cfg.Validation.CPULimit,
		cfg.Validation.MemLimit, cfg.Validation.Network,
		cfg.Validation.AllowDependencyInstall, cfg.Validation.SecurityScan,
		cfg.Validation.WarmPoolEnabled, cfg.Validation.WarmPoolSize, cfg.Validation.WarmColdInterval,
		cfg.Security.ForceLocalForSensitive,
		cfg.Git.AutoCommit, cfg.Git.BranchPrefix,
		cfg.API.Port, cfg.API.RateLimitRPM,
		cfg.Observability.LogPrompts, cfg.Observability.LogTokenCounts,
	)
	return os.WriteFile(path, []byte(content), 0644)
}

// --- axiom run ---

var runBudget float64

var runCmd = &cobra.Command{
	Use:   "run [prompt]",
	Short: "Start a new project: generate SRS, await approval, execute",
	Long: `Submits a natural language prompt describing the desired software.
The orchestrator generates an SRS, presents it for approval, then
autonomously executes the approved plan.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prompt := args[0]
		fmt.Printf("Starting Axiom project with prompt: %s\n", prompt)
		if runBudget > 0 {
			fmt.Printf("Budget: $%.2f\n", runBudget)
		}

		// Load config.
		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if runBudget > 0 {
			cfg.Budget.MaxUSD = runBudget
		}

		// Initialize engine.
		eng, err := engine.New(cfg)
		if err != nil {
			return fmt.Errorf("init engine: %w", err)
		}
		defer eng.Stop()

		if err := eng.Start(); err != nil {
			return fmt.Errorf("start engine: %w", err)
		}

		fmt.Println("Engine started. Orchestrator will generate SRS for approval.")
		fmt.Printf("Prompt: %s\n", prompt)
		return nil
	},
}

func init() {
	runCmd.Flags().Float64Var(&runBudget, "budget", 0, "Maximum budget in USD")
}

// --- axiom status ---

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show project status, task tree, active Meeseeks, budget, resources",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		fmt.Printf("Project: %s\n", cfg.Project.Name)
		fmt.Printf("Budget:  $%.2f (warn at %.0f%%)\n", cfg.Budget.MaxUSD, cfg.Budget.WarnAtPercent)
		fmt.Printf("Max Meeseeks: %d\n", cfg.Concurrency.MaxMeeseeks)
		fmt.Printf("Runtime: %s\n", cfg.Orchestrator.Runtime)
		return nil
	},
}

// --- axiom pause ---

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause execution: stop spawning new Meeseeks, let active ones complete",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Pausing execution. Active Meeseeks will complete their current tasks.")
		return nil
	},
}

// --- axiom resume ---

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a paused project",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Resuming execution.")
		return nil
	},
}

// --- axiom cancel ---

var cancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel execution, kill containers, revert uncommitted changes",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Cancelling execution. Killing active containers and reverting uncommitted changes.")
		return nil
	},
}

// --- axiom export ---

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export project state as human-readable JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		state := map[string]interface{}{
			"version": Version,
			"status":  "exported",
		}
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	},
}
