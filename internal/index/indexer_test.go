package index

import (
	"database/sql"
	"encoding/json"
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

func TestFindImplementations(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	// Define an interface with two methods.
	writeGoFile(t, tmpDir, "src/store/store.go", `package store

type Store interface {
	Get(id int) error
	Save(data string) error
}
`)

	// Define a type that implements both methods (should be found).
	writeGoFile(t, tmpDir, "src/store/memory.go", `package store

type MemoryStore struct {
	data map[int]string
}

func (m *MemoryStore) Get(id int) error {
	return nil
}

func (m *MemoryStore) Save(data string) error {
	return nil
}
`)

	// Define a type that implements only one method (should NOT be found).
	writeGoFile(t, tmpDir, "src/store/partial.go", `package store

type PartialStore struct{}

func (p *PartialStore) Get(id int) error {
	return nil
}
`)

	// Define a type that implements both methods plus extra (should be found).
	writeGoFile(t, tmpDir, "src/store/extended.go", `package store

type ExtendedStore struct{}

func (e *ExtendedStore) Get(id int) error {
	return nil
}

func (e *ExtendedStore) Save(data string) error {
	return nil
}

func (e *ExtendedStore) Delete(id int) error {
	return nil
}
`)

	if err := idx.FullIndex(tmpDir); err != nil {
		t.Fatalf("full index: %v", err)
	}

	result, err := idx.FindImplementations("Store")
	if err != nil {
		t.Fatalf("find implementations: %v", err)
	}

	// Should find MemoryStore and ExtendedStore, but NOT PartialStore.
	if len(result.Results) != 2 {
		names := make([]interface{}, len(result.Results))
		for i, r := range result.Results {
			names[i] = r["type"]
		}
		t.Fatalf("expected 2 implementations, got %d: %v", len(result.Results), names)
	}

	found := map[string]bool{}
	for _, r := range result.Results {
		found[r["type"].(string)] = true
	}
	if !found["MemoryStore"] {
		t.Error("expected MemoryStore to implement Store")
	}
	if !found["ExtendedStore"] {
		t.Error("expected ExtendedStore to implement Store")
	}
	if found["PartialStore"] {
		t.Error("PartialStore should NOT implement Store (missing Save method)")
	}
}

func TestReverseDependencies(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	// Package "auth" defines HandleAuth.
	writeGoFile(t, tmpDir, "src/auth/auth.go", `package auth

func HandleAuth() error {
	return nil
}
`)

	// Package "api" imports "auth" (by package name match).
	writeGoFile(t, tmpDir, "src/api/routes.go", `package api

import "project/auth"

func RegisterRoutes() {
	_ = auth.HandleAuth()
}
`)

	// Package "main" does NOT import "auth".
	writeGoFile(t, tmpDir, "src/main.go", `package main

func main() {}
`)

	if err := idx.FullIndex(tmpDir); err != nil {
		t.Fatalf("full index: %v", err)
	}

	result, err := idx.ReverseDependencies("HandleAuth")
	if err != nil {
		t.Fatalf("reverse dependencies: %v", err)
	}

	// Should find files that import "auth" (the package containing HandleAuth).
	// The api/routes.go file imports "project/auth" which contains "auth" substring.
	if len(result.Results) == 0 {
		t.Fatal("expected at least 1 reverse dependency, got 0")
	}

	foundAPIFile := false
	for _, r := range result.Results {
		file, ok := r["file"].(string)
		if ok && file == "src/api/routes.go" {
			foundAPIFile = true
		}
	}
	if !foundAPIFile {
		t.Error("expected src/api/routes.go to appear as reverse dependency of HandleAuth")
	}
}

func TestHandleQueryIndex(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/main.go", `package main

func Greet(name string) string {
	return "Hello, " + name
}
`)

	idx.FullIndex(tmpDir)

	// Simulate a raw JSON IPC message for query_index.
	rawJSON, _ := json.Marshal(QueryRequest{
		QueryType: QueryLookupSymbol,
		Params:    QueryParams{Name: "Greet", Type: "function"},
	})

	resp, err := idx.HandleQueryIndex("task-001", nil, rawJSON)
	if err != nil {
		t.Fatalf("HandleQueryIndex: %v", err)
	}

	qr, ok := resp.(*QueryResult)
	if !ok {
		t.Fatalf("expected *QueryResult, got %T", resp)
	}
	if len(qr.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(qr.Results))
	}
	if qr.Results[0]["name"] != "Greet" {
		t.Errorf("name = %v, want Greet", qr.Results[0]["name"])
	}
}

func TestHandleQueryIndexInvalidJSON(t *testing.T) {
	idx, _ := setupTestIndexer(t)

	_, err := idx.HandleQueryIndex("task-001", nil, []byte(`{invalid json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestVendorAndNodeModulesExcluded(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	// Files inside vendor/ and node_modules/ should be skipped.
	writeGoFile(t, tmpDir, "vendor/lib/lib.go", `package lib
func VendorFunc() {}
`)
	writeGoFile(t, tmpDir, "node_modules/pkg/pkg.go", `package pkg
func NodeFunc() {}
`)
	writeGoFile(t, tmpDir, ".git/hooks/pre-commit.go", `package hooks
func GitHookFunc() {}
`)
	writeGoFile(t, tmpDir, "src/app.go", `package main
func App() {}
`)

	if err := idx.FullIndex(tmpDir); err != nil {
		t.Fatalf("full index: %v", err)
	}

	count, _ := idx.SymbolCount()
	if count != 1 {
		t.Errorf("expected 1 symbol (only App), got %d", count)
	}

	// Verify excluded symbols are not present.
	for _, name := range []string{"VendorFunc", "NodeFunc", "GitHookFunc"} {
		result, _ := idx.LookupSymbol(name, "")
		if len(result.Results) != 0 {
			t.Errorf("%s should not be indexed (excluded directory)", name)
		}
	}
}

func TestModuleGraphFilteredByPackage(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/api/handler.go", `package api

import (
	"fmt"
	"net/http"
)

func Handle() {
	fmt.Println("handle")
	_ = http.StatusOK
}
`)

	writeGoFile(t, tmpDir, "src/cli/cmd.go", `package cli

import "os"

func Run() {
	_ = os.Args
}
`)

	if err := idx.FullIndex(tmpDir); err != nil {
		t.Fatalf("full index: %v", err)
	}

	// Full module graph should include imports from both packages.
	fullResult, err := idx.ModuleGraph("")
	if err != nil {
		t.Fatalf("module graph (full): %v", err)
	}
	if len(fullResult.Results) < 3 {
		t.Errorf("expected at least 3 import edges in full graph, got %d", len(fullResult.Results))
	}

	// Filtered module graph for "api" should only include api's imports.
	apiResult, err := idx.ModuleGraph("api")
	if err != nil {
		t.Fatalf("module graph (api): %v", err)
	}
	if len(apiResult.Results) != 2 {
		t.Errorf("expected 2 import edges for api (fmt, net/http), got %d", len(apiResult.Results))
	}
}

func TestLookupSymbolNoKindFilter(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/app.go", `package main

const AppName = "test"

func AppName2() {}

type AppConfig struct{}
`)

	idx.FullIndex(tmpDir)

	// Lookup without kind filter should return all matches.
	result, err := idx.LookupSymbol("AppName", "")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 1 {
		t.Errorf("expected 1 result for AppName (constant), got %d", len(result.Results))
	}

	// Lookup with kind filter should narrow results.
	result, err = idx.LookupSymbol("AppName", "constant")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 1 {
		t.Errorf("expected 1 constant, got %d", len(result.Results))
	}

	result, err = idx.LookupSymbol("AppName", "function")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(result.Results) != 0 {
		t.Errorf("expected 0 functions named AppName, got %d", len(result.Results))
	}
}

func TestFindImplementationsEmptyInterface(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	// An interface with no methods -- every type trivially implements it,
	// but with no required methods the query returns empty (no methods to match).
	writeGoFile(t, tmpDir, "src/types.go", `package main

type Empty interface{}

type Foo struct{}
`)

	idx.FullIndex(tmpDir)

	result, err := idx.FindImplementations("Empty")
	if err != nil {
		t.Fatalf("find implementations: %v", err)
	}
	// Empty interface has no methods, so requiredMethods is empty, returns empty result.
	if len(result.Results) != 0 {
		t.Errorf("expected 0 implementations for empty interface, got %d", len(result.Results))
	}
}

func TestFileCount(t *testing.T) {
	idx, tmpDir := setupTestIndexer(t)

	writeGoFile(t, tmpDir, "src/a.go", `package main
func A() {}
`)
	writeGoFile(t, tmpDir, "src/b.go", `package main
func B() {}
`)
	writeGoFile(t, tmpDir, "src/c.go", `package main
func C() {}
`)

	idx.FullIndex(tmpDir)

	count, err := idx.FileCount()
	if err != nil {
		t.Fatalf("file count: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3 files, got %d", count)
	}
}
