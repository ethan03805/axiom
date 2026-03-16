package index

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func setupTestIndexer(t *testing.T) (*Indexer, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-index-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	db, err := sql.Open("sqlite", filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.Exec("PRAGMA foreign_keys=ON")
	t.Cleanup(func() { db.Close() })

	idx := NewIndexer(db)
	if err := idx.InitSchema(); err != nil {
		t.Fatalf("init schema: %v", err)
	}

	// Register Go parser.
	idx.RegisterParser(NewGoParser())

	return idx, tmpDir
}

// writeGoFile creates a Go source file in the test directory.
func writeGoFile(t *testing.T, dir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(dir, relPath)
	os.MkdirAll(filepath.Dir(fullPath), 0755)
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", relPath, err)
	}
}

func TestGoParserExtractsFunction(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/handlers/auth.go", `package handlers

import "net/http"

// HandleAuth processes authentication requests.
func HandleAuth(w http.ResponseWriter, r *http.Request) error {
	return nil
}

func privateHelper() string {
	return "secret"
}
`)

	if err := idx.FullIndex(tmpDir); err != nil {
		t.Fatalf("full index: %v", err)
	}

	// Lookup the exported function.
	result, err := idx.LookupSymbol("HandleAuth", "function")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}

	sym := result.Results[0]
	if sym["name"] != "HandleAuth" {
		t.Errorf("name = %s", sym["name"])
	}
	if sym["exported"] != true {
		t.Errorf("expected exported=true")
	}
	sig := sym["signature"].(string)
	if sig == "" {
		t.Error("expected non-empty signature")
	}

	// Lookup the private function.
	result, err = idx.LookupSymbol("privateHelper", "function")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result.Results))
	}
	if result.Results[0]["exported"] != false {
		t.Error("expected exported=false for private function")
	}
}

func TestGoParserExtractsStruct(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/models/user.go", `package models

type User struct {
	ID    int
	Name  string
	Email string
	admin bool
}
`)

	idx.FullIndex(tmpDir)

	result, err := idx.LookupSymbol("User", "type")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 type, got %d", len(result.Results))
	}
	if result.Results[0]["kind"] != "type" {
		t.Errorf("kind = %s, want type", result.Results[0]["kind"])
	}
}

func TestGoParserExtractsInterface(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/store/store.go", `package store

type UserStore interface {
	Get(id int) (*User, error)
	Create(user *User) error
	Delete(id int) error
}
`)

	idx.FullIndex(tmpDir)

	result, err := idx.LookupSymbol("UserStore", "interface")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 interface, got %d", len(result.Results))
	}
	if result.Results[0]["kind"] != "interface" {
		t.Errorf("kind = %s", result.Results[0]["kind"])
	}
}

func TestGoParserExtractsConstants(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/config/config.go", `package config

const MaxRetries = 3
const DefaultTimeout = 30

var Version = "1.0.0"
`)

	idx.FullIndex(tmpDir)

	result, _ := idx.LookupSymbol("MaxRetries", "constant")
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 constant, got %d", len(result.Results))
	}

	result, _ = idx.LookupSymbol("Version", "variable")
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 variable, got %d", len(result.Results))
	}
}

func TestGoParserExtractsImports(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/main.go", `package main

import (
	"fmt"
	"net/http"
	mylog "log"
)

func main() {
	fmt.Println("hello")
}
`)

	idx.FullIndex(tmpDir)

	// Check module graph shows imports.
	result, err := idx.ModuleGraph("")
	if err != nil {
		t.Fatalf("module graph: %v", err)
	}
	if len(result.Results) < 3 {
		t.Errorf("expected at least 3 import edges, got %d", len(result.Results))
	}
}

func TestListExports(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/api/api.go", `package api

func PublicHandler() {}
func AnotherPublic() {}
func privateFunc() {}

type Server struct{}
type config struct{}
`)

	idx.FullIndex(tmpDir)

	result, err := idx.ListExports("api")
	if err != nil {
		t.Fatalf("list exports: %v", err)
	}
	// Should find: PublicHandler, AnotherPublic, Server (3 exported symbols).
	if len(result.Results) != 3 {
		names := make([]interface{}, len(result.Results))
		for i, r := range result.Results {
			names[i] = r["name"]
		}
		t.Errorf("expected 3 exports, got %d: %v", len(result.Results), names)
	}
}

func TestFullAndIncrementalIndex(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/main.go", `package main

func Hello() {}
`)

	// Full index.
	if err := idx.FullIndex(tmpDir); err != nil {
		t.Fatalf("full index: %v", err)
	}

	count, _ := idx.SymbolCount()
	if count != 1 {
		t.Errorf("expected 1 symbol after full index, got %d", count)
	}

	// Add a new file.
	writeGoFile(t, tmpDir, "src/utils.go", `package main

func Helper() {}
func AnotherHelper() {}
`)

	// Incremental index.
	if err := idx.IncrementalIndex(tmpDir, []string{"src/utils.go"}); err != nil {
		t.Fatalf("incremental index: %v", err)
	}

	count, _ = idx.SymbolCount()
	if count != 3 {
		t.Errorf("expected 3 symbols after incremental, got %d", count)
	}

	// Modify existing file (should replace old entries).
	writeGoFile(t, tmpDir, "src/main.go", `package main

func Hello() {}
func Goodbye() {}
`)

	if err := idx.IncrementalIndex(tmpDir, []string{"src/main.go"}); err != nil {
		t.Fatalf("incremental index: %v", err)
	}

	count, _ = idx.SymbolCount()
	if count != 4 {
		t.Errorf("expected 4 symbols after modification, got %d", count)
	}
}

func TestAxiomDirExcluded(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	// Create a Go file inside .axiom/ that should be excluded.
	writeGoFile(t, tmpDir, ".axiom/internal.go", `package axiom
func InternalFunc() {}
`)
	writeGoFile(t, tmpDir, "src/main.go", `package main
func Main() {}
`)

	idx.FullIndex(tmpDir)

	count, _ := idx.SymbolCount()
	if count != 1 {
		t.Errorf("expected 1 symbol (.axiom excluded), got %d", count)
	}

	// Verify the .axiom file is not indexed.
	result, _ := idx.LookupSymbol("InternalFunc", "")
	if len(result.Results) != 0 {
		t.Error("InternalFunc from .axiom/ should not be indexed")
	}
}

func TestExecuteQuery(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/main.go", `package main

func Hello() string { return "hello" }
`)

	idx.FullIndex(tmpDir)

	// Test via ExecuteQuery dispatcher.
	result, err := idx.ExecuteQuery(&QueryRequest{
		QueryType: QueryLookupSymbol,
		Params:    QueryParams{Name: "Hello", Type: "function"},
	})
	if err != nil {
		t.Fatalf("execute query: %v", err)
	}
	if len(result.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(result.Results))
	}
}

func TestExecuteQueryUnknownType(t *testing.T) {
	idx, _ := setupTestIndexer(t)

	_, err := idx.ExecuteQuery(&QueryRequest{
		QueryType: "invalid_query",
	})
	if err == nil {
		t.Error("expected error for unknown query type")
	}
}

func TestIncrementalIndexDeletedFile(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/temp.go", `package main
func TempFunc() {}
`)

	idx.FullIndex(tmpDir)
	count, _ := idx.SymbolCount()
	if count != 1 {
		t.Fatalf("expected 1 symbol, got %d", count)
	}

	// Delete the file.
	os.Remove(filepath.Join(tmpDir, "src/temp.go"))

	// Incremental should remove entries for deleted file.
	idx.IncrementalIndex(tmpDir, []string{"src/temp.go"})

	count, _ = idx.SymbolCount()
	if count != 0 {
		t.Errorf("expected 0 symbols after deletion, got %d", count)
	}
}

func TestMethodExtraction(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/server.go", `package main

type Server struct {
	Port int
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {
}
`)

	idx.FullIndex(tmpDir)

	// Should have: Server (type), Start (method), Stop (method).
	count, _ := idx.SymbolCount()
	if count != 3 {
		t.Errorf("expected 3 symbols, got %d", count)
	}

	// Start should have parent_name = Server.
	result, _ := idx.LookupSymbol("Start", "function")
	if len(result.Results) != 1 {
		t.Fatalf("expected 1 result for Start, got %d", len(result.Results))
	}
}
