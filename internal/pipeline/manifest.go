// Package pipeline implements the File Router and multi-stage Approval Pipeline.
// No file reaches the project filesystem without passing through the full pipeline.
// All operations are executed by the Trusted Engine.
//
// See Architecture.md Section 14 for the complete specification.
package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manifest represents the output manifest that every Meeseeks must emit
// alongside its output files in /workspace/staging/.
// See Architecture Section 10.4 for the format specification.
type Manifest struct {
	TaskID       string       `json:"task_id"`
	BaseSnapshot string       `json:"base_snapshot"`
	Files        ManifestFiles `json:"files"`
}

// ManifestFiles groups file operations by type.
type ManifestFiles struct {
	Added    []FileEntry   `json:"added"`
	Modified []FileEntry   `json:"modified"`
	Deleted  []string      `json:"deleted"`
	Renamed  []RenameEntry `json:"renamed"`
}

// FileEntry represents an added or modified file in the manifest.
type FileEntry struct {
	Path      string `json:"path"`
	Binary    bool   `json:"binary"`
	SizeBytes int64  `json:"size_bytes,omitempty"` // Required for binary files
}

// RenameEntry represents a file rename operation.
type RenameEntry struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ParseManifest reads and parses a manifest.json file from the staging directory.
func ParseManifest(stagingDir string) (*Manifest, error) {
	manifestPath := filepath.Join(stagingDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("read manifest.json: %w", err)
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest.json: %w", err)
	}

	return &m, nil
}

// ValidateManifest performs comprehensive validation of a manifest against
// the staging directory contents. Returns a list of validation errors.
//
// Checks performed per Architecture Section 14.2 (Stage 1):
//   - All files listed in manifest exist in staging
//   - No unlisted files exist in staging (except manifest.json itself)
//   - Binary files have the size_bytes field set
//   - All paths are non-empty
//   - Manifest has a task_id
func ValidateManifest(m *Manifest, stagingDir string) []string {
	var errors []string

	if m.TaskID == "" {
		errors = append(errors, "manifest missing task_id")
	}

	// Collect all declared file paths from manifest.
	declared := make(map[string]bool)
	declared["manifest.json"] = true // The manifest itself is expected.

	// Validate added files.
	for _, f := range m.Files.Added {
		if f.Path == "" {
			errors = append(errors, "added file has empty path")
			continue
		}
		declared[f.Path] = true

		// Check file exists in staging.
		fullPath := filepath.Join(stagingDir, f.Path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("added file not found in staging: %s", f.Path))
		}

		// Binary files must have size_bytes.
		if f.Binary && f.SizeBytes == 0 {
			errors = append(errors, fmt.Sprintf("binary file missing size_bytes: %s", f.Path))
		}
	}

	// Validate modified files.
	for _, f := range m.Files.Modified {
		if f.Path == "" {
			errors = append(errors, "modified file has empty path")
			continue
		}
		declared[f.Path] = true

		fullPath := filepath.Join(stagingDir, f.Path)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("modified file not found in staging: %s", f.Path))
		}

		if f.Binary && f.SizeBytes == 0 {
			errors = append(errors, fmt.Sprintf("binary modified file missing size_bytes: %s", f.Path))
		}
	}

	// Validate deleted files (just path existence in the declaration).
	for _, path := range m.Files.Deleted {
		if path == "" {
			errors = append(errors, "deleted file has empty path")
		}
		// Deleted files are NOT expected in staging (they're being removed).
	}

	// Validate renamed files.
	for _, r := range m.Files.Renamed {
		if r.From == "" || r.To == "" {
			errors = append(errors, "rename entry has empty from or to path")
			continue
		}
		declared[r.To] = true
		// The "to" file should exist in staging (the new name).
		fullPath := filepath.Join(stagingDir, r.To)
		if _, err := os.Stat(fullPath); os.IsNotExist(err) {
			errors = append(errors, fmt.Sprintf("renamed file target not found in staging: %s", r.To))
		}
	}

	// Check for unlisted files in staging.
	// Walk the staging directory and flag any file not in the manifest.
	err := filepath.Walk(stagingDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		relPath, err := filepath.Rel(stagingDir, path)
		if err != nil {
			return nil
		}
		// Normalize separators for cross-platform.
		relPath = filepath.ToSlash(relPath)
		if !declared[relPath] {
			errors = append(errors, fmt.Sprintf("unlisted file found in staging: %s", relPath))
		}
		return nil
	})
	if err != nil {
		errors = append(errors, fmt.Sprintf("error scanning staging directory: %v", err))
	}

	return errors
}

// AllPaths returns all file paths referenced by the manifest (added, modified,
// deleted, renamed to). Used for scope checking.
func (m *Manifest) AllPaths() []string {
	var paths []string
	for _, f := range m.Files.Added {
		paths = append(paths, f.Path)
	}
	for _, f := range m.Files.Modified {
		paths = append(paths, f.Path)
	}
	paths = append(paths, m.Files.Deleted...)
	for _, r := range m.Files.Renamed {
		paths = append(paths, r.From, r.To)
	}
	return paths
}

// NonBinaryPaths returns paths of non-binary files (for compilation/linting).
func (m *Manifest) NonBinaryPaths() []string {
	var paths []string
	for _, f := range m.Files.Added {
		if !f.Binary {
			paths = append(paths, f.Path)
		}
	}
	for _, f := range m.Files.Modified {
		if !f.Binary {
			paths = append(paths, f.Path)
		}
	}
	return paths
}

// HasChanges returns true if the manifest declares any file operations.
func (m *Manifest) HasChanges() bool {
	return len(m.Files.Added) > 0 || len(m.Files.Modified) > 0 ||
		len(m.Files.Deleted) > 0 || len(m.Files.Renamed) > 0
}

// pathContainsTraversal checks if a path contains directory traversal.
func pathContainsTraversal(p string) bool {
	normalized := filepath.ToSlash(p)
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}
