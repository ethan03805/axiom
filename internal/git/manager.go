// Package git implements Git operations for the Axiom Trusted Engine.
// All git commands are executed by the engine; no LLM agent has direct git access.
//
// See Architecture.md Section 23 for the Git Integration specification.
package git

import (
	"fmt"
	"os/exec"
	"strings"
)

// Manager handles all git operations for the project.
// Operations are executed via the git CLI for reliability across git versions.
type Manager struct {
	rootDir      string
	branchPrefix string
}

// NewManager creates a Git Manager for the given project root.
// branchPrefix is typically "axiom" (producing branches like "axiom/my-project").
func NewManager(rootDir, branchPrefix string) *Manager {
	return &Manager{
		rootDir:      rootDir,
		branchPrefix: branchPrefix,
	}
}

// CommitMetadata holds the information needed to create an Axiom-format commit.
// See Architecture Section 23.2 for the commit message format.
type CommitMetadata struct {
	TaskID        string
	TaskTitle     string
	SRSRefs       []string
	MeeseeksModel string
	ReviewerModel string
	AttemptNumber int
	MaxAttempts   int
	CostUSD       float64
	BaseSnapshot  string
}

// CheckClean verifies the working tree is clean (no uncommitted changes).
// Axiom refuses to start on a dirty working tree to prevent merge conflicts.
// See Architecture Section 28.2.
func (m *Manager) CheckClean() error {
	output, err := m.run("status", "--porcelain")
	if err != nil {
		return fmt.Errorf("git status: %w", err)
	}
	output = strings.TrimSpace(output)
	if output != "" {
		lines := strings.Split(output, "\n")
		return fmt.Errorf("dirty working tree (%d uncommitted files). "+
			"Please commit or stash your changes before running axiom.\n"+
			"Uncommitted files:\n  %s", len(lines), strings.Join(lines, "\n  "))
	}
	return nil
}

// CreateProjectBranch creates the axiom project branch from the current HEAD.
// Branch name format: <branchPrefix>/<slug>
// See Architecture Section 23.1.
func (m *Manager) CreateProjectBranch(slug string) (string, error) {
	branchName := m.branchPrefix + "/" + slug

	// Check if branch already exists.
	_, err := m.run("rev-parse", "--verify", branchName)
	if err == nil {
		// Branch exists, just check it out.
		if _, err := m.run("checkout", branchName); err != nil {
			return "", fmt.Errorf("checkout existing branch %s: %w", branchName, err)
		}
		return branchName, nil
	}

	// Create and checkout new branch from current HEAD.
	if _, err := m.run("checkout", "-b", branchName); err != nil {
		return "", fmt.Errorf("create branch %s: %w", branchName, err)
	}

	return branchName, nil
}

