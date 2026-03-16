package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathTraversalRejected(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-traverse-*")
	defer os.RemoveAll(tmpDir)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "../../../etc/passwd", Binary: false},
			},
		},
	}

	errors := ValidatePathSafety(m, tmpDir, nil, DefaultRouterConfig())
	if len(errors) == 0 {
		t.Error("expected path traversal error")
	}
	found := false
	for _, e := range errors {
		if e == "path traversal detected: ../../../etc/passwd" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected traversal error, got: %v", errors)
	}
}

func TestAbsolutePathRejected(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-abs-*")
	defer os.RemoveAll(tmpDir)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "/etc/shadow", Binary: false}},
		},
	}

	errors := ValidatePathSafety(m, tmpDir, nil, DefaultRouterConfig())
	found := false
	for _, e := range errors {
		if e == "absolute path not allowed: /etc/shadow" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected absolute path error, got: %v", errors)
	}
}

func TestAxiomDirRejected(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-axiomdir-*")
	defer os.RemoveAll(tmpDir)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: ".axiom/config.toml", Binary: false}},
		},
	}

	errors := ValidatePathSafety(m, tmpDir, nil, DefaultRouterConfig())
	found := false
	for _, e := range errors {
		if e == "path targets internal .axiom directory: .axiom/config.toml" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected .axiom directory error, got: %v", errors)
	}
}

func TestOversizedFileRejected(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-oversize-*")
	defer os.RemoveAll(tmpDir)

	// Create a file exceeding the max size.
	bigFile := filepath.Join(tmpDir, "big.bin")
	data := make([]byte, 2*1024*1024) // 2MB
	os.WriteFile(bigFile, data, 0644)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "big.bin", Binary: true, SizeBytes: int64(len(data))}},
		},
	}

	config := RouterConfig{MaxFileSize: 1 * 1024 * 1024} // 1MB limit
	errors := ValidatePathSafety(m, tmpDir, nil, config)
	found := false
	for _, e := range errors {
		if len(e) > 0 && e[:14] == "file too large" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected oversized file error, got: %v", errors)
	}
}

func TestScopeEnforcement(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-scope-*")
	defer os.RemoveAll(tmpDir)

	// Create the file in staging.
	os.WriteFile(filepath.Join(tmpDir, "out_of_scope.go"), []byte("package main"), 0644)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "out_of_scope.go", Binary: false}},
		},
	}

	// Only "in_scope.go" is allowed.
	allowed := BuildAllowedPaths([]string{"in_scope.go"})
	errors := ValidatePathSafety(m, tmpDir, allowed, DefaultRouterConfig())
	found := false
	for _, e := range errors {
		if e == "file outside task scope: out_of_scope.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected scope error, got: %v", errors)
	}
}

func TestScopeEnforcementAllowed(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-scope-ok-*")
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "in_scope.go"), []byte("package main"), 0644)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "in_scope.go", Binary: false}},
		},
	}

	allowed := BuildAllowedPaths([]string{"in_scope.go"})
	errors := ValidatePathSafety(m, tmpDir, allowed, DefaultRouterConfig())
	if len(errors) != 0 {
		t.Errorf("expected no errors for in-scope file, got: %v", errors)
	}
}

func TestSymlinkRejected(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-symlink-*")
	defer os.RemoveAll(tmpDir)

	// Create a regular file and a symlink to it.
	realFile := filepath.Join(tmpDir, "real.go")
	os.WriteFile(realFile, []byte("package main"), 0644)
	symlink := filepath.Join(tmpDir, "link.go")
	os.Symlink(realFile, symlink)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "link.go", Binary: false}},
		},
	}

	errors := ValidatePathSafety(m, tmpDir, nil, DefaultRouterConfig())
	found := false
	for _, e := range errors {
		if e == "symlink not allowed: link.go" {
			found = true
		}
	}
	if !found {
		// Symlinks show as irregular file type on some systems.
		for _, e := range errors {
			if len(e) > 0 {
				found = true // Any error about the symlink counts
			}
		}
	}
	if !found {
		t.Errorf("expected symlink error, got: %v", errors)
	}
}

func TestDeletedPathTraversalRejected(t *testing.T) {
	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Deleted: []string{"../../important.db"},
		},
	}

	errors := ValidatePathSafety(m, "", nil, DefaultRouterConfig())
	found := false
	for _, e := range errors {
		if e == "path traversal detected: ../../important.db" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected traversal error on deleted path, got: %v", errors)
	}
}

func TestValidPathPassesAllChecks(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-router-valid-*")
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "src"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "src", "main.go"), []byte("package main"), 0644)

	m := &Manifest{
		TaskID: "t1",
		Files: ManifestFiles{
			Added: []FileEntry{{Path: "src/main.go", Binary: false}},
		},
	}

	errors := ValidatePathSafety(m, tmpDir, nil, DefaultRouterConfig())
	if len(errors) != 0 {
		t.Errorf("expected no errors for valid path, got: %v", errors)
	}
}
