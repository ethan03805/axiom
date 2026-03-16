// Package index implements the Semantic Indexer that maintains a structured,
// queryable index of the project's code symbols, exports, interfaces, and
// dependency relationships. It enables the orchestrator to build precise
// TaskSpecs without reading raw source files.
//
// See Architecture.md Section 17 for the full specification.
package index

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SymbolKind identifies the type of a code symbol.
type SymbolKind string

const (
	KindFunction  SymbolKind = "function"
	KindType      SymbolKind = "type"
	KindInterface SymbolKind = "interface"
	KindConstant  SymbolKind = "constant"
	KindVariable  SymbolKind = "variable"
)

// Symbol represents a code symbol extracted from a source file.
// See Architecture Section 17.3 for the index contents.
type Symbol struct {
	Name        string
	Kind        SymbolKind
	FilePath    string
	Line        int
	EndLine     int
	Signature   string
	ReturnType  string
	Exported    bool
	ParentName  string // Enclosing type for methods
	PackagePath string
	Fields      []Field // For types/structs
}

// Field represents a field within a type/struct.
type Field struct {
	Name     string
	TypeName string
	Exported bool
}

// Import represents a file's import of a package/module.
type Import struct {
	FilePath   string
	ImportPath string
	Alias      string
}

// Dependency represents a package-level dependency.
type Dependency struct {
	FromPkg string
	ToPkg   string
	DepType string // "import" | "reference"
}

// Parser extracts symbols and imports from a source file.
// Different language backends (Go stdlib, tree-sitter) implement this interface.
type Parser interface {
	// Parse extracts symbols and imports from the file at the given path.
	Parse(filePath string) ([]Symbol, []Import, error)
	// SupportedExtensions returns the file extensions this parser handles.
	SupportedExtensions() []string
}

// Indexer maintains the semantic index in SQLite. It coordinates parsing,
// storage, and querying of code symbols.
type Indexer struct {
	db      *sql.DB
	parsers map[string]Parser // extension -> parser
}

// NewIndexer creates an Indexer using the given SQLite database connection.
// The database must already have the index schema tables created.
func NewIndexer(db *sql.DB) *Indexer {
	idx := &Indexer{
		db:      db,
		parsers: make(map[string]Parser),
	}
	return idx
}

// InitSchema creates the index tables if they don't exist.
func (idx *Indexer) InitSchema() error {
	_, err := idx.db.Exec(IndexSchema)
	if err != nil {
		return fmt.Errorf("init index schema: %w", err)
	}
	return nil
}

// RegisterParser adds a parser for the given file extensions.
func (idx *Indexer) RegisterParser(p Parser) {
	for _, ext := range p.SupportedExtensions() {
		idx.parsers[ext] = p
	}
}

// FullIndex performs a complete index of all supported files under rootDir.
// This clears the existing index and rebuilds from scratch.
// See Architecture Section 17.4: called on project initialization and `axiom index refresh`.
func (idx *Indexer) FullIndex(rootDir string) error {
	// Clear existing index.
	if err := idx.clearIndex(); err != nil {
		return fmt.Errorf("clear index: %w", err)
	}

	// Walk the project directory and index supported files.
	return filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip unreadable files
		}
		if info.IsDir() {
			// Exclude .axiom/ directory per Architecture Section 28.2.
			if info.Name() == ".axiom" {
				return filepath.SkipDir
			}
			// Exclude common non-source directories.
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		return idx.indexFile(path, relPath)
	})
}

// IncrementalIndex re-indexes only the specified files.
// See Architecture Section 17.4: called after each merge queue commit.
func (idx *Indexer) IncrementalIndex(rootDir string, changedFiles []string) error {
	for _, relPath := range changedFiles {
		// Remove old entries for this file.
		if err := idx.removeFileEntries(relPath); err != nil {
			return fmt.Errorf("remove old entries for %s: %w", relPath, err)
		}

		fullPath := filepath.Join(rootDir, relPath)

		// If the file was deleted, we're done (entries removed).
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			continue
		}

		// Re-index the file.
		if err := idx.indexFile(fullPath, relPath); err != nil {
			return fmt.Errorf("reindex %s: %w", relPath, err)
		}
	}
	return nil
}

