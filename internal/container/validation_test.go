package container

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/state"
)

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
		Image:          "axiom-meeseeks-go:latest",
		CPULimit:       1.0,
		MemoryLimit:    "4g",
		TimeoutMinutes: 10,
		Network:        "none",
		SecurityScan:   false,
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

	_ = mock // used for verification below

	// Create a staging directory with some output.
	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)
	os.WriteFile(filepath.Join(stagingDir, "main.go"), []byte("package main"), 0644)

	// The validate call will fail to find the result file since the mock
	// container doesn't actually run. This is expected -- we verify the
	// container was created with correct config. The error from missing
	// result file is a known test limitation when not using real Docker.
	result, err := vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)
	// Accept either success or the "result file not found" error.
	if err != nil && result == nil {
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

	// Verify result is returned.
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestValidationSandboxRecordsSession(t *testing.T) {
	vs, _ := setupTestValidation(t)
	ctx := context.Background()

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
	// Exit reason is "completed" when the result file is found, or "error"
	// when the mock container exits without writing it. Both are acceptable.
	if sessions[0].ExitReason != "completed" && sessions[0].ExitReason != "error" {
		t.Errorf("expected completed or error, got %s", sessions[0].ExitReason)
	}
}

func TestValidationSandboxUsesCorrectImage(t *testing.T) {
	vs, mock := setupTestValidation(t)
	ctx := context.Background()

	stagingDir, _ := os.MkdirTemp("", "axiom-staging-*")
	defer os.RemoveAll(stagingDir)

	vs.Validate(ctx, "task-val", stagingDir, vs.projectRoot)

	// Image should match the configured validation image (same family as Meeseeks).
	if mock.created[0].Config.Image != "axiom-meeseeks-go:latest" {
		t.Errorf("expected axiom-meeseeks-go:latest, got %s", mock.created[0].Config.Image)
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
