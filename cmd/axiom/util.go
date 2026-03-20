package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"github.com/ethan03805/axiom/internal/doctor"
	"github.com/ethan03805/axiom/internal/engine"
	"github.com/spf13/cobra"
)

// --- axiom version ---

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show Axiom version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("axiom version %s\n", Version)
		fmt.Printf("  go:       %s\n", runtime.Version())
		fmt.Printf("  os/arch:  %s/%s\n", runtime.GOOS, runtime.GOARCH)
		return nil
	},
}

// --- axiom doctor ---

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system requirements (Docker, BitNet, network, resources)",
	Long: `Validates that all system prerequisites are met for running Axiom:
  - Docker daemon availability and version
  - Git availability
  - BitNet server status
  - Network connectivity (OpenRouter reachability)
  - System resources (CPU, memory, disk)
  - Container images available
  - Secret scanner regex patterns
  - Configuration validity

See Architecture Section 27.7.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := engine.LoadConfig()
		if err != nil {
			// Use defaults if config cannot be loaded.
			cfg = engine.DefaultConfig()
		}

		projectRoot := "."

		d := doctor.New(doctor.DoctorConfig{
			BitNetHost:        cfg.BitNet.Host,
			BitNetPort:        cfg.BitNet.Port,
			DockerImage:       cfg.Docker.Image,
			SensitivePatterns: cfg.Security.SensitivePatterns,
			WarmPoolEnabled:   cfg.Validation.WarmPoolEnabled,
			WarmPoolImage:     cfg.Docker.Image,
			ProjectRoot:       projectRoot,
			OpenRouterAPIKey:  cfg.OpenRouter.APIKey,
		})

		report := d.Run()
		doctor.PrintReport(report)
		return nil
	},
}

// --- axiom config ---

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configuration management",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var configReloadCmd = &cobra.Command{
	Use:   "reload",
	Short: "Reload configuration without engine restart",
	Long: `Reloads API keys and other settings from config files without requiring
an engine restart. Reads both global (~/.axiom/config.toml) and project
(.axiom/config.toml) configuration files, validates them, and reports
the active settings.

See Architecture Section 19.5 (Credential Management).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Reloading configuration...")

		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("reload config: %w", err)
		}

		// Try to signal the running engine process via PID file.
		pidPath := filepath.Join(".axiom", "engine.pid")
		pidData, err := os.ReadFile(pidPath)
		if err == nil {
			pidStr := strings.TrimSpace(string(pidData))
			if pid, parseErr := strconv.Atoi(pidStr); parseErr == nil {
				proc, findErr := os.FindProcess(pid)
				if findErr == nil {
					if sigErr := proc.Signal(syscall.SIGHUP); sigErr == nil {
						fmt.Printf("Sent reload signal to running engine (PID %d).\n", pid)
					} else {
						fmt.Printf("Could not signal engine process %d: %v\n", pid, sigErr)
						fmt.Println("If no engine is running, the new config will take effect on next 'axiom run'.")
					}
				}
			}
		} else {
			fmt.Println("No running engine detected (no PID file).")
			fmt.Println("New configuration will take effect on next 'axiom run'.")
		}

		fmt.Println("Configuration reloaded successfully.")
		fmt.Printf("  Project:       %s\n", cfg.Project.Name)
		fmt.Printf("  Budget:        $%.2f\n", cfg.Budget.MaxUSD)
		fmt.Printf("  Max Meeseeks:  %d\n", cfg.Concurrency.MaxMeeseeks)
		fmt.Printf("  Runtime:       %s\n", cfg.Orchestrator.Runtime)
		fmt.Printf("  Docker Image:  %s\n", cfg.Docker.Image)
		fmt.Printf("  BitNet:        %v (port %d)\n", cfg.BitNet.Enabled, cfg.BitNet.Port)
		fmt.Printf("  API Port:      %d\n", cfg.API.Port)
		return nil
	},
}

func init() {
	configCmd.AddCommand(configReloadCmd)
}
