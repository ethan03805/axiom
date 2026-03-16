package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-manifest-*")
	if err != nil {
		t.Fatalf("create temp: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Write a valid manifest matching Architecture Section 10.4 format.
	manifest := Manifest{
		TaskID:       "task-042",
		BaseSnapshot: "abc123def",
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "src/handlers/auth.go", Binary: false},
				{Path: "public/logo.png", Binary: true, SizeBytes: 24576},
			},
			Modified: []FileEntry{
				{Path: "src/routes/api.go", Binary: false},
			},
			Deleted: []string{"src/handlers/old_auth.go"},
			Renamed: []RenameEntry{
				{From: "src/utils/hash.go", To: "src/crypto/hash.go"},
			},
		},
	}

	data, _ := json.MarshalIndent(manifest, "", "  ")
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	parsed, err := ParseManifest(tmpDir)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.TaskID != "task-042" {
		t.Errorf("task_id = %s, want task-042", parsed.TaskID)
	}
	if parsed.BaseSnapshot != "abc123def" {
		t.Errorf("base_snapshot = %s", parsed.BaseSnapshot)
	}
	if len(parsed.Files.Added) != 2 {
		t.Errorf("added count = %d, want 2", len(parsed.Files.Added))
	}
	if len(parsed.Files.Modified) != 1 {
		t.Errorf("modified count = %d, want 1", len(parsed.Files.Modified))
	}
	if len(parsed.Files.Deleted) != 1 {
		t.Errorf("deleted count = %d, want 1", len(parsed.Files.Deleted))
	}
	if len(parsed.Files.Renamed) != 1 {
		t.Errorf("renamed count = %d, want 1", len(parsed.Files.Renamed))
	}
}

func TestValidateManifestAllValid(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-manifest-valid-*")
	defer os.RemoveAll(tmpDir)

	// Create staged files.
	os.MkdirAll(filepath.Join(tmpDir, "src", "handlers"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "src", "handlers", "auth.go"), []byte("package handlers"), 0644)

	manifest := &Manifest{
		TaskID:       "task-001",
		BaseSnapshot: "abc123",
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "src/handlers/auth.go", Binary: false},
			},
		},
	}
	// Write manifest to staging.
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	errors := ValidateManifest(manifest, tmpDir)
	if len(errors) != 0 {
		t.Errorf("expected no errors, got: %v", errors)
	}
}

func TestValidateManifestMissingFile(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-manifest-missing-*")
	defer os.RemoveAll(tmpDir)

	// Don't create the staged file.
	manifest := &Manifest{
		TaskID: "task-001",
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "src/missing.go", Binary: false},
			},
		},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	errors := ValidateManifest(manifest, tmpDir)
	if len(errors) == 0 {
		t.Error("expected error for missing file")
	}
	found := false
	for _, e := range errors {
		if e == "added file not found in staging: src/missing.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'added file not found' error, got: %v", errors)
	}
}

func TestValidateManifestUnlistedFile(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-manifest-unlisted-*")
	defer os.RemoveAll(tmpDir)

	// Create files: one declared, one not.
	os.WriteFile(filepath.Join(tmpDir, "declared.go"), []byte("package main"), 0644)
	os.WriteFile(filepath.Join(tmpDir, "sneaky.go"), []byte("package exploit"), 0644)

	manifest := &Manifest{
		TaskID: "task-001",
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "declared.go", Binary: false},
			},
		},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	errors := ValidateManifest(manifest, tmpDir)
	found := false
	for _, e := range errors {
		if e == "unlisted file found in staging: sneaky.go" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'unlisted file' error, got: %v", errors)
	}
}

func TestValidateManifestBinaryMissingSizeBytes(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-manifest-binary-*")
	defer os.RemoveAll(tmpDir)

	os.WriteFile(filepath.Join(tmpDir, "image.png"), []byte{0x89, 0x50, 0x4E, 0x47}, 0644)

	manifest := &Manifest{
		TaskID: "task-001",
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "image.png", Binary: true, SizeBytes: 0}, // Missing size_bytes
			},
		},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	errors := ValidateManifest(manifest, tmpDir)
	found := false
	for _, e := range errors {
		if e == "binary file missing size_bytes: image.png" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'binary file missing size_bytes' error, got: %v", errors)
	}
}

func TestValidateManifestMissingTaskID(t *testing.T) {
	manifest := &Manifest{
		TaskID: "", // Missing
		Files:  ManifestFiles{},
	}
	tmpDir, _ := os.MkdirTemp("", "axiom-manifest-noid-*")
	defer os.RemoveAll(tmpDir)
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), data, 0644)

	errors := ValidateManifest(manifest, tmpDir)
	if len(errors) == 0 {
		t.Error("expected error for missing task_id")
	}
}

func TestAllPaths(t *testing.T) {
	m := &Manifest{
		Files: ManifestFiles{
			Added:    []FileEntry{{Path: "a.go"}, {Path: "b.go"}},
			Modified: []FileEntry{{Path: "c.go"}},
			Deleted:  []string{"d.go"},
			Renamed:  []RenameEntry{{From: "e.go", To: "f.go"}},
		},
	}
	paths := m.AllPaths()
	if len(paths) != 6 {
		t.Errorf("expected 6 paths, got %d: %v", len(paths), paths)
	}
}

func TestNonBinaryPaths(t *testing.T) {
	m := &Manifest{
		Files: ManifestFiles{
			Added: []FileEntry{
				{Path: "code.go", Binary: false},
				{Path: "image.png", Binary: true, SizeBytes: 100},
			},
			Modified: []FileEntry{
				{Path: "other.go", Binary: false},
			},
		},
	}
	paths := m.NonBinaryPaths()
	if len(paths) != 2 {
		t.Errorf("expected 2 non-binary paths, got %d: %v", len(paths), paths)
	}
}
