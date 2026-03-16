package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// --- axiom api ---

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Manage the REST + WebSocket API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var apiStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the REST + WebSocket API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting API server...")
		return nil
	},
}

var apiStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Stopping API server...")
		return nil
	},
}

// --- axiom api token ---

var apiTokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage API authentication tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var tokenScope string
var tokenExpires string

var apiTokenGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Generate a new API authentication token",
	Long: `Generates a new API token for authenticating Claw or other external
orchestrators. Tokens are prefixed with 'axm_sk_'.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Generating API token (scope: %s, expires: %s)...\n", tokenScope, tokenExpires)
		fmt.Println("Token: axm_sk_<generated-token>")
		fmt.Println("Store this token securely. It will not be shown again.")
		return nil
	},
}

var apiTokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active API tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("%-20s %-15s %-12s %s\n", "TOKEN ID", "SCOPE", "EXPIRES", "CREATED")
		fmt.Println("--------------------------------------------------------------")
		fmt.Println("(No active tokens)")
		return nil
	},
}

var apiTokenRevokeCmd = &cobra.Command{
	Use:   "revoke [token-id]",
	Short: "Revoke a specific API token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Revoked token: %s\n", args[0])
		return nil
	},
}

// --- axiom tunnel ---

var tunnelCmd = &cobra.Command{
	Use:   "tunnel",
	Short: "Manage Cloudflare Tunnel for remote Claw access",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var tunnelStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Cloudflare Tunnel for remote Claw access",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Starting Cloudflare Tunnel...")
		fmt.Println("Public URL: https://<random>.trycloudflare.com")
		return nil
	},
}

var tunnelStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Cloudflare Tunnel",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Stopping tunnel...")
		return nil
	},
}

func init() {
	apiTokenGenerateCmd.Flags().StringVar(&tokenScope, "scope", "full-control", "Token scope: read-only or full-control")
	apiTokenGenerateCmd.Flags().StringVar(&tokenExpires, "expires", "24h", "Token expiration duration")

	apiTokenCmd.AddCommand(apiTokenGenerateCmd)
	apiTokenCmd.AddCommand(apiTokenListCmd)
	apiTokenCmd.AddCommand(apiTokenRevokeCmd)

	apiCmd.AddCommand(apiStartCmd)
	apiCmd.AddCommand(apiStopCmd)
	apiCmd.AddCommand(apiTokenCmd)

	tunnelCmd.AddCommand(tunnelStartCmd)
	tunnelCmd.AddCommand(tunnelStopCmd)
}
