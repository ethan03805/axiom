package index

import (
	"encoding/json"
	"fmt"
)

// QueryType identifies the kind of index query.
// See Architecture Section 17.5 for the typed query API.
type QueryType string

const (
	QueryLookupSymbol       QueryType = "lookup_symbol"
	QueryReverseDependencies QueryType = "reverse_dependencies"
	QueryListExports        QueryType = "list_exports"
	QueryFindImplementations QueryType = "find_implementations"
	QueryModuleGraph        QueryType = "module_graph"
)

// QueryRequest represents a structured query to the semantic index.
type QueryRequest struct {
	QueryType QueryType `json:"query_type"`
	Params    QueryParams `json:"params"`
}

// QueryParams holds the parameters for a query.
type QueryParams struct {
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`         // "function", "type", "interface"
	SymbolName  string `json:"symbol_name,omitempty"`
	PackagePath string `json:"package_path,omitempty"`
	InterfaceName string `json:"interface_name,omitempty"`
	RootPackage string `json:"root_package,omitempty"`
}

// QueryResult holds the result of an index query.
type QueryResult struct {
	Results []map[string]interface{} `json:"results"`
}

// LookupSymbol finds a symbol by name and optional kind.
// Returns: file, line, signature, exported status.
// See Architecture Section 17.5.
func (idx *Indexer) LookupSymbol(name string, kind string) (*QueryResult, error) {
	query := `SELECT name, kind, file_path, line, signature, exported, package_path
	          FROM idx_symbols WHERE name = ?`
	args := []interface{}{name}
	if kind != "" {
		query += " AND kind = ?"
		args = append(args, kind)
	}

	rows, err := idx.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("lookup_symbol: %w", err)
	}
	defer rows.Close()

	result := &QueryResult{}
	for rows.Next() {
		var name, kind, file, sig, pkg string
		var line int
		var exported bool
		if err := rows.Scan(&name, &kind, &file, &line, &sig, &exported, &pkg); err != nil {
			return nil, fmt.Errorf("scan symbol: %w", err)
		}
		result.Results = append(result.Results, map[string]interface{}{
			"name":      name,
			"kind":      kind,
			"file":      file,
			"line":      line,
			"signature": sig,
			"exported":  exported,
			"package":   pkg,
		})
	}
	return result, rows.Err()
}

// ReverseDependencies finds all files/symbols that import or reference a symbol.
// Returns: list of files/symbols that depend on the given symbol.
// See Architecture Section 17.5.
func (idx *Indexer) ReverseDependencies(symbolName string) (*QueryResult, error) {
	// Find the package that contains this symbol.
	var symPkg string
	err := idx.db.QueryRow(
		"SELECT package_path FROM idx_symbols WHERE name = ? LIMIT 1", symbolName,
	).Scan(&symPkg)
	if err != nil {
		return &QueryResult{}, nil // Symbol not found; return empty
	}

	// Find all files that import this package.
	rows, err := idx.db.Query(`
		SELECT DISTINCT i.file_path, s.name, 'import' as usage
		FROM idx_imports i
		LEFT JOIN idx_symbols s ON s.file_path = i.file_path
		WHERE i.import_path LIKE ?
		ORDER BY i.file_path`,
		"%"+symPkg+"%",
	)
	if err != nil {
		return nil, fmt.Errorf("reverse_dependencies: %w", err)
	}
	defer rows.Close()

	result := &QueryResult{}
	seen := make(map[string]bool)
	for rows.Next() {
		var file string
		var sym *string
		var usage string
		if err := rows.Scan(&file, &sym, &usage); err != nil {
			return nil, fmt.Errorf("scan dep: %w", err)
		}
		key := file
		if seen[key] {
			continue
		}
		seen[key] = true

		entry := map[string]interface{}{
			"file":  file,
			"usage": usage,
		}
		if sym != nil {
			entry["symbol"] = *sym
		}
		result.Results = append(result.Results, entry)
	}
	return result, rows.Err()
}

// ListExports returns all exported symbols from a package.
// See Architecture Section 17.5.
func (idx *Indexer) ListExports(packagePath string) (*QueryResult, error) {
	rows, err := idx.db.Query(`
		SELECT name, kind, file_path, line, signature
		FROM idx_symbols
		WHERE package_path = ? AND exported = 1
		ORDER BY kind, name`,
		packagePath,
	)
	if err != nil {
		return nil, fmt.Errorf("list_exports: %w", err)
	}
	defer rows.Close()

	result := &QueryResult{}
	for rows.Next() {
		var name, kind, file, sig string
		var line int
		if err := rows.Scan(&name, &kind, &file, &line, &sig); err != nil {
			return nil, fmt.Errorf("scan export: %w", err)
		}
		result.Results = append(result.Results, map[string]interface{}{
			"name":      name,
			"kind":      kind,
			"file":      file,
			"line":      line,
			"signature": sig,
		})
	}
	return result, rows.Err()
}

// FindImplementations finds all types that implement a given interface.
// Compares the method sets of types against the interface's method signatures.
// See Architecture Section 17.5.
func (idx *Indexer) FindImplementations(interfaceName string) (*QueryResult, error) {
	// Get the interface's method names.
	ifaceMethods, err := idx.db.Query(`
		SELECT f.name FROM idx_fields f
		JOIN idx_symbols s ON s.id = f.symbol_id
		WHERE s.name = ? AND s.kind = 'interface'`,
		interfaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("find_implementations query interface: %w", err)
	}

	var requiredMethods []string
	for ifaceMethods.Next() {
		var m string
		ifaceMethods.Scan(&m)
		requiredMethods = append(requiredMethods, m)
	}
	ifaceMethods.Close()

	if len(requiredMethods) == 0 {
		return &QueryResult{}, nil
	}

	// Find types that have methods matching all interface methods.
	// A type implements an interface if it has all required methods.
	typeRows, err := idx.db.Query(`
		SELECT DISTINCT s.name, s.file_path, s.line, s.package_path
		FROM idx_symbols s
		WHERE s.kind = 'type' AND s.name != ?`,
		interfaceName,
	)
	if err != nil {
		return nil, fmt.Errorf("find_implementations query types: %w", err)
	}
	defer typeRows.Close()

	result := &QueryResult{}
	for typeRows.Next() {
		var name, file, pkg string
		var line int
		if err := typeRows.Scan(&name, &file, &line, &pkg); err != nil {
			continue
		}

		// Check if this type has all required methods.
		// Methods are functions with parent_name matching the type name.
		var methodCount int
		for _, method := range requiredMethods {
			var count int
			idx.db.QueryRow(
				`SELECT COUNT(*) FROM idx_symbols
				 WHERE parent_name = ? AND name = ? AND kind = 'function'`,
				name, method,
			).Scan(&count)
			if count > 0 {
				methodCount++
			}
		}

		if methodCount == len(requiredMethods) {
			result.Results = append(result.Results, map[string]interface{}{
				"type":    name,
				"file":    file,
				"line":    line,
				"package": pkg,
			})
		}
	}
	return result, nil
}

// ModuleGraph returns the dependency graph for the project or a specific package.
// See Architecture Section 17.5.
func (idx *Indexer) ModuleGraph(rootPackage string) (*QueryResult, error) {
	query := `SELECT DISTINCT file_path, import_path FROM idx_imports`
	var args []interface{}
	if rootPackage != "" {
		query += ` WHERE file_path IN (
			SELECT DISTINCT file_path FROM idx_symbols WHERE package_path = ?
		)`
		args = append(args, rootPackage)
	}
	query += " ORDER BY file_path, import_path"

	rows, err := idx.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("module_graph: %w", err)
	}
	defer rows.Close()

	result := &QueryResult{}
	for rows.Next() {
		var file, imp string
		if err := rows.Scan(&file, &imp); err != nil {
			return nil, fmt.Errorf("scan graph edge: %w", err)
		}
		result.Results = append(result.Results, map[string]interface{}{
			"file":       file,
			"imports":    imp,
		})
	}
	return result, rows.Err()
}

// ExecuteQuery dispatches a structured query request to the appropriate query method.
// This is the main entry point for IPC query_index requests.
func (idx *Indexer) ExecuteQuery(req *QueryRequest) (*QueryResult, error) {
	switch req.QueryType {
	case QueryLookupSymbol:
		return idx.LookupSymbol(req.Params.Name, req.Params.Type)
	case QueryReverseDependencies:
		return idx.ReverseDependencies(req.Params.SymbolName)
	case QueryListExports:
		return idx.ListExports(req.Params.PackagePath)
	case QueryFindImplementations:
		return idx.FindImplementations(req.Params.InterfaceName)
	case QueryModuleGraph:
		return idx.ModuleGraph(req.Params.RootPackage)
	default:
		return nil, fmt.Errorf("unknown query type: %s", req.QueryType)
	}
}

// HandleQueryIndex is the IPC handler for query_index messages.
// Designed to be registered with the IPC Dispatcher.
func (idx *Indexer) HandleQueryIndex(taskID string, msg interface{}, raw []byte) (interface{}, error) {
	// Parse the action request to extract query parameters.
	var req QueryRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("parse query request: %w", err)
	}

	result, err := idx.ExecuteQuery(&req)
	if err != nil {
		return nil, err
	}

	return result, nil
}
