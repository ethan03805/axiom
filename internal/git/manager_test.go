package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo creates a temporary directory with an initialized git repo.
func setupTestRepo(t *testing.T) (*Manager, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-git-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Initialize a git repo.
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@axiom.dev")
	runGit(t, tmpDir, "config", "user.name", "Axiom Test")

	// Create an initial commit so HEAD exists.
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test Project"), 0644)
	runGit(t, tmpDir, "add", "-A")
	runGit(t, tmpDir, "commit", "-m", "Initial commit")

	mgr := NewManager(tmpDir, "axiom")
	return mgr, tmpDir
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s: %v", args[0], string(output), err)
	}
	return string(output)
}

func TestCheckCleanOnCleanRepo(t *testing.T) {
	mgr, _ := setupTestRepo(t)

	if err := mgr.CheckClean(); err != nil {
		t.Errorf("expected clean repo, got: %v", err)
	}
}

func TestCheckCleanOnDirtyRepo(t *testing.T) {
	mgr, tmpDir := setupTestRepo(t)

	// Create an uncommitted file.
	os.WriteFile(filepath.Join(tmpDir, "dirty.txt"), []byte("uncommitted"), 0644)

	err := mgr.CheckClean()
	if err == nil {
		t.Error("expected dirty working tree error")
	}
	if !strings.Contains(err.Error(), "dirty working tree") {
		t.Errorf("error should mention dirty working tree: %v", err)
	}
	if !strings.Contains(err.Error(), "dirty.txt") {
		t.Errorf("error should list the uncommitted file: %v", err)
	}
}

func TestCreateProjectBranch(t *testing.T) {
	mgr, _ := setupTestRepo(t)

	branchName, err := mgr.CreateProjectBranch("my-project")
	if err != nil {
		t.Fatalf("create branch: %v", err)
	}

	if branchName != "axiom/my-project" {
		t.Errorf("expected axiom/my-project, got %s", branchName)
	}

	// Verify we're on the new branch.
	current, err := mgr.CurrentBranch()
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if current != "axiom/my-project" {
		t.Errorf("expected to be on axiom/my-project, got %s", current)
	}
}

func TestCreateProjectBranchIdempotent(t *testing.T) {
	mgr, _ := setupTestRepo(t)

	// Create branch twice -- second call should just checkout.
	mgr.CreateProjectBranch("my-project")
	branchName, err := mgr.CreateProjectBranch("my-project")
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	if branchName != "axiom/my-project" {
		t.Errorf("expected axiom/my-project, got %s", branchName)
	}
}

func TestHeadSHA(t *testing.T) {
	mgr, _ := setupTestRepo(t)

	sha, err := mgr.HeadSHA()
	if err != nil {
		t.Fatalf("head sha: %v", err)
	}
	if len(sha) < 7 {
		t.Errorf("SHA too short: %s", sha)
	}
}

func TestCommitFormat(t *testing.T) {
	mgr, tmpDir := setupTestRepo(t)
	mgr.CreateProjectBranch("test-proj")

	// Create a file to commit.
	os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0644)

	sha, err := mgr.Commit(&CommitMetadata{
		TaskID:        "task-001",
		TaskTitle:     "Implement auth handler",
		SRSRefs:       []string{"FR-001", "AC-003"},
		MeeseeksModel: "anthropic/claude-4-sonnet",
		ReviewerModel: "openai/gpt-4o",
		AttemptNumber: 2,
		MaxAttempts:   3,
		CostUSD:       0.0234,
		BaseSnapshot:  "abc123def456",
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}

	if len(sha) < 7 {
		t.Errorf("commit SHA too short: %s", sha)
	}

	// Verify commit message format.
	output := runGit(t, tmpDir, "log", "-1", "--format=%B")
	if !strings.Contains(output, "[axiom] Implement auth handler") {
		t.Errorf("commit message missing title: %s", output)
	}
	if !strings.Contains(output, "Task: task-001") {
		t.Errorf("commit message missing task ID: %s", output)
	}
	if !strings.Contains(output, "SRS Refs: FR-001, AC-003") {
		t.Errorf("commit message missing SRS refs: %s", output)
	}
	if !strings.Contains(output, "Meeseeks Model: anthropic/claude-4-sonnet") {
		t.Errorf("commit message missing model: %s", output)
	}
	if !strings.Contains(output, "Attempt: 2/3") {
		t.Errorf("commit message missing attempt: %s", output)
	}
	if !strings.Contains(output, "Cost: $0.0234") {
		t.Errorf("commit message missing cost: %s", output)
	}
	if !strings.Contains(output, "Base Snapshot: abc123d") {
		t.Errorf("commit message missing snapshot: %s", output)
	}
}

func TestIsAncestor(t *testing.T) {
	mgr, tmpDir := setupTestRepo(t)

	sha1, _ := mgr.HeadSHA()

	// Make a new commit.
	os.WriteFile(filepath.Join(tmpDir, "new.txt"), []byte("new"), 0644)
	runGit(t, tmpDir, "add", "-A")
	runGit(t, tmpDir, "commit", "-m", "second commit")

	sha2, _ := mgr.HeadSHA()

	// sha1 should be ancestor of sha2.
	isAnc, err := mgr.IsAncestor(sha1, sha2)
	if err != nil {
		t.Fatalf("is ancestor: %v", err)
	}
	if !isAnc {
		t.Error("sha1 should be ancestor of sha2")
	}

	// sha2 should NOT be ancestor of sha1.
	isAnc, _ = mgr.IsAncestor(sha2, sha1)
	if isAnc {
		t.Error("sha2 should not be ancestor of sha1")
	}
}

func TestDiffFiles(t *testing.T) {
	mgr, tmpDir := setupTestRepo(t)

	sha1, _ := mgr.HeadSHA()

	os.WriteFile(filepath.Join(tmpDir, "changed.txt"), []byte("changed"), 0644)
	runGit(t, tmpDir, "add", "-A")
	runGit(t, tmpDir, "commit", "-m", "add changed.txt")

	sha2, _ := mgr.HeadSHA()

	files, err := mgr.DiffFiles(sha1, sha2)
	if err != nil {
		t.Fatalf("diff files: %v", err)
	}
	if len(files) != 1 || files[0] != "changed.txt" {
		t.Errorf("expected [changed.txt], got %v", files)
	}
}

func TestSnapshotValidation(t *testing.T) {
	mgr, tmpDir := setupTestRepo(t)
	sm := NewSnapshotManager(mgr)

	// Current snapshot should be current.
	sha, _ := sm.CurrentSnapshot()
	status, err := sm.ValidateSnapshot(sha)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if status != SnapshotCurrent {
		t.Errorf("expected SnapshotCurrent, got %d", status)
	}

	// Advance HEAD.
	os.WriteFile(filepath.Join(tmpDir, "new.txt"), []byte("new"), 0644)
	runGit(t, tmpDir, "add", "-A")
	runGit(t, tmpDir, "commit", "-m", "advance HEAD")

	// Old snapshot should now be stale.
	status, err = sm.ValidateSnapshot(sha)
	if err != nil {
		t.Fatalf("validate stale: %v", err)
	}
	if status != SnapshotStale {
		t.Errorf("expected SnapshotStale, got %d", status)
	}
}
