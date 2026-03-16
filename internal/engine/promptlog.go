package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ethan03805/axiom/internal/security"
)

// PromptLogEntry represents a logged prompt + response pair.
// See Architecture Section 31.3.
type PromptLogEntry struct {
	TaskID        string        `json:"task_id"`
	AttemptNumber int           `json:"attempt_number"`
	ModelID       string        `json:"model_id"`
	Prompt        string        `json:"prompt,omitempty"`   // Only when log_prompts=true
	Response      string        `json:"response,omitempty"` // Only when log_prompts=true
	InputTokens   int           `json:"input_tokens"`
	OutputTokens  int           `json:"output_tokens"`
	CostUSD       float64       `json:"cost_usd"`
	LatencyMs     int64         `json:"latency_ms"`
	Timestamp     time.Time     `json:"timestamp"`
}

// PromptLogger handles writing prompt logs with configurable verbosity.
// When log_prompts is true, full prompts and responses are logged.
// When false, only token counts and cost are recorded.
//
// See Architecture Section 31.
type PromptLogger struct {
	logsDir     string // .axiom/logs/prompts/
	logPrompts  bool
	scanner     *security.SecretScanner
}

// NewPromptLogger creates a PromptLogger.
func NewPromptLogger(axiomDir string, logPrompts bool, scanner *security.SecretScanner) *PromptLogger {
	logsDir := filepath.Join(axiomDir, "logs", "prompts")
	os.MkdirAll(logsDir, 0755)

	return &PromptLogger{
		logsDir:    logsDir,
		logPrompts: logPrompts,
		scanner:    scanner,
	}
}

// Log records a prompt/response interaction.
// When log_prompts is enabled, writes full content to a JSON file
// with secret redaction applied.
// See Architecture Section 31.3 and Section 29.4 point 5.
func (pl *PromptLogger) Log(entry *PromptLogEntry) error {
	if !pl.logPrompts {
		// When logging is disabled, only token counts are tracked
		// (via task_attempts table, not here).
		return nil
	}

	// Apply secret redaction to prompt and response.
	// See Architecture Section 29.4 point 5.
	if pl.scanner != nil {
		entry.Prompt = pl.scanner.RedactForPromptLog(entry.Prompt)
		entry.Response = pl.scanner.RedactForPromptLog(entry.Response)
	}

	// Write to .axiom/logs/prompts/<task-id>-<attempt>.json
	filename := fmt.Sprintf("%s-%d.json", entry.TaskID, entry.AttemptNumber)
	path := filepath.Join(pl.logsDir, filename)

	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal prompt log: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write prompt log: %w", err)
	}

	return nil
}

// IsEnabled returns whether full prompt logging is enabled.
func (pl *PromptLogger) IsEnabled() bool {
	return pl.logPrompts
}

// ReadLog reads a previously written prompt log entry.
func (pl *PromptLogger) ReadLog(taskID string, attempt int) (*PromptLogEntry, error) {
	filename := fmt.Sprintf("%s-%d.json", taskID, attempt)
	path := filepath.Join(pl.logsDir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read prompt log: %w", err)
	}

	var entry PromptLogEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("unmarshal prompt log: %w", err)
	}

	return &entry, nil
}

// CleanStagingDirs removes leftover staging files from a crashed session.
// See Architecture Section 22.3 step 4.
func CleanStagingDirs(axiomDir string) error {
	dirs := []string{
		filepath.Join(axiomDir, "containers", "staging"),
		filepath.Join(axiomDir, "containers", "ipc"),
		filepath.Join(axiomDir, "containers", "specs"),
		filepath.Join(axiomDir, "validation"),
	}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // Directory may not exist
		}
		for _, entry := range entries {
			if entry.IsDir() {
				os.RemoveAll(filepath.Join(dir, entry.Name()))
			}
		}
	}

	return nil
}
