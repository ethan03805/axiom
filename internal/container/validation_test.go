package container

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

// writeValidationResult writes a mock validation result JSON file to the given
// directory. When this directory is the project root, the overlay copy will
// pick it up as part of the base layer, simulating the container having written
// results to the mounted working directory.
func writeValidationResult(t *testing.T, dir string, result validationResultJSON) {
	t.Helper()
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal validation result: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, validationResultFile), data, 0644); err != nil {
		t.Fatalf("write validation result: %v", err)
	}
}

func setupTestValidation(t *testing.T) (*ValidationSandbox, *mockDockerClient) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-validation-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatalf("create db: %v", err)
	}
	if err := db.RunMigrations(); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	mock := newMockDocker()
	emitter := events.NewEmitter()

	config := ValidationConfig{
		Image:           "axiom-meeseeks-go:latest",
		CPULimit:        1.0,
		MemoryLimit:     "4g",
		TimeoutMinutes:  10,
		Network:         "none",
		SecurityScan:    false,
		AllowDepInstall: true,
	}

	vs := NewValidationSandbox(mock, db, emitter, config, tmpDir)

	// Create task for FK constraint.
	db.CreateTask(&state.Task{
		ID: "task-val", Title: "Validation Test", Status: "in_progress",
		Tier: "standard", TaskType: "implementation",
	})

	return vs, mock
}

func TestValidationSandboxSpawnsContainer(t *testing.T) {
	vs, mock := setupTestValidation(t)
	ctx := context.Background()

	// Write a passing validation result in the project root so the overlay
	// copy picks it up. The mock docker client returns an empty container
	// list, so isContainerRunning returns false immediately (simulating the
	// container having already exited).
	writeValidationResult(t, vs.projectRoot, validationResultJSON{
		CompilePass: true,
		LintPass:    true,
		TestPass:    true,
		TestCount:   5,
		TestPassed:  5,
	})

	// Create a staging directory with some output.
	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)
	os.WriteFile(filepath.Join(stagingDir, "main.go"), []byte("package main"), 0644)

	result, err := vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Verify a container was created.
	if len(mock.created) != 1 {
		t.Fatalf("expected 1 container created, got %d", len(mock.created))
	}

	// Verify hardening flags.
	hc := mock.created[0].HostConfig
	if !hc.ReadonlyRootfs {
		t.Error("expected read-only rootfs")
	}
	if len(hc.CapDrop) == 0 || hc.CapDrop[0] != "ALL" {
		t.Error("expected cap-drop ALL")
	}
	if hc.NetworkMode != "none" {
		t.Errorf("expected network=none, got %s", hc.NetworkMode)
	}

	// Verify resource limits match validation config (higher than Meeseeks).
	expectedCPU := int64(1.0 * 1e9)
	if hc.NanoCPUs != expectedCPU {
		t.Errorf("expected NanoCPUs=%d, got %d", expectedCPU, hc.NanoCPUs)
	}
	expectedMem := ParseMemoryBytes("4g")
	if hc.Memory != expectedMem {
		t.Errorf("expected Memory=%d, got %d", expectedMem, hc.Memory)
	}

	// Verify labels.
	cfg := mock.created[0].Config
	if cfg.Labels["axiom.container-type"] != "validator" {
		t.Errorf("expected container-type=validator, got %s", cfg.Labels["axiom.container-type"])
	}

	// Verify container was cleaned up.
	if len(mock.removed) != 1 {
		t.Errorf("expected 1 container removed, got %d", len(mock.removed))
	}

	// Verify result contains the values from the result file.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if !result.CompilePass {
		t.Error("expected CompilePass=true")
	}
	if !result.LintPass {
		t.Error("expected LintPass=true")
	}
	if !result.TestPass {
		t.Error("expected TestPass=true")
	}
	if result.TestCount != 5 {
		t.Errorf("expected TestCount=5, got %d", result.TestCount)
	}
	if result.TestPassed != 5 {
		t.Errorf("expected TestPassed=5, got %d", result.TestPassed)
	}
}