// indexFile parses a single file and stores its symbols and imports.
func (idx *Indexer) indexFile(fullPath, relPath string) error {
	ext := filepath.Ext(fullPath)
	parser, ok := idx.parsers[ext]
	if !ok {
		return nil // No parser for this extension; skip silently
	}

	symbols, imports, err := parser.Parse(fullPath)
	if err != nil {
		return nil // Parse errors are non-fatal; skip the file
	}

	// Store symbols.
	for _, sym := range symbols {
		sym.FilePath = relPath
		if err := idx.insertSymbol(&sym); err != nil {
			return err
		}
	}

	// Store imports.
	for _, imp := range imports {
		imp.FilePath = relPath
		if err := idx.insertImport(&imp); err != nil {
			return err
		}
	}

	return nil
}

// insertSymbol stores a symbol in the index.
func (idx *Indexer) insertSymbol(sym *Symbol) error {
	result, err := idx.db.Exec(
		`INSERT INTO idx_symbols (name, kind, file_path, line, end_line, signature, return_type, exported, parent_name, package_path)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sym.Name, string(sym.Kind), sym.FilePath, sym.Line, sym.EndLine,
		sym.Signature, sym.ReturnType, sym.Exported, sym.ParentName, sym.PackagePath,
	)
	if err != nil {
		return fmt.Errorf("insert symbol %s: %w", sym.Name, err)
	}

	// Store fields for types/structs.
	if len(sym.Fields) > 0 {
		symID, _ := result.LastInsertId()
		for _, field := range sym.Fields {
			_, err := idx.db.Exec(
				`INSERT INTO idx_fields (symbol_id, name, type_name, exported) VALUES (?, ?, ?, ?)`,
				symID, field.Name, field.TypeName, field.Exported,
			)
			if err != nil {
				return fmt.Errorf("insert field %s.%s: %w", sym.Name, field.Name, err)
			}
		}
	}
	return nil
}

// insertImport stores an import in the index.
func (idx *Indexer) insertImport(imp *Import) error {
	_, err := idx.db.Exec(
		`INSERT INTO idx_imports (file_path, import_path, alias) VALUES (?, ?, ?)`,
		imp.FilePath, imp.ImportPath, imp.Alias,
	)
	if err != nil {
		return fmt.Errorf("insert import: %w", err)
	}
	return nil
}

// clearIndex removes all entries from the index tables.
func (idx *Indexer) clearIndex() error {
	tables := []string{"idx_fields", "idx_symbols", "idx_imports", "idx_dependencies"}
	for _, table := range tables {
		if _, err := idx.db.Exec("DELETE FROM " + table); err != nil {
			return fmt.Errorf("clear %s: %w", table, err)
		}
	}
	return nil
}

// removeFileEntries removes all index entries for a specific file.
func (idx *Indexer) removeFileEntries(filePath string) error {
	// Remove fields for symbols in this file.
	_, err := idx.db.Exec(
		`DELETE FROM idx_fields WHERE symbol_id IN (SELECT id FROM idx_symbols WHERE file_path = ?)`,
		filePath,
	)
	if err != nil {
		return err
	}
	// Remove symbols.
	if _, err := idx.db.Exec("DELETE FROM idx_symbols WHERE file_path = ?", filePath); err != nil {
		return err
	}
	// Remove imports.
	if _, err := idx.db.Exec("DELETE FROM idx_imports WHERE file_path = ?", filePath); err != nil {
		return err
	}
	return nil
}

// SymbolCount returns the total number of indexed symbols.
func (idx *Indexer) SymbolCount() (int, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(*) FROM idx_symbols").Scan(&count)
	return count, err
}

// FileCount returns the number of distinct files in the index.
func (idx *Indexer) FileCount() (int, error) {
	var count int
	err := idx.db.QueryRow("SELECT COUNT(DISTINCT file_path) FROM idx_symbols").Scan(&count)
	return count, err
}

// languageForExt returns a human-readable language name for a file extension.
func languageForExt(ext string) string {
	switch ext {
	case ".go":
		return "go"
	case ".js", ".jsx", ".ts", ".tsx":
		return "javascript"
	case ".py":
		return "python"
	case ".rs":
		return "rust"
	default:
		return ""
	}
}

// isExported checks if a Go identifier is exported (starts with uppercase).
func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	return strings.ToUpper(name[:1]) == name[:1]
}
