package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ethan03805/axiom/internal/security"
)

func TestPromptLoggerWritesWhenEnabled(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-promptlog-*")
	defer os.RemoveAll(tmpDir)

	scanner := security.NewSecretScanner(nil)
	logger := NewPromptLogger(tmpDir, true, scanner)

	entry := &PromptLogEntry{
		TaskID:        "task-001",
		AttemptNumber: 1,
		ModelID:       "anthropic/claude-4-sonnet",
		Prompt:        "Write a function",
		Response:      "func Hello() {}",
		InputTokens:   100,
		OutputTokens:  50,
		CostUSD:       0.01,
		LatencyMs:     500,
		Timestamp:     time.Now(),
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("log: %v", err)
	}

	// Verify file was created.
	path := filepath.Join(tmpDir, "logs", "prompts", "task-001-1.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("prompt log file should exist")
	}

	// Read it back.
	readEntry, err := logger.ReadLog("task-001", 1)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if readEntry.ModelID != "anthropic/claude-4-sonnet" {
		t.Errorf("model_id = %s", readEntry.ModelID)
	}
	if readEntry.Prompt != "Write a function" {
		t.Errorf("prompt = %s", readEntry.Prompt)
	}
}

func TestPromptLoggerSkipsWhenDisabled(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-promptlog-disabled-*")
	defer os.RemoveAll(tmpDir)

	logger := NewPromptLogger(tmpDir, false, nil)

	entry := &PromptLogEntry{
		TaskID:        "task-002",
		AttemptNumber: 1,
		Prompt:        "secret prompt",
	}

	if err := logger.Log(entry); err != nil {
		t.Fatalf("log: %v", err)
	}

	// File should NOT be created.
	path := filepath.Join(tmpDir, "logs", "prompts", "task-002-1.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("prompt log file should NOT exist when logging disabled")
	}
}

func TestPromptLoggerRedactsSecrets(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-promptlog-redact-*")
	defer os.RemoveAll(tmpDir)

	scanner := security.NewSecretScanner(nil)
	logger := NewPromptLogger(tmpDir, true, scanner)

	entry := &PromptLogEntry{
		TaskID:        "task-003",
		AttemptNumber: 1,
		Prompt:        "Use this key: sk-1234567890abcdefghijklmnop",
		Response:      "Here is the key: sk-abcdefghijklmnopqrstuvwx",
		Timestamp:     time.Now(),
	}

	logger.Log(entry)

	readEntry, _ := logger.ReadLog("task-003", 1)

	if strings.Contains(readEntry.Prompt, "sk-1234567890") {
		t.Error("prompt log should not contain raw secret in prompt")
	}
	if strings.Contains(readEntry.Response, "sk-abcdef") {
		t.Error("prompt log should not contain raw secret in response")
	}
	if !strings.Contains(readEntry.Prompt, "[REDACTED]") {
		t.Error("prompt should contain [REDACTED]")
	}
}

func TestPromptLoggerIsEnabled(t *testing.T) {
	logger := NewPromptLogger("/tmp", true, nil)
	if !logger.IsEnabled() {
		t.Error("should be enabled")
	}

	logger2 := NewPromptLogger("/tmp", false, nil)
	if logger2.IsEnabled() {
		t.Error("should not be enabled")
	}
}

func TestCleanStagingDirs(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-clean-staging-*")
	defer os.RemoveAll(tmpDir)

	// Create some staging subdirectories.
	stagingDir := filepath.Join(tmpDir, "containers", "staging")
	os.MkdirAll(filepath.Join(stagingDir, "task-old-1"), 0755)
	os.MkdirAll(filepath.Join(stagingDir, "task-old-2"), 0755)
	os.WriteFile(filepath.Join(stagingDir, "task-old-1", "output.go"), []byte("stale"), 0644)

	ipcDir := filepath.Join(tmpDir, "containers", "ipc")
	os.MkdirAll(filepath.Join(ipcDir, "task-old-1", "input"), 0755)

	if err := CleanStagingDirs(tmpDir); err != nil {
		t.Fatalf("clean: %v", err)
	}

	// Staging subdirs should be removed.
	entries, _ := os.ReadDir(stagingDir)
	if len(entries) != 0 {
		t.Errorf("expected 0 staging entries after cleanup, got %d", len(entries))
	}

	// IPC subdirs should be removed.
	entries, _ = os.ReadDir(ipcDir)
	if len(entries) != 0 {
		t.Errorf("expected 0 ipc entries after cleanup, got %d", len(entries))
	}
}
