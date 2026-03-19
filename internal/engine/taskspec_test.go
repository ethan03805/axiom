package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethan03805/axiom/internal/security"
	"github.com/ethan03805/axiom/internal/state"
)

func setupTaskSpecTest(t *testing.T) (*TaskSpecBuilder, string) {
	t.Helper()

	tmpDir := t.TempDir()
	axiomDir := filepath.Join(tmpDir, ".axiom")
	os.MkdirAll(axiomDir, 0755)

	db, err := state.NewDB(filepath.Join(axiomDir, "axiom.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.RunMigrations()
	t.Cleanup(func() { db.Close() })

	scanner := security.NewSecretScanner(nil)
	builder := NewTaskSpecBuilder(db, scanner, tmpDir)

	return builder, tmpDir
}

func TestBuildTaskSpec(t *testing.T) {
	builder, _ := setupTaskSpecTest(t)

	req := &TaskSpecRequest{
		Task: &state.Task{
			ID:          "task-001",
			Title:       "Implement user authentication handler",
			Description: "Create the auth handler with JWT validation.",
			Tier:        "standard",
			TaskType:    "implementation",
		},
		ContextTier:    TierFile,
		SRSRefs:        []string{"FR-001", "AC-005"},
		TargetFiles:    []string{"src/handlers/auth.go"},
		AcceptCriteria: []string{"JWT tokens are validated", "Invalid tokens return 401"},
		Constraints: TaskConstraints{
			Language: "Go 1.22",
			Style:    "Standard Go conventions",
		},
	}

	spec, err := builder.Build(req, "abc123def")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// Verify TaskSpec structure matches Architecture Section 10.3.
	if !strings.Contains(spec, "# TaskSpec: task-001") {
		t.Error("missing TaskSpec header")
	}
	if !strings.Contains(spec, "git_sha: abc123def") {
		t.Error("missing base snapshot")
	}
	if !strings.Contains(spec, "## Objective") {
		t.Error("missing Objective section")
	}
	if !strings.Contains(spec, "Implement user authentication handler") {
		t.Error("missing task title in objective")
	}
	if !strings.Contains(spec, "## Context") {
		t.Error("missing Context section")
	}
	if !strings.Contains(spec, "## Interface Contract") {
		t.Error("missing Interface Contract section")
	}
	if !strings.Contains(spec, "src/handlers/auth.go") {
		t.Error("missing target file in interface contract")
	}
	if !strings.Contains(spec, "## Constraints") {
		t.Error("missing Constraints section")
	}
	if !strings.Contains(spec, "Go 1.22") {
		t.Error("missing language constraint")
	}
	if !strings.Contains(spec, "## SRS References") {
		t.Error("missing SRS References section")
	}
	if !strings.Contains(spec, "FR-001") {
		t.Error("missing SRS ref")
	}
	if !strings.Contains(spec, "## Acceptance Criteria") {
		t.Error("missing Acceptance Criteria section")
	}
	if !strings.Contains(spec, "JWT tokens are validated") {
		t.Error("missing acceptance criterion")
	}
	if !strings.Contains(spec, "## Output Format") {
		t.Error("missing Output Format section")
	}
	if !strings.Contains(spec, "manifest.json") {
		t.Error("missing manifest.json instruction")
	}
}

func TestBuildTaskSpecWithFeedback(t *testing.T) {
	builder, _ := setupTaskSpecTest(t)

	req := &TaskSpecRequest{
		Task: &state.Task{
			ID:    "task-002",
			Title: "Fix auth handler",
			Tier:  "standard",
			TaskType: "implementation",
		},
		ContextTier:   TierSymbol,
		TargetFiles:   []string{"src/handlers/auth.go"},
		Feedback:      "Compilation failed: undefined variable 'tokenKey' at line 42",
		AttemptNumber: 2,
	}

	spec, err := builder.Build(req, "def456ghi")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	if !strings.Contains(spec, "## Prior Attempt Feedback") {
		t.Error("missing feedback section")
	}
	if !strings.Contains(spec, "Attempt 1 failed") {
		t.Error("missing attempt number in feedback")
	}
	if !strings.Contains(spec, "tokenKey") {
		t.Error("missing feedback content")
	}
}

func TestBuildTaskSpecFileContextWithSecurity(t *testing.T) {
	builder, tmpDir := setupTaskSpecTest(t)

	// Create a file with a secret.
	srcDir := filepath.Join(tmpDir, "src")
	os.MkdirAll(srcDir, 0755)
	os.WriteFile(filepath.Join(srcDir, "config.go"), []byte(`package src
const APIKey = "sk-1234567890abcdef1234567890abcdef"
func GetConfig() {}
`), 0644)

	req := &TaskSpecRequest{
		Task: &state.Task{
			ID:    "task-003",
			Title: "Update config",
			Tier:  "standard",
			TaskType: "implementation",
		},
		ContextTier: TierFile,
		TargetFiles: []string{"src/config.go"},
	}

	spec, err := builder.Build(req, "ghi789")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// The secret should be redacted.
	if strings.Contains(spec, "sk-1234567890abcdef") {
		t.Error("secret was not redacted in TaskSpec")
	}
	// The file content should be wrapped in untrusted content tags.
	if !strings.Contains(spec, "untrusted_repo_content") {
		t.Error("file content not wrapped in untrusted tags")
	}
}

func TestBuildTaskSpecExcludedPath(t *testing.T) {
	builder, tmpDir := setupTaskSpecTest(t)

	// Create a .env file (should be excluded from prompts).
	os.WriteFile(filepath.Join(tmpDir, ".env"), []byte("SECRET=value"), 0644)

	req := &TaskSpecRequest{
		Task: &state.Task{
			ID:    "task-004",
			Title: "Update env",
			Tier:  "standard",
			TaskType: "implementation",
		},
		ContextTier: TierFile,
		TargetFiles: []string{".env"},
	}

	spec, err := builder.Build(req, "jkl012")
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	// .env should not appear in the context (excluded by security pipeline).
	if strings.Contains(spec, "SECRET=value") {
		t.Error(".env content should be excluded from TaskSpec")
	}
}

func TestContextTierLabels(t *testing.T) {
	builder, _ := setupTaskSpecTest(t)

	tiers := []struct {
		tier     ContextTier
		expected string
	}{
		{TierSymbol, "tier: symbol"},
		{TierFile, "tier: file"},
		{TierPackage, "tier: package"},
		{TierRepoMap, "tier: repo-map"},
	}

	for _, tt := range tiers {
		req := &TaskSpecRequest{
			Task: &state.Task{
				ID:    "task-tier",
				Title: "Test tier " + string(tt.tier),
				Tier:  "standard",
				TaskType: "implementation",
			},
			ContextTier: tt.tier,
		}

		spec, _ := builder.Build(req, "abc")
		if !strings.Contains(spec, tt.expected) {
			t.Errorf("tier %s: expected context header containing %q", tt.tier, tt.expected)
		}
	}
}