func TestValidationSandboxRecordsSession(t *testing.T) {
	vs, _ := setupTestValidation(t)
	ctx := context.Background()

	// Write a result file so the validation completes successfully.
	writeValidationResult(t, vs.projectRoot, validationResultJSON{
		CompilePass: true,
		LintPass:    true,
		TestPass:    true,
	})

	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)

	vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)

	// Verify session was recorded and stopped.
	sessions, err := vs.db.ListContainerSessionsByTask("task-val")
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ContainerType != "validator" {
		t.Errorf("expected validator, got %s", sessions[0].ContainerType)
	}
	if sessions[0].ExitReason != "completed" {
		t.Errorf("expected completed, got %s", sessions[0].ExitReason)
	}
}

func TestValidationSandboxUsesCorrectImage(t *testing.T) {
	vs, mock := setupTestValidation(t)
	ctx := context.Background()

	writeValidationResult(t, vs.projectRoot, validationResultJSON{
		CompilePass: true,
		LintPass:    true,
		TestPass:    true,
	})

	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)

	vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)

	// Image should match the configured validation image (same family as Meeseeks).
	if mock.created[0].Config.Image != "axiom-meeseeks-go:latest" {
		t.Errorf("expected axiom-meeseeks-go:latest, got %s", mock.created[0].Config.Image)
	}
}

func TestValidationSandboxNoResultFile(t *testing.T) {
	vs, _ := setupTestValidation(t)
	ctx := context.Background()

	// Do NOT write a result file -- simulates container crash.
	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)

	result, err := vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)

	// Should return an error indicating the container did not write results.
	if err == nil {
		t.Fatal("expected error when result file is missing")
	}

	// Should still return a non-nil result with failing checks.
	if result == nil {
		t.Fatal("expected non-nil result even on error")
	}
	if result.CompilePass {
		t.Error("expected CompilePass=false for missing result file")
	}
	if result.LintPass {
		t.Error("expected LintPass=false for missing result file")
	}
	if result.TestPass {
		t.Error("expected TestPass=false for missing result file")
	}
}

func TestValidationSandboxFailingResult(t *testing.T) {
	vs, _ := setupTestValidation(t)
	ctx := context.Background()

	// Write a failing validation result.
	writeValidationResult(t, vs.projectRoot, validationResultJSON{
		CompilePass:  true,
		LintPass:     false,
		LintError:    "src/main.go:10: unused variable 'x'",
		LintWarnings: []string{"line 5: long line"},
		TestPass:     false,
		TestError:    "TestFoo: expected 42, got 0",
		TestCount:    3,
		TestPassed:   1,
	})

	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)

	result, err := vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !result.CompilePass {
		t.Error("expected CompilePass=true")
	}
	if result.LintPass {
		t.Error("expected LintPass=false")
	}
	if result.LintError != "src/main.go:10: unused variable 'x'" {
		t.Errorf("unexpected LintError: %s", result.LintError)
	}
	if len(result.LintWarnings) != 1 {
		t.Errorf("expected 1 lint warning, got %d", len(result.LintWarnings))
	}
	if result.TestPass {
		t.Error("expected TestPass=false")
	}
	if result.TestCount != 3 {
		t.Errorf("expected TestCount=3, got %d", result.TestCount)
	}
	if result.TestPassed != 1 {
		t.Errorf("expected TestPassed=1, got %d", result.TestPassed)
	}
}

