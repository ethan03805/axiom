package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RouterConfig holds configuration for the file router.
type RouterConfig struct {
	MaxFileSize int64 // Maximum file size in bytes (default 1MB)
}

// DefaultRouterConfig returns the default file router configuration.
func DefaultRouterConfig() RouterConfig {
	return RouterConfig{
		MaxFileSize: 1 * 1024 * 1024, // 1MB per Architecture Section 29.5
	}
}

// ValidatePathSafety checks all paths in a manifest for safety violations.
// Returns a list of validation errors.
//
// Checks performed per Architecture Section 14.2 and Section 29.5:
//   - Path canonicalization (reject ".." traversal)
//   - Symlink rejection
//   - Device file and FIFO rejection
//   - Oversized file rejection (configurable max, default 1MB)
//   - Scope enforcement (files must be in task's declared target_files)
//   - No absolute paths
func ValidatePathSafety(m *Manifest, stagingDir string, allowedPaths map[string]bool, config RouterConfig) []string {
	var errors []string

	for _, f := range m.Files.Added {
		errors = append(errors, validateFilePath(f.Path, stagingDir, allowedPaths, config)...)
	}
	for _, f := range m.Files.Modified {
		errors = append(errors, validateFilePath(f.Path, stagingDir, allowedPaths, config)...)
	}
	for _, path := range m.Files.Deleted {
		errors = append(errors, validateDeclaredPath(path, allowedPaths)...)
	}
	for _, r := range m.Files.Renamed {
		errors = append(errors, validateDeclaredPath(r.From, allowedPaths)...)
		errors = append(errors, validateFilePath(r.To, stagingDir, allowedPaths, config)...)
	}

	return errors
}

// validateFilePath checks a single file path for safety violations.
// The file must exist in the staging directory.
func validateFilePath(relPath, stagingDir string, allowedPaths map[string]bool, config RouterConfig) []string {
	var errors []string

	// Reject empty paths.
	if relPath == "" {
		errors = append(errors, "empty file path")
		return errors
	}

	// Reject absolute paths.
	if filepath.IsAbs(relPath) {
		errors = append(errors, fmt.Sprintf("absolute path not allowed: %s", relPath))
		return errors
	}

	// Reject directory traversal (e.g. "../../../etc/passwd").
	if pathContainsTraversal(relPath) {
		errors = append(errors, fmt.Sprintf("path traversal detected: %s", relPath))
		return errors
	}

	// Reject paths starting with .axiom/ (internal state should never be written).
	normalized := filepath.ToSlash(relPath)
	if strings.HasPrefix(normalized, ".axiom/") || normalized == ".axiom" {
		errors = append(errors, fmt.Sprintf("path targets internal .axiom directory: %s", relPath))
		return errors
	}

	// Scope enforcement: file must be in the task's allowed paths.
	// If allowedPaths is nil, scope checking is skipped (e.g. for tests).
	if allowedPaths != nil && !allowedPaths[relPath] {
		errors = append(errors, fmt.Sprintf("file outside task scope: %s", relPath))
	}

	// Check the file in staging for symlinks, device files, FIFOs, and size.
	fullPath := filepath.Join(stagingDir, relPath)
	info, err := os.Lstat(fullPath) // Lstat: don't follow symlinks
	if err != nil {
		if os.IsNotExist(err) {
			// Already caught by manifest validation; don't double-report.
			return errors
		}
		errors = append(errors, fmt.Sprintf("cannot stat %s: %v", relPath, err))
		return errors
	}

	mode := info.Mode()

	// Reject symlinks.
	if mode&os.ModeSymlink != 0 {
		errors = append(errors, fmt.Sprintf("symlink not allowed: %s", relPath))
	}

	// Reject device files.
	if mode&os.ModeDevice != 0 {
		errors = append(errors, fmt.Sprintf("device file not allowed: %s", relPath))
	}

	// Reject named pipes (FIFOs).
	if mode&os.ModeNamedPipe != 0 {
		errors = append(errors, fmt.Sprintf("named pipe (FIFO) not allowed: %s", relPath))
	}

	// Reject irregular files (sockets, etc).
	if !mode.IsRegular() && mode&os.ModeSymlink == 0 {
		errors = append(errors, fmt.Sprintf("irregular file type not allowed: %s (mode %s)", relPath, mode))
	}

	// Reject oversized files.
	if mode.IsRegular() && info.Size() > config.MaxFileSize {
		errors = append(errors, fmt.Sprintf("file too large: %s (%d bytes, max %d)",
			relPath, info.Size(), config.MaxFileSize))
	}

	return errors
}

// validateDeclaredPath checks a path that is only declared (deletions, rename sources)
// but doesn't need to exist in staging.
func validateDeclaredPath(relPath string, allowedPaths map[string]bool) []string {
	var errors []string

	if relPath == "" {
		errors = append(errors, "empty declared path")
		return errors
	}
	if filepath.IsAbs(relPath) {
		errors = append(errors, fmt.Sprintf("absolute path not allowed: %s", relPath))
	}
	if pathContainsTraversal(relPath) {
		errors = append(errors, fmt.Sprintf("path traversal detected: %s", relPath))
	}
	if allowedPaths != nil && !allowedPaths[relPath] {
		errors = append(errors, fmt.Sprintf("declared path outside task scope: %s", relPath))
	}

	return errors
}

// BuildAllowedPaths creates a set of allowed paths from a task's target files
// list. This is used for scope enforcement during path safety validation.
func BuildAllowedPaths(targetFiles []string) map[string]bool {
	allowed := make(map[string]bool, len(targetFiles))
	for _, f := range targetFiles {
		allowed[f] = true
	}
	return allowed
}
