package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setupTestGenerator(t *testing.T) (*Generator, string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "axiom-skill-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	return NewGenerator(tmpDir), tmpDir
}

func testData() *TemplateData {
	return &TemplateData{
		ProjectName:  "My Project",
		ProjectSlug:  "my-project",
		BudgetUSD:    10.0,
		MaxMeeseeks:  10,
		APIPort:      3000,
		BitNetEnabled: true,
		BitNetPort:   3002,
		DockerImage:  "axiom-meeseeks-multi:latest",
		BranchPrefix: "axiom",
		ModelTiers:   "local, cheap, standard, premium",
	}
}

func TestGenerateAllRuntimes(t *testing.T) {
	gen, tmpDir := setupTestGenerator(t)

	for _, rt := range SupportedRuntimes() {
		path, err := gen.Generate(rt, testData())
		if err != nil {
			t.Fatalf("generate %s: %v", rt, err)
		}

		// Verify file was created.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("%s: output file not created at %s", rt, path)
		}

		// Verify output path matches expected.
		expectedPath := filepath.Join(tmpDir, OutputPaths[rt])
		if path != expectedPath {
			t.Errorf("%s: path = %s, want %s", rt, path, expectedPath)
		}
	}
}

func TestGenerateContainsAll13Topics(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	for _, rt := range SupportedRuntimes() {
		path, err := gen.Generate(rt, testData())
		if err != nil {
			t.Fatalf("generate %s: %v", rt, err)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", rt, err)
		}

		missing := VerifyContent(string(content))
		if len(missing) > 0 {
			t.Errorf("%s: missing required topics: %v", rt, missing)
		}
	}
}

func TestGenerateClawContainsAPIPort(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	path, _ := gen.Generate(RuntimeClaw, testData())
	content, _ := os.ReadFile(path)

	if !strings.Contains(string(content), "3000") {
		t.Error("Claw skill should contain API port")
	}
	if !strings.Contains(string(content), "REST API") {
		t.Error("Claw skill should reference REST API")
	}
}

func TestGenerateClaudeCodeContainsWarning(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	path, _ := gen.Generate(RuntimeClaudeCode, testData())
	content, _ := os.ReadFile(path)

	if !strings.Contains(string(content), "Do NOT execute code directly") {
		t.Error("Claude Code skill should contain execution warning")
	}
}

func TestGenerateInjectsDynamicContent(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	data := testData()
	data.ProjectName = "Super API"
	data.BudgetUSD = 25.50

	path, _ := gen.Generate(RuntimeClaw, data)
	content, _ := os.ReadFile(path)

	if !strings.Contains(string(content), "Super API") {
		t.Error("should contain project name")
	}
	if !strings.Contains(string(content), "25.50") {
		t.Error("should contain budget amount")
	}
}

func TestGenerateUnsupportedRuntime(t *testing.T) {
	gen, _ := setupTestGenerator(t)

	_, err := gen.Generate("invalid-runtime", testData())
	if err == nil {
		t.Error("expected error for unsupported runtime")
	}
}

func TestIsValidRuntime(t *testing.T) {
	valid := []string{"claw", "claude-code", "codex", "opencode"}
	for _, rt := range valid {
		if !IsValidRuntime(rt) {
			t.Errorf("%s should be valid", rt)
		}
	}
	invalid := []string{"invalid", "cursor", "", "CLAW"}
	for _, rt := range invalid {
		if IsValidRuntime(rt) {
			t.Errorf("%s should be invalid", rt)
		}
	}
}

func TestContentTopicsCount(t *testing.T) {
	topics := ContentTopics()
	if len(topics) != 13 {
		t.Errorf("expected 13 topics, got %d", len(topics))
	}
}

func TestVerifyContentDetectsMissing(t *testing.T) {
	// Incomplete content missing some topics.
	content := "## Axiom Workflow\n## Budget Management\n"
	missing := VerifyContent(content)
	if len(missing) != 11 { // 13 - 2 present = 11 missing
		t.Errorf("expected 11 missing topics, got %d", len(missing))
	}
}

func TestOutputPathsComplete(t *testing.T) {
	for _, rt := range SupportedRuntimes() {
		if _, ok := OutputPaths[rt]; !ok {
			t.Errorf("missing output path for %s", rt)
		}
	}
}

func TestGenerateCreatesParentDirs(t *testing.T) {
	gen, tmpDir := setupTestGenerator(t)

	// Claude Code outputs to .claude/CLAUDE.md which needs a subdirectory.
	_, err := gen.Generate(RuntimeClaudeCode, testData())
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	claudeDir := filepath.Join(tmpDir, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		t.Error(".claude directory should be created")
	}
}
