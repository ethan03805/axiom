package main

import (
	"fmt"

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

var bitnetStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the local inference server",
	Long: `Starts the BitNet inference server with Falcon3 1-bit weights.
On first run, downloads model weights if not present (with user confirmation).
The server exposes an OpenAI-compatible API at the configured port.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting BitNet inference server...")
		fmt.Println("(Server lifecycle managed by broker.BitNetServer)")
		return nil
	},
}

var bitnetStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the local inference server",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Stopping BitNet inference server...")
		return nil
	},
}

var bitnetStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show server status, resource usage, active requests",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("BitNet Server Status")
		fmt.Println("--------------------")
		fmt.Println("Status:  not running")
		fmt.Println("Host:    localhost")
		fmt.Println("Port:    3002")
		return nil
	},
}

var bitnetModelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available local models",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Local Models")
		fmt.Println("------------")
		fmt.Println("(No models found. Run 'axiom bitnet start' to download.)")
		return nil
	},
}

func init() {
	bitnetCmd.AddCommand(bitnetStartCmd)
	bitnetCmd.AddCommand(bitnetStopCmd)
	bitnetCmd.AddCommand(bitnetStatusCmd)
	bitnetCmd.AddCommand(bitnetModelsCmd)
}
