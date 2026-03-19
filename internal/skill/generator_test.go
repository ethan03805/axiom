package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testTemplateData returns a TemplateData populated with test values.
func testTemplateData() TemplateData {
	return TemplateData{
		ProjectName:       "test-project",
		ProjectSlug:       "test-project",
		BudgetUSD:         25.00,
		BudgetMax:         "25.00",
		MaxMeeseeks:       8,
		APIPort:           3000,
		BitNetEnabled:     true,
		BitNetPort:        3002,
		DockerImage:       "axiom-meeseeks-multi:latest",
		BranchPrefix:      "axiom",
		ModelTiers:        "Standard model tiers configured.",
		ModelTiersSummary: "Standard model tiers configured.",
		IPCEndpoint:       "filesystem IPC at /workspace/ipc/",
	}
}

// findRepoRoot walks upward from the current working directory to find the
// repository root (identified by the presence of a skills/ directory).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "skills")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root (no skills/ directory found)")
		}
		dir = parent
	}
}

func TestIsValidRuntime(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"claw", true},
		{"claude-code", true},
		{"codex", true},
		{"opencode", true},
		{"invalid", false},
		{"", false},
		{"CLAW", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := IsValidRuntime(tt.input)
			if got != tt.want {
				t.Errorf("IsValidRuntime(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestSupportedRuntimes(t *testing.T) {
	rts := SupportedRuntimes()
	if len(rts) != 4 {
		t.Fatalf("expected 4 supported runtimes, got %d", len(rts))
	}

	expected := map[Runtime]bool{
		RuntimeClaw: true, RuntimeClaudeCode: true,
		RuntimeCodex: true, RuntimeOpenCode: true,
	}
	for _, rt := range rts {
		if !expected[rt] {
			t.Errorf("unexpected runtime: %s", rt)
		}
	}
}

func TestLoadTemplatesFromDir(t *testing.T) {
	repoRoot := findRepoRoot(t)
	gen := NewGenerator(repoRoot)

	if err := gen.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates() error: %v", err)
	}

	if !gen.HasTemplates() {
		t.Fatal("HasTemplates() returned false after loading")
	}

	// Verify all four runtimes have templates.
	for _, rt := range SupportedRuntimes() {
		if _, ok := gen.templates[rt]; !ok {
			t.Errorf("missing template for runtime %s", rt)
		}
	}
}

func TestLoadTemplates_MissingDir(t *testing.T) {
	gen := NewGenerator("/nonexistent/path")
	err := gen.LoadTemplates()
	if err == nil {
		t.Fatal("expected error for missing skills directory")
	}
	if !strings.Contains(err.Error(), "skills directory not found") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestGenerate_AllRuntimes(t *testing.T) {
	repoRoot := findRepoRoot(t)
	gen := NewGenerator(repoRoot)
	if err := gen.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	data := testTemplateData()

	for _, rt := range SupportedRuntimes() {
		t.Run(string(rt), func(t *testing.T) {
			content, err := gen.Generate(rt, data)
			if err != nil {
				t.Fatalf("Generate(%s) error: %v", rt, err)
			}
			if content == "" {
				t.Fatal("Generate returned empty content")
			}

			// Check that project-specific data was injected.
			if !strings.Contains(content, "test-project") {
				t.Error("rendered content does not contain project name")
			}
			if !strings.Contains(content, "axiom-meeseeks-multi:latest") {
				t.Error("rendered content does not contain docker image")
			}

			// Verify the 13 required content sections are present.
			missing := VerifyContent(content)
			if len(missing) > 0 {
				t.Errorf("missing content topics: %v", missing)
			}
		})
	}
}

func TestGenerate_UnsupportedRuntime(t *testing.T) {
	repoRoot := findRepoRoot(t)
	gen := NewGenerator(repoRoot)
	if err := gen.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	_, err := gen.Generate(Runtime("invalid"), testTemplateData())
	if err == nil {
		t.Fatal("expected error for unsupported runtime")
	}
	if !strings.Contains(err.Error(), "no template loaded") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerate_BudgetMaxAutoPopulated(t *testing.T) {
	repoRoot := findRepoRoot(t)
	gen := NewGenerator(repoRoot)
	if err := gen.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	data := testTemplateData()
	data.BudgetMax = "" // Leave empty -- should be auto-populated from BudgetUSD.

	content, err := gen.Generate(RuntimeClaw, data)
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if !strings.Contains(content, "25.00") {
		t.Error("auto-populated BudgetMax not found in rendered output")
	}
}

func TestWriteSkillFile_AllRuntimes(t *testing.T) {
	repoRoot := findRepoRoot(t)
	gen := NewGenerator(repoRoot)
	if err := gen.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	data := testTemplateData()

	// Write to a temporary directory.
	tmpDir := t.TempDir()

	for _, rt := range SupportedRuntimes() {
		t.Run(string(rt), func(t *testing.T) {
			outputPath, err := gen.WriteSkillFile(rt, data, tmpDir)
			if err != nil {
				t.Fatalf("WriteSkillFile(%s) error: %v", rt, err)
			}

			// Verify the output path matches expected.
			expectedRel := OutputPaths[rt]
			expectedFull := filepath.Join(tmpDir, expectedRel)
			if outputPath != expectedFull {
				t.Errorf("output path = %q, want %q", outputPath, expectedFull)
			}

			// Verify the file exists and has content.
			info, err := os.Stat(outputPath)
			if err != nil {
				t.Fatalf("stat output file: %v", err)
			}
			if info.Size() == 0 {
				t.Error("output file is empty")
			}

			// Read and verify content.
			content, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatalf("read output file: %v", err)
			}
			if !strings.Contains(string(content), "test-project") {
				t.Error("output file does not contain project name")
			}
		})
	}
}

func TestWriteSkillFile_CreatesSubdirectories(t *testing.T) {
	repoRoot := findRepoRoot(t)
	gen := NewGenerator(repoRoot)
	if err := gen.LoadTemplates(); err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}

	tmpDir := t.TempDir()
	data := testTemplateData()

	// Claude Code output goes to .claude/CLAUDE.md -- verify the directory is created.
	outputPath, err := gen.WriteSkillFile(RuntimeClaudeCode, data, tmpDir)
	if err != nil {
		t.Fatalf("WriteSkillFile error: %v", err)
	}

	expectedDir := filepath.Join(tmpDir, ".claude")
	info, err := os.Stat(expectedDir)
	if err != nil {
		t.Fatalf(".claude directory was not created: %v", err)
	}
	if !info.IsDir() {
		t.Error(".claude is not a directory")
	}

	if !strings.HasSuffix(outputPath, ".claude/CLAUDE.md") {
		t.Errorf("unexpected output path: %s", outputPath)
	}
}

func TestOutputPaths(t *testing.T) {
	// Verify all runtimes have output paths defined.
	for _, rt := range SupportedRuntimes() {
		if _, ok := OutputPaths[rt]; !ok {
			t.Errorf("no output path for runtime %s", rt)
		}
	}

	// Verify specific paths per Architecture Section 25.2.
	checks := map[Runtime]string{
		RuntimeClaw:       "axiom-skill.md",
		RuntimeClaudeCode: ".claude/CLAUDE.md",
		RuntimeCodex:      "codex-instructions.md",
		RuntimeOpenCode:   "opencode-instructions.md",
	}
	for rt, want := range checks {
		got := OutputPaths[rt]
		if got != want {
			t.Errorf("OutputPaths[%s] = %q, want %q", rt, got, want)
		}
	}
}

func TestVerifyContent(t *testing.T) {
	// A string with all topics should return no missing items.
	full := strings.Join(ContentTopics(), "\n")
	missing := VerifyContent(full)
	if len(missing) != 0 {
		t.Errorf("expected no missing topics, got %v", missing)
	}

	// An empty string should report all topics missing.
	missing = VerifyContent("")
	if len(missing) != len(ContentTopics()) {
		t.Errorf("expected %d missing topics, got %d", len(ContentTopics()), len(missing))
	}
}
