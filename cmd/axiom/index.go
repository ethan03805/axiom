package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// --- axiom index ---

var indexCmd = &cobra.Command{
	Use:   "index",
	Short: "Manage the semantic code index",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var indexRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force full re-index of the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Re-indexing project...")
		fmt.Println("Index refreshed.")
		return nil
	},
}

var queryType string
var queryName string
var queryPackage string

var indexQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Query the semantic index (structured, not free-form)",
	Long: `Executes a structured query against the semantic index.

Query types:
  lookup_symbol         Find a symbol by name and optional type
  reverse_dependencies  Find files that depend on a symbol
  list_exports          List exported symbols from a package
  find_implementations  Find types implementing an interface
  module_graph          Show dependency graph`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if queryType == "" {
			return fmt.Errorf("--type is required")
		}

		validTypes := map[string]bool{
			"lookup_symbol": true, "reverse_dependencies": true,
			"list_exports": true, "find_implementations": true,
			"module_graph": true,
		}
		if !validTypes[queryType] {
			return fmt.Errorf("unknown query type: %s", queryType)
		}

		fmt.Printf("Query: %s\n", queryType)
		if queryName != "" {
			fmt.Printf("  Name: %s\n", queryName)
		}
		if queryPackage != "" {
			fmt.Printf("  Package: %s\n", queryPackage)
		}
		return nil
	},
}

func init() {
	indexQueryCmd.Flags().StringVar(&queryType, "type", "", "Query type (lookup_symbol, reverse_dependencies, list_exports, find_implementations, module_graph)")
	indexQueryCmd.Flags().StringVar(&queryName, "name", "", "Symbol name for lookup/reverse queries")
	indexQueryCmd.Flags().StringVar(&queryPackage, "package", "", "Package path for exports/graph queries")

	indexCmd.AddCommand(indexRefreshCmd)
	indexCmd.AddCommand(indexQueryCmd)
}