func TestOverlayCreation(t *testing.T) {
	vs, _ := setupTestValidation(t)

	// Create project files in the project root.
	os.WriteFile(filepath.Join(vs.projectRoot, "main.go"), []byte("package main\nfunc main() {}"), 0644)
	os.MkdirAll(filepath.Join(vs.projectRoot, "pkg"), 0755)
	os.WriteFile(filepath.Join(vs.projectRoot, "pkg", "lib.go"), []byte("package pkg"), 0644)

	// Create directories that should be skipped.
	os.MkdirAll(filepath.Join(vs.projectRoot, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(vs.projectRoot, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)
	os.MkdirAll(filepath.Join(vs.projectRoot, "node_modules", "some-pkg"), 0755)
	os.WriteFile(filepath.Join(vs.projectRoot, "node_modules", "some-pkg", "index.js"), []byte("module.exports = {}"), 0644)
	// .axiom/ is already created by setupTestValidation.

	// Create staging directory with an override and a new file.
	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)
	os.WriteFile(filepath.Join(stagingDir, "main.go"), []byte("package main\nfunc main() { fmt.Println(\"hello\") }"), 0644)
	os.MkdirAll(filepath.Join(stagingDir, "internal"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "internal", "handler.go"), []byte("package internal"), 0644)

	// Create overlay.
	workDir, err := vs.createOverlay("task-overlay", stagingDir, vs.projectRoot)
	if err != nil {
		t.Fatalf("createOverlay: %v", err)
	}
	defer os.RemoveAll(workDir)

	// Verify project files were copied (base layer).
	if _, err := os.Stat(filepath.Join(workDir, "pkg", "lib.go")); os.IsNotExist(err) {
		t.Error("expected pkg/lib.go to be copied from project")
	}

	// Verify skipped directories are NOT in the overlay.
	if _, err := os.Stat(filepath.Join(workDir, ".git")); !os.IsNotExist(err) {
		t.Error("expected .git/ to be skipped in overlay")
	}
	if _, err := os.Stat(filepath.Join(workDir, ".axiom")); !os.IsNotExist(err) {
		t.Error("expected .axiom/ to be skipped in overlay")
	}
	if _, err := os.Stat(filepath.Join(workDir, "node_modules")); !os.IsNotExist(err) {
		t.Error("expected node_modules/ to be skipped in overlay")
	}

	// Verify staging files override project files (overlay layer).
	mainContent, err := os.ReadFile(filepath.Join(workDir, "main.go"))
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	if string(mainContent) != "package main\nfunc main() { fmt.Println(\"hello\") }" {
		t.Errorf("main.go should be the staging version, got: %s", string(mainContent))
	}

	// Verify new staging files are present.
	if _, err := os.Stat(filepath.Join(workDir, "internal", "handler.go")); os.IsNotExist(err) {
		t.Error("expected internal/handler.go from staging to be in overlay")
	}
}

func TestOverlayEmptyStagingDir(t *testing.T) {
	vs, _ := setupTestValidation(t)

	os.WriteFile(filepath.Join(vs.projectRoot, "README.md"), []byte("# Hello"), 0644)

	stagingDir, _ := os.MkdirTemp("", "axiom-staging-empty-*")
	defer os.RemoveAll(stagingDir)

	workDir, err := vs.createOverlay("task-empty", stagingDir, vs.projectRoot)
	if err != nil {
		t.Fatalf("createOverlay: %v", err)
	}
	defer os.RemoveAll(workDir)

	// Project file should still be there.
	if _, err := os.Stat(filepath.Join(workDir, "README.md")); os.IsNotExist(err) {
		t.Error("expected README.md from project in overlay")
	}
}

func TestOverlayMissingStagingDir(t *testing.T) {
	vs, _ := setupTestValidation(t)

	os.WriteFile(filepath.Join(vs.projectRoot, "go.mod"), []byte("module test"), 0644)

	// Use a nonexistent staging dir.
	workDir, err := vs.createOverlay("task-nostagingdir", "/nonexistent/staging", vs.projectRoot)
	if err != nil {
		t.Fatalf("createOverlay: %v", err)
	}
	defer os.RemoveAll(workDir)

	// Project file should still be there.
	if _, err := os.Stat(filepath.Join(workDir, "go.mod")); os.IsNotExist(err) {
		t.Error("expected go.mod from project in overlay")
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		image    string
		expected string
	}{
		{"axiom-meeseeks-go:latest", "go"},
		{"axiom-meeseeks-node:latest", "node"},
		{"axiom-meeseeks-python:latest", "python"},
		{"axiom-meeseeks-rust:latest", "rust"},
		{"axiom-meeseeks-multi:latest", "multi"},
		{"custom-image:v1", "multi"},
	}
	for _, tt := range tests {
		result := DetectLanguage(tt.image)
		if result != tt.expected {
			t.Errorf("DetectLanguage(%s) = %s, want %s", tt.image, result, tt.expected)
		}
	}
}

func TestLanguageProfileRegistry(t *testing.T) {
	// Verify all expected profiles exist.
	for _, lang := range []string{"go", "node", "python", "rust", "multi"} {
		p := ProfileRegistry[lang]
		if p == nil {
			t.Errorf("missing profile for language: %s", lang)
			continue
		}
		if p.Name != lang {
			t.Errorf("profile %s has name %s", lang, p.Name)
		}
	}

	// Verify Go profile specifics.
	goP := ProfileRegistry["go"]
	if goP.Image != "axiom-meeseeks-go:latest" {
		t.Errorf("Go profile image = %s", goP.Image)
	}
	if len(goP.CompileCmd) == 0 {
		t.Error("Go profile should have compile command")
	}
	if len(goP.LintCmd) == 0 {
		t.Error("Go profile should have lint command")
	}

	// Verify Node profile disables scripts.
	nodeP := ProfileRegistry["node"]
	found := false
	for _, arg := range nodeP.DependencyCmd {
		if arg == "--ignore-scripts" {
			found = true
		}
	}
	if !found {
		t.Error("Node profile should use --ignore-scripts")
	}
}

func TestWarmPool(t *testing.T) {
	wp := NewWarmPool(true, 3, 10)

	if !wp.Enabled() {
		t.Error("expected pool to be enabled")
	}
	if wp.Size() != 0 {
		t.Errorf("expected empty pool, got %d", wp.Size())
	}

	// Return some containers to the pool.
	wp.Return("container-1")
	wp.Return("container-2")
	if wp.Size() != 2 {
		t.Errorf("expected 2 in pool, got %d", wp.Size())
	}

	// Acquire from pool.
	id := wp.Acquire()
	if id != "container-1" {
		t.Errorf("expected container-1, got %s", id)
	}
	if wp.Size() != 1 {
		t.Errorf("expected 1 in pool, got %d", wp.Size())
	}

	// Acquire remaining.
	id = wp.Acquire()
	if id != "container-2" {
		t.Errorf("expected container-2, got %s", id)
	}

	// Pool empty, acquire returns "".
	id = wp.Acquire()
	if id != "" {
		t.Errorf("expected empty string for empty pool, got %s", id)
	}
}

func TestWarmPoolMaxSize(t *testing.T) {
	wp := NewWarmPool(true, 2, 10)

	wp.Return("c1")
	wp.Return("c2")
	wp.Return("c3") // Over capacity, should be ignored.

	if wp.Size() != 2 {
		t.Errorf("expected 2 (max size), got %d", wp.Size())
	}
}

func TestWarmPoolDisabled(t *testing.T) {
	wp := NewWarmPool(false, 3, 10)

	if wp.Enabled() {
		t.Error("expected pool to be disabled")
	}

	// Operations on disabled pool are no-ops.
	wp.Return("c1")
	if wp.Size() != 0 {
		t.Error("disabled pool should not accept containers")
	}

	id := wp.Acquire()
	if id != "" {
		t.Error("disabled pool should return empty string")
	}
}

func TestWarmPoolColdBuildInterval(t *testing.T) {
	wp := NewWarmPool(true, 3, 3)

	// Warm uses: acquire increments count.
	wp.Return("c1")
	wp.Return("c2")
	wp.Return("c3")

	wp.Acquire() // warmUsesCount = 1
	if wp.NeedsColdBuild() {
		t.Error("should not need cold build after 1 use")
	}
	wp.Acquire() // warmUsesCount = 2
	if wp.NeedsColdBuild() {
		t.Error("should not need cold build after 2 uses")
	}
	wp.Acquire() // warmUsesCount = 3
	if !wp.NeedsColdBuild() {
		t.Error("should need cold build after 3 uses (interval=3)")
	}
}

func TestWarmPoolDrain(t *testing.T) {
	wp := NewWarmPool(true, 5, 10)
	wp.Return("c1")
	wp.Return("c2")

	ids := wp.Drain()
	if len(ids) != 2 {
		t.Errorf("expected 2 drained, got %d", len(ids))
	}
	if wp.Size() != 0 {
		t.Error("pool should be empty after drain")
	}
}
