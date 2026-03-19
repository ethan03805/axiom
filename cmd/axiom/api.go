package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ethan03805/axiom/internal/api"
	"github.com/ethan03805/axiom/internal/engine"
	"github.com/ethan03805/axiom/internal/tunnel"
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
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		coord, err := engine.NewCoordinator(cfg, cwd)
		if err != nil {
			return fmt.Errorf("create coordinator: %w", err)
		}

		port := cfg.API.Port
		if port == 0 {
			port = 3000
		}
		rateLimit := cfg.API.RateLimitRPM
		if rateLimit == 0 {
			rateLimit = 120
		}

		srv := api.NewServer(api.ServerConfig{
			Port:         port,
			RateLimitRPM: rateLimit,
			AllowedIPs:   cfg.API.AllowedIPs,
		}, coord.Emitter())

		if err := srv.Start(); err != nil {
			return fmt.Errorf("start api server: %w", err)
		}

		fmt.Printf("API server started on port %d\n", port)
		fmt.Println("Press Ctrl+C to stop.")

		// Block until interrupted.
		select {}
	},
}

var apiStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the API server",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("To stop the API server, terminate the process running 'axiom api start' (e.g. Ctrl+C or kill the PID).")
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
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		storageDir := filepath.Join(home, ".axiom", "api-tokens")

		ta, err := api.NewTokenAuthWithStorage(storageDir)
		if err != nil {
			return fmt.Errorf("init token storage: %w", err)
		}

		scope := api.TokenScope(tokenScope)
		if scope != api.ScopeReadOnly && scope != api.ScopeFullControl {
			scope = api.ScopeFullControl
		}

		expDur, err := time.ParseDuration(tokenExpires)
		if err != nil {
			expDur = 24 * time.Hour
		}

		info, tokenValue := ta.Generate(scope, expDur)

		fmt.Printf("Token generated successfully.\n")
		fmt.Printf("  ID:      %s\n", info.ID)
		fmt.Printf("  Scope:   %s\n", info.Scope)
		fmt.Printf("  Expires: %s\n", info.ExpiresAt.Format(time.RFC3339))
		fmt.Printf("  Token:   %s\n", tokenValue)
		fmt.Println("\nStore this token securely. It will not be shown again.")
		return nil
	},
}

var apiTokenListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all active API tokens",
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		storageDir := filepath.Join(home, ".axiom", "api-tokens")

		ta, err := api.NewTokenAuthWithStorage(storageDir)
		if err != nil {
			return fmt.Errorf("init token storage: %w", err)
		}

		tokens := ta.List()
		fmt.Printf("%-20s %-15s %-25s %s\n", "TOKEN ID", "SCOPE", "EXPIRES", "CREATED")
		fmt.Println("--------------------------------------------------------------")
		if len(tokens) == 0 {
			fmt.Println("(No active tokens)")
		}
		for _, t := range tokens {
			fmt.Printf("%-20s %-15s %-25s %s\n",
				t.ID,
				t.Scope,
				t.ExpiresAt.Format(time.RFC3339),
				t.CreatedAt.Format(time.RFC3339),
			)
		}
		return nil
	},
}

var apiTokenRevokeCmd = &cobra.Command{
	Use:   "revoke [token-id]",
	Short: "Revoke a specific API token",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("get home dir: %w", err)
		}
		storageDir := filepath.Join(home, ".axiom", "api-tokens")

		ta, err := api.NewTokenAuthWithStorage(storageDir)
		if err != nil {
			return fmt.Errorf("init token storage: %w", err)
		}

		if err := ta.Revoke(args[0]); err != nil {
			return fmt.Errorf("revoke token: %w", err)
		}

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

// tunnelMgr is the shared tunnel manager instance used by start/stop commands.
var tunnelMgr *tunnel.Manager

var tunnelStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start Cloudflare Tunnel for remote Claw access",
	Long: `Starts a Cloudflare Tunnel pointing to the local API server.
Requires cloudflared to be installed. The tunnel provides a public URL
for remote Claw orchestrator connections.

See Architecture Section 24.4.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		port := cfg.API.Port
		if port == 0 {
			port = 3000
		}

		fmt.Println("Starting Cloudflare Tunnel...")
		tunnelMgr = tunnel.NewManager(port)
		publicURL, err := tunnelMgr.Start()
		if err != nil {
			return fmt.Errorf("start tunnel: %w", err)
		}

		fmt.Printf("Public URL: %s\n", publicURL)
		fmt.Println("Press Ctrl+C to stop.")

		// Block until interrupted, then clean up.
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		fmt.Println("\nStopping tunnel...")
		if err := tunnelMgr.Stop(); err != nil {
			return fmt.Errorf("stop tunnel: %w", err)
		}
		fmt.Println("Tunnel stopped.")
		return nil
	},
}

var tunnelStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Cloudflare Tunnel",
	Long: `Stops a running Cloudflare Tunnel. If no tunnel is running in this
process, prints guidance on how to stop it.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if tunnelMgr != nil && tunnelMgr.IsRunning() {
			if err := tunnelMgr.Stop(); err != nil {
				return fmt.Errorf("stop tunnel: %w", err)
			}
			fmt.Println("Tunnel stopped.")
			return nil
		}
		fmt.Println("No tunnel is running in this process.")
		fmt.Println("To stop a tunnel started in another terminal, terminate that process (Ctrl+C or kill the PID).")
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
