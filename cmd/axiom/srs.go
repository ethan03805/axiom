package main

import (
	"fmt"

	"github.com/ethan03805/axiom/internal/engine"
	"github.com/spf13/cobra"
)

var srsCmd = &cobra.Command{
	Use:   "srs",
	Short: "SRS management commands",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var srsApproveCmd = &cobra.Command{
	Use:   "approve",
	Short: "Approve the generated SRS document",
	Long: `Approves the SRS at .axiom/srs.md, locking it as immutable.
This triggers task decomposition and execution to begin.
See Architecture Section 5.1 step 4.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		coord, err := loadCoordinator()
		if err != nil {
			return err
		}
		defer coord.Stop()

		hash, err := coord.ApproveSRS("user")
		if err != nil {
			return fmt.Errorf("approve SRS: %w", err)
		}
		fmt.Printf("SRS approved and locked (SHA-256: %s)\n", hash[:16])
		fmt.Println("Task decomposition and execution will begin.")
		return nil
	},
}

var srsRejectCmd = &cobra.Command{
	Use:   "reject [feedback]",
	Short: "Reject the generated SRS with feedback",
	Long: `Rejects the SRS draft with feedback for the orchestrator to revise.
See Architecture Section 5.1 step 4.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := engine.LoadConfig()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		_ = cfg

		coord, err := loadCoordinator()
		if err != nil {
			return err
		}
		defer coord.Stop()

		if err := coord.RejectSRS(args[0]); err != nil {
			return fmt.Errorf("reject SRS: %w", err)
		}
		fmt.Println("SRS rejected. The orchestrator will revise based on your feedback.")
		return nil
	},
}

func init() {
	srsCmd.AddCommand(srsApproveCmd)
	srsCmd.AddCommand(srsRejectCmd)
}
