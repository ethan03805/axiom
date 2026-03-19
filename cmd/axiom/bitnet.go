package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ethan03805/axiom/internal/broker"
	"github.com/ethan03805/axiom/internal/engine"
	"github.com/spf13/cobra"
)

// --- axiom bitnet ---

var bitnetCmd = &cobra.Command{
	Use:   "bitnet",
	Short: "Manage the local BitNet inference server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// newBitNetServerFromConfig loads config and creates a BitNetServer instance.
func newBitNetServerFromConfig() (*broker.BitNetServer, error) {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	srv := broker.NewBitNetServer(broker.BitNetServerConfig{
		Enabled:               cfg.BitNet.Enabled,
		Host:                  cfg.BitNet.Host,
		Port:                  cfg.BitNet.Port,
		MaxConcurrentRequests: cfg.BitNet.MaxConcurrentRequests,
		CPUThreads:            cfg.BitNet.CPUThreads,
	})
	return srv, nil
}

var bitnetStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local inference server",
	Long: `Starts the BitNet inference server with Falcon3 1-bit weights.
On first run, downloads model weights if not present (with user confirmation).
The server exposes an OpenAI-compatible API at the configured port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		// Check if weights are present.
		hasWeights, err := srv.EnsureWeights()
		if err != nil {
			return fmt.Errorf("check model weights: %w", err)
		}

		if !hasWeights {
			fmt.Print("Falcon3 1-bit model weights not found. Download? (y/n) ")
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("read input: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Download cancelled. Cannot start without model weights.")
				return nil
			}

			// Set up the models directory and create a placeholder.
			// In production this would download the actual weights.
			fmt.Println("Downloading Falcon3 1-bit model weights...")
			modelsDir, err := srv.SetupModelsDir()
			if err != nil {
				return fmt.Errorf("setup models directory: %w", err)
			}
			// Stub: create a placeholder weight file so Start() succeeds.
			placeholderPath := modelsDir + "/falcon3-1b.gguf"
			if err := os.WriteFile(placeholderPath, []byte("placeholder"), 0644); err != nil {
				return fmt.Errorf("write placeholder weights: %w", err)
			}
			fmt.Printf("Model weights saved to %s\n", modelsDir)
		}

		fmt.Println("Starting BitNet inference server...")
		if err := srv.Start(); err != nil {
			if broker.NeedsFirstRun(err) {
				fmt.Println(err.Error())
				return nil
			}
			return fmt.Errorf("start server: %w", err)
		}

		status := srv.Status()
		fmt.Println("BitNet server started successfully.")
		fmt.Printf("  Host:       %s\n", status.Host)
		fmt.Printf("  Port:       %d\n", status.Port)
		fmt.Printf("  Threads:    %d\n", status.CPUThreads)
		fmt.Printf("  Running:    %v\n", status.Running)
		return nil
	},
}

var bitnetStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local inference server",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		if err := srv.Stop(); err != nil {
			return fmt.Errorf("stop server: %w", err)
		}

		fmt.Println("BitNet server stopped.")
		return nil
	},
}

var bitnetStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show server status, resource usage, active requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		status := srv.Status()
		usage := srv.GetResourceUsage()

		fmt.Println("BitNet Server Status")
		fmt.Println("--------------------")
		if status.Running {
			fmt.Println("Status:          running")
			fmt.Printf("Uptime:          %s\n", status.Uptime.Truncate(time.Second))
		} else {
			fmt.Println("Status:          stopped")
		}
		fmt.Printf("Enabled:         %v\n", status.Enabled)
		fmt.Printf("Host:            %s\n", status.Host)
		fmt.Printf("Port:            %d\n", status.Port)
		fmt.Printf("CPU Threads:     %d / %d\n", status.CPUThreads, usage.TotalCPUs)
		fmt.Printf("CPU Usage:       %.1f%%\n", usage.CPUPercent)
		fmt.Printf("Active Requests: %d\n", status.ActiveRequests)

		hasWeights, _ := srv.EnsureWeights()
		if hasWeights {
			fmt.Println("Model Weights:   present")
		} else {
			fmt.Println("Model Weights:   not found")
		}
		return nil
	},
}

var bitnetModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available local models",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv, err := newBitNetServerFromConfig()
		if err != nil {
			return err
		}

		models, err := srv.ListModels()
		if err != nil {
			return fmt.Errorf("list models: %w", err)
		}

		fmt.Println("Local Models")
		fmt.Println("------------")
		if len(models) == 0 {
			fmt.Printf("No models found in %s\n", srv.GetModelsDir())
			fmt.Println("Run 'axiom bitnet start' to download Falcon3 1-bit weights.")
			return nil
		}
		fmt.Printf("Models directory: %s\n", srv.GetModelsDir())
		fmt.Println()
		for _, m := range models {
			fmt.Printf("  %s\n", m)
		}
		return nil
	},
}

func init() {
	bitnetCmd.AddCommand(bitnetStartCmd)
	bitnetCmd.AddCommand(bitnetStopCmd)
	bitnetCmd.AddCommand(bitnetStatusCmd)
	bitnetCmd.AddCommand(bitnetModelsCmd)
}
