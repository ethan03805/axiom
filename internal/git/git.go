package git

// Repo provides git operations for the project repository.
type Repo struct {
	rootDir      string
	branchPrefix string
	autoCommit   bool
}

// BranchInfo holds information about a git branch.
type BranchInfo struct {
	Name   string
	Commit string
	IsHead bool
}

// CommitInfo holds information about a git commit.
type CommitInfo struct {
	Hash    string
	Message string
	Author  string
	Date    string
}

// DiffEntry represents a single file change in a diff.
type DiffEntry struct {
	Path      string
	Operation string
	Additions int
	Deletions int
}

// New creates a new Repo instance.
func New(rootDir, branchPrefix string, autoCommit bool) *Repo {
	return &Repo{
		rootDir:      rootDir,
		branchPrefix: branchPrefix,
		autoCommit:   autoCommit,
	}
}

// Snapshot creates a snapshot (commit) of the current working tree state.
func (r *Repo) Snapshot(message string) (string, error) {
	return "", nil
}

// CreateBranch creates a new branch from the given base.
func (r *Repo) CreateBranch(name, base string) error {
	return nil
}

// Checkout switches to the given branch.
func (r *Repo) Checkout(branch string) error {
	return nil
}

// Diff returns the diff between two commits or branches.
func (r *Repo) Diff(from, to string) ([]DiffEntry, error) {
	return nil, nil
}

// MergeBranch merges source branch into the current branch.
func (r *Repo) MergeBranch(source string) error {
	return nil
}

// CurrentBranch returns the name of the current branch.
func (r *Repo) CurrentBranch() (string, error) {
	return "", nil
}

// HeadCommit returns the hash of the current HEAD commit.
func (r *Repo) HeadCommit() (string, error) {
	return "", nil
}

// ListBranches returns all branches with the configured prefix.
func (r *Repo) ListBranches() ([]BranchInfo, error) {
	return nil, nil
}

// DeleteBranch deletes a branch by name.
func (r *Repo) DeleteBranch(name string) error {
	return nil
}