// HeadSHA returns the SHA of the current HEAD commit.
func (m *Manager) HeadSHA() (string, error) {
	output, err := m.run("rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get HEAD: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// CurrentBranch returns the name of the current branch.
func (m *Manager) CurrentBranch() (string, error) {
	output, err := m.run("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("get current branch: %w", err)
	}
	return strings.TrimSpace(output), nil
}

// Commit creates a commit with the Axiom-format message.
// See Architecture Section 23.2 for the format.
func (m *Manager) Commit(meta *CommitMetadata) (string, error) {
	message := formatCommitMessage(meta)

	// Stage all changes.
	if _, err := m.run("add", "-A"); err != nil {
		return "", fmt.Errorf("git add: %w", err)
	}

	// Check if there's anything to commit.
	status, err := m.run("status", "--porcelain")
	if err != nil {
		return "", fmt.Errorf("git status: %w", err)
	}
	if strings.TrimSpace(status) == "" {
		return "", fmt.Errorf("nothing to commit")
	}

	// Create the commit.
	if _, err := m.run("commit", "-m", message); err != nil {
		return "", fmt.Errorf("git commit: %w", err)
	}

	// Return the new commit SHA.
	return m.HeadSHA()
}

// ApplyFiles copies staged output files to the working tree.
// Files from stagingDir are copied to their declared paths relative to rootDir.
func (m *Manager) ApplyFiles(files map[string]string) error {
	for relPath, content := range files {
		fullPath := m.rootDir + "/" + relPath
		// Ensure parent directory exists.
		dir := fullPath[:strings.LastIndex(fullPath, "/")]
		if _, err := exec.Command("mkdir", "-p", dir).Output(); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
		if err := writeFile(fullPath, content); err != nil {
			return fmt.Errorf("write %s: %w", relPath, err)
		}
	}
	return nil
}

// DeleteFiles removes files from the working tree.
func (m *Manager) DeleteFiles(paths []string) error {
	for _, p := range paths {
		if _, err := m.run("rm", "-f", p); err != nil {
			// File may not exist in git; try OS remove.
			exec.Command("rm", "-f", m.rootDir+"/"+p).Run()
		}
	}
	return nil
}

// IsAncestor checks if ancestorSHA is an ancestor of descendantSHA.
// Used to determine if a base snapshot is stale.
func (m *Manager) IsAncestor(ancestorSHA, descendantSHA string) (bool, error) {
	_, err := m.run("merge-base", "--is-ancestor", ancestorSHA, descendantSHA)
	if err != nil {
		// Exit code 1 means not an ancestor (not an error).
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return false, nil
		}
		return false, fmt.Errorf("is-ancestor check: %w", err)
	}
	return true, nil
}

// DiffFiles returns the list of files changed between two SHAs.
func (m *Manager) DiffFiles(fromSHA, toSHA string) ([]string, error) {
	output, err := m.run("diff", "--name-only", fromSHA, toSHA)
	if err != nil {
		return nil, fmt.Errorf("diff: %w", err)
	}
	output = strings.TrimSpace(output)
	if output == "" {
		return nil, nil
	}
	return strings.Split(output, "\n"), nil
}

// ResetHard resets the working tree to the given SHA and removes untracked
// files. Used for reverting failed merge attempts to a clean state.
func (m *Manager) ResetHard(sha string) error {
	if _, err := m.run("reset", "--hard", sha); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	// Remove untracked files that were created during the failed merge.
	if _, err := m.run("clean", "-fd"); err != nil {
		// Non-fatal: the reset succeeded, clean is best-effort.
		_ = err
	}
	return nil
}

// formatCommitMessage builds the commit message per Architecture Section 23.2.
func formatCommitMessage(meta *CommitMetadata) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[axiom] %s\n\n", meta.TaskTitle))
	b.WriteString(fmt.Sprintf("Task: %s\n", meta.TaskID))
	if len(meta.SRSRefs) > 0 {
		b.WriteString(fmt.Sprintf("SRS Refs: %s\n", strings.Join(meta.SRSRefs, ", ")))
	}
	if meta.MeeseeksModel != "" {
		b.WriteString(fmt.Sprintf("Meeseeks Model: %s\n", meta.MeeseeksModel))
	}
	if meta.ReviewerModel != "" {
		b.WriteString(fmt.Sprintf("Reviewer Model: %s\n", meta.ReviewerModel))
	}
	b.WriteString(fmt.Sprintf("Attempt: %d/%d\n", meta.AttemptNumber, meta.MaxAttempts))
	b.WriteString(fmt.Sprintf("Cost: $%.4f\n", meta.CostUSD))
	if meta.BaseSnapshot != "" {
		b.WriteString(fmt.Sprintf("Base Snapshot: %s\n", meta.BaseSnapshot[:minLen(len(meta.BaseSnapshot), 7)]))
	}
	return b.String()
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// run executes a git command in the project root directory.
func (m *Manager) run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = m.rootDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("git %s: %s: %w", args[0], strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}

// writeFile writes content to a file, creating parent directories as needed.
func writeFile(path, content string) error {
	return exec.Command("sh", "-c", fmt.Sprintf("cat > %q << 'AXIOM_EOF'\n%s\nAXIOM_EOF", path, content)).Run()
}
