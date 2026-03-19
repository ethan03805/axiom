package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ethan03805/axiom/internal/index"
	"github.com/ethan03805/axiom/internal/state"
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

// openIndexer opens the project SQLite database and creates an Indexer
// with the Go parser registered. Caller must close the returned state.DB.
func openIndexer() (*index.Indexer, *state.DB, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("get working directory: %w", err)
	}
	dbPath := filepath.Join(cwd, ".axiom", "axiom.db")

	db, err := state.NewDB(dbPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}

	idx := index.NewIndexer(db.Conn())
	if err := idx.InitSchema(); err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("init index schema: %w", err)
	}

	idx.RegisterParser(index.NewGoParser())
	return idx, db, nil
}

var indexRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Force full re-index of the project",
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		idx, db, err := openIndexer()
		if err != nil {
			return err
		}
		defer db.Close()

		fmt.Println("Re-indexing project...")
		if err := idx.FullIndex(cwd); err != nil {
			return fmt.Errorf("full index: %w", err)
		}

		symCount, _ := idx.SymbolCount()
		fileCount, _ := idx.FileCount()
		fmt.Printf("Index refreshed: %d symbols across %d files.\n", symCount, fileCount)
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

		idx, db, err := openIndexer()
		if err != nil {
			return err
		}
		defer db.Close()

		req := &index.QueryRequest{
			QueryType: index.QueryType(queryType),
			Params: index.QueryParams{
				Name:          queryName,
				SymbolName:    queryName,
				PackagePath:   queryPackage,
				InterfaceName: queryName,
				RootPackage:   queryPackage,
			},
		}

		result, err := idx.ExecuteQuery(req)
		if err != nil {
			return fmt.Errorf("execute query: %w", err)
		}

		if len(result.Results) == 0 {
			fmt.Println("No results found.")
			return nil
		}

		data, err := json.MarshalIndent(result.Results, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal results: %w", err)
		}
		fmt.Println(string(data))
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
