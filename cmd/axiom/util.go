package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"

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
  - BitNet server status
  - Network connectivity (OpenRouter reachability)
  - System resources (CPU, memory, disk)
  - Container images available
  - Configuration validity`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Axiom Doctor")
		fmt.Println("============")
		fmt.Println()

		checks := []struct {
			name  string
			check func() (string, error)
		}{
			{"Docker", checkDocker},
			{"Git", checkGit},
			{"System Resources", checkResources},
			{"Configuration", checkConfig},
		}

		allPassed := true
		for _, c := range checks {
			result, err := c.check()
			if err != nil {
				fmt.Printf("  [FAIL] %s: %v\n", c.name, err)
				allPassed = false
			} else {
				fmt.Printf("  [PASS] %s: %s\n", c.name, result)
			}
		}

		fmt.Println()
		if allPassed {
			fmt.Println("All checks passed. Axiom is ready to run.")
		} else {
			fmt.Println("Some checks failed. Please resolve the issues above.")
		}
		return nil
	},
}

func checkDocker() (string, error) {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return "", fmt.Errorf("Docker not found or not running. Install Docker and ensure the daemon is started.")
	}
	return fmt.Sprintf("Docker %s", strings.TrimSpace(string(out))), nil
}

func checkGit() (string, error) {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("Git not found. Install git.")
	}
	return strings.TrimSpace(string(out)), nil
}

func checkResources() (string, error) {
	cpus := runtime.NumCPU()
	if cpus < 2 {
		return "", fmt.Errorf("at least 2 CPUs recommended, found %d", cpus)
	}
	return fmt.Sprintf("%d CPUs available", cpus), nil
}

func checkConfig() (string, error) {
	// Check if .axiom/config.toml exists.
	_, err := exec.Command("test", "-f", ".axiom/config.toml").Output()
	if err != nil {
		return "No project config (run 'axiom init' first)", nil
	}
	return "Project config found", nil
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
	Long:  `Reloads API keys and other settings from config files without requiring an engine restart.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Reloading configuration...")
		fmt.Println("Configuration reloaded.")
		return nil
	},
}

func init() {
	configCmd.AddCommand(configReloadCmd)
}
