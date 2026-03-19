package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethan03805/axiom/internal/engine"
	"github.com/ethan03805/axiom/internal/state"
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

		// Write .gitignore entries per Architecture Section 28.2.
		gitignorePath := filepath.Join(axiomDir, ".gitignore")
		if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
			gitignore := `# Axiom runtime state (gitignored per Architecture Section 28.2)
axiom.db
axiom.db-wal
axiom.db-shm
containers/
validation/
logs/
`
			if err := os.WriteFile(gitignorePath, []byte(gitignore), 0644); err != nil {
				return fmt.Errorf("write .gitignore: %w", err)
			}
			fmt.Printf("Created %s\n", gitignorePath)
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
autonomously executes the approved plan.

See Architecture Section 5.1 for the project lifecycle.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		prompt := args[0]

		// Load config.
		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		if runBudget > 0 {
			cfg.Budget.MaxUSD = runBudget
		}

		// Verify .axiom/ exists.
		if _, err := os.Stat(".axiom"); os.IsNotExist(err) {
			return fmt.Errorf("not an Axiom project. Run 'axiom init' first")
		}

		// Get absolute project root.
		projectRoot, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		fmt.Printf("Starting Axiom project with prompt: %s\n", prompt)
		if cfg.Budget.MaxUSD > 0 {
			fmt.Printf("Budget: $%.2f\n", cfg.Budget.MaxUSD)
		}

		// Initialize the coordinator (wires all subsystems).
		coord, err := engine.NewCoordinator(cfg, projectRoot)
		if err != nil {
			return fmt.Errorf("init coordinator: %w", err)
		}
		defer coord.Stop()

		// Start the coordinator (crash recovery + execution loop).
		if err := coord.Start(); err != nil {
			return fmt.Errorf("start coordinator: %w", err)
		}

		// Check git working tree is clean.
		// Architecture Section 28.2: refuse to start on dirty working tree.
		if err := coord.GitManager().CheckClean(); err != nil {
			coord.Stop()
			return err
		}

		fmt.Println("Engine started. Orchestrator will generate SRS for approval.")
		fmt.Printf("Runtime: %s\n", cfg.Orchestrator.Runtime)
		fmt.Printf("Max concurrent Meeseeks: %d\n", cfg.Concurrency.MaxMeeseeks)

		// The execution loop runs in the background via the coordinator.
		// For now, wait for interrupt signal.
		// In production, this would block until completion or cancellation.
		fmt.Println("\nPress Ctrl+C to stop.")
		select {}
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

		// Try to read state from SQLite if it exists.
		dbPath := filepath.Join(".axiom", "axiom.db")
		if _, err := os.Stat(dbPath); err == nil {
			projectRoot, _ := os.Getwd()
			coord, err := engine.NewCoordinator(cfg, projectRoot)
			if err == nil {
				defer coord.Stop()

				// Show task summary.
				tasks, err := coord.DB().ListTasks(state.TaskFilter{})
				if err == nil && len(tasks) > 0 {
					fmt.Printf("\nTask Tree (%d tasks):\n", len(tasks))
					counts := map[string]int{}
					for _, t := range tasks {
						counts[t.Status]++
					}
					for status, count := range counts {
						fmt.Printf("  %-20s %d\n", status, count)
					}
				}

				// Show budget usage.
				total, err := coord.DB().GetProjectCost()
				if err == nil && total > 0 {
					fmt.Printf("\nSpend: $%.4f / $%.2f (%.1f%%)\n",
						total, cfg.Budget.MaxUSD,
						(total/cfg.Budget.MaxUSD)*100)
				}
			}
		}

		return nil
	},
}

// --- axiom pause ---

var pauseCmd = &cobra.Command{
	Use:   "pause",
	Short: "Pause execution: stop spawning new Meeseeks, let active ones complete",
	Long:  `See Architecture Section 5.3.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		coord, err := loadCoordinator()
		if err != nil {
			return err
		}
		defer coord.Stop()
		coord.Pause()
		fmt.Println("Execution paused. Active Meeseeks will complete their current tasks.")
		fmt.Println("Run 'axiom resume' to continue.")
		return nil
	},
}

// --- axiom resume ---

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a paused project",
	RunE: func(cmd *cobra.Command, args []string) error {
		coord, err := loadCoordinator()
		if err != nil {
			return err
		}
		defer coord.Stop()
		coord.Resume()
		fmt.Println("Execution resumed.")
		return nil
	},
}

// --- axiom cancel ---

var cancelCmd = &cobra.Command{
	Use:   "cancel",
	Short: "Cancel execution, kill containers, revert uncommitted changes",
	Long:  `See Architecture Section 5.3.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		coord, err := loadCoordinator()
		if err != nil {
			return err
		}
		defer coord.Stop()
		if err := coord.Cancel(cmd.Context()); err != nil {
			return fmt.Errorf("cancel: %w", err)
		}
		fmt.Println("Execution cancelled. Containers killed, uncommitted changes reverted.")
		return nil
	},
}

// --- axiom export ---

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export project state as human-readable JSON",
	RunE: func(cmd *cobra.Command, args []string) error {
		coord, err := loadCoordinator()
		if err != nil {
			// If no coordinator, export minimal state.
			exportData := map[string]interface{}{
				"version": Version,
				"status":  "no active project",
			}
			data, _ := json.MarshalIndent(exportData, "", "  ")
			fmt.Println(string(data))
			return nil
		}
		defer coord.Stop()

		tasks, _ := coord.DB().ListTasks(state.TaskFilter{})
		total, _ := coord.DB().GetProjectCost()
		exportData := map[string]interface{}{
			"version":    Version,
			"tasks":      tasks,
			"total_cost": total,
			"config":     coord.Config(),
		}
		data, err := json.MarshalIndent(exportData, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	},
}

// loadCoordinator creates a Coordinator for the current project directory.
// Used by commands that need to interact with an existing project.
func loadCoordinator() (*engine.Coordinator, error) {
	if _, err := os.Stat(".axiom"); os.IsNotExist(err) {
		return nil, fmt.Errorf("not an Axiom project. Run 'axiom init' first")
	}
	cfg, err := engine.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	projectRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("get working directory: %w", err)
	}
	return engine.NewCoordinator(cfg, projectRoot)
}
