package merge

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ethan03805/axiom/internal/events"
	"github.com/ethan03805/axiom/internal/git"
)

func setupTestQueue(t *testing.T) (*Queue, *git.Manager, string) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "axiom-merge-test-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Init git repo with initial commit.
	runGit(t, tmpDir, "init")
	runGit(t, tmpDir, "config", "user.email", "test@axiom.dev")
	runGit(t, tmpDir, "config", "user.name", "Axiom Test")
	os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# Test"), 0644)
	runGit(t, tmpDir, "add", "-A")
	runGit(t, tmpDir, "commit", "-m", "initial")

	// Create axiom branch.
	runGit(t, tmpDir, "checkout", "-b", "axiom/test-project")

	gitMgr := git.NewManager(tmpDir, "axiom")
	emitter := events.NewEmitter()
	q := NewQueue(gitMgr, emitter)

	return q, gitMgr, tmpDir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s: %v", args[0], string(out), err)
	}
}

func TestMergeQueueCurrentSnapshot(t *testing.T) {
	q, gitMgr, tmpDir := setupTestQueue(t)

	baseSHA, _ := gitMgr.HeadSHA()

	q.Submit(&MergeItem{
		TaskID:       "task-001",
		TaskTitle:    "Add auth handler",
		BaseSnapshot: baseSHA,
		Files: map[string]string{
			"src/auth.go": "package src\n\nfunc Auth() {}\n",
		},
		SRSRefs:       []string{"FR-001"},
		MeeseeksModel: "claude-4-sonnet",
		AttemptNumber: 1,
		MaxAttempts:   3,
	})

	// Ensure src dir exists.
	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	result, ok := q.ProcessNext()
	if !ok {
		t.Fatal("expected item to process")
	}
	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if result.CommitSHA == "" {
		t.Error("expected commit SHA")
	}

	// Verify commit exists.
	cmd := exec.Command("git", "log", "-1", "--format=%s")
	cmd.Dir = tmpDir
	out, _ := cmd.Output()
	if len(out) == 0 {
		t.Error("no commit found")
	}
}

func TestMergeQueueStaleSnapshot(t *testing.T) {
	q, gitMgr, tmpDir := setupTestQueue(t)

	baseSHA, _ := gitMgr.HeadSHA()

	// Advance HEAD by making another commit.
	os.WriteFile(filepath.Join(tmpDir, "other.txt"), []byte("other work"), 0644)
	runGit(t, tmpDir, "add", "-A")
	runGit(t, tmpDir, "commit", "-m", "advance HEAD")

	// Submit with the old (stale) base snapshot.
	q.Submit(&MergeItem{
		TaskID:       "task-stale",
		TaskTitle:    "Stale task",
		BaseSnapshot: baseSHA,
		Files:        map[string]string{"new.go": "package main"},
	})

	result, ok := q.ProcessNext()
	if !ok {
		t.Fatal("expected item to process")
	}
	if result.Success {
		t.Error("expected failure for stale snapshot")
	}
	if !result.NeedsRequeue {
		t.Error("expected NeedsRequeue for stale snapshot")
	}
	if len(result.ChangedFiles) == 0 {
		t.Error("expected changed files list for stale snapshot")
	}
}

func TestMergeQueueIntegrationFailure(t *testing.T) {
	q, gitMgr, tmpDir := setupTestQueue(t)

	baseSHA, _ := gitMgr.HeadSHA()

	// Set up a validation function that always fails.
	q.ValidateFn = func(taskID string) error {
		return fmt.Errorf("compilation error: syntax error in auth.go")
	}

	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	q.Submit(&MergeItem{
		TaskID:       "task-intfail",
		TaskTitle:    "Integration fail",
		BaseSnapshot: baseSHA,
		Files:        map[string]string{"src/bad.go": "package src // broken"},
	})

	result, _ := q.ProcessNext()
	if result.Success {
		t.Error("expected failure from integration check")
	}
	if !result.NeedsRequeue {
		t.Error("expected NeedsRequeue on integration failure")
	}

	// Verify working tree was reverted (reset --hard).
	// The bad file should not exist after revert.
	_, err := os.Stat(filepath.Join(tmpDir, "src", "bad.go"))
	if err == nil {
		t.Error("expected bad.go to be reverted after integration failure")
	}
}

func TestMergeQueueSerialization(t *testing.T) {
	q, gitMgr, tmpDir := setupTestQueue(t)

	baseSHA, _ := gitMgr.HeadSHA()

	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	// Submit two items.
	q.Submit(&MergeItem{
		TaskID:       "task-a",
		TaskTitle:    "First task",
		BaseSnapshot: baseSHA,
		Files:        map[string]string{"src/a.go": "package src\nfunc A() {}\n"},
		AttemptNumber: 1, MaxAttempts: 3,
	})

	// Process first.
	result1, _ := q.ProcessNext()
	if !result1.Success {
		t.Fatalf("first merge failed: %s", result1.Error)
	}

	// Get new HEAD for second item's snapshot.
	newSHA, _ := gitMgr.HeadSHA()

	q.Submit(&MergeItem{
		TaskID:       "task-b",
		TaskTitle:    "Second task",
		BaseSnapshot: newSHA,
		Files:        map[string]string{"src/b.go": "package src\nfunc B() {}\n"},
		AttemptNumber: 1, MaxAttempts: 3,
	})

	result2, _ := q.ProcessNext()
	if !result2.Success {
		t.Fatalf("second merge failed: %s", result2.Error)
	}

	// Verify two commits were made.
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = tmpDir
	out, _ := cmd.Output()
	lines := 0
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	if lines < 3 { // initial + task-a + task-b
		t.Errorf("expected at least 3 commits, got %d", lines)
	}
}

func TestMergeQueueEmptyDoesNothing(t *testing.T) {
	q, _, _ := setupTestQueue(t)

	_, ok := q.ProcessNext()
	if ok {
		t.Error("expected no item to process on empty queue")
	}
}

func TestMergeQueueDepth(t *testing.T) {
	q, gitMgr, _ := setupTestQueue(t)
	baseSHA, _ := gitMgr.HeadSHA()

	if q.Depth() != 0 {
		t.Errorf("expected depth 0, got %d", q.Depth())
	}

	q.Submit(&MergeItem{TaskID: "t1", BaseSnapshot: baseSHA})
	q.Submit(&MergeItem{TaskID: "t2", BaseSnapshot: baseSHA})

	if q.Depth() != 2 {
		t.Errorf("expected depth 2, got %d", q.Depth())
	}
}

func TestMergeQueueReindex(t *testing.T) {
	q, gitMgr, tmpDir := setupTestQueue(t)
	baseSHA, _ := gitMgr.HeadSHA()

	var reindexedFiles []string
	q.ReindexFn = func(files []string) error {
		reindexedFiles = files
		return nil
	}

	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)

	q.Submit(&MergeItem{
		TaskID:       "task-reindex",
		TaskTitle:    "Reindex test",
		BaseSnapshot: baseSHA,
		Files:        map[string]string{"src/indexed.go": "package src"},
		AttemptNumber: 1, MaxAttempts: 3,
	})

	result, _ := q.ProcessNext()
	if !result.Success {
		t.Fatalf("merge failed: %s", result.Error)
	}

	if len(reindexedFiles) != 1 || reindexedFiles[0] != "src/indexed.go" {
		t.Errorf("expected reindex of [src/indexed.go], got %v", reindexedFiles)
	}
}

// Ensure imports are used.
var (
	_ = strings.Contains
	_ = fmt.Sprintf
)
