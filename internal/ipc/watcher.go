package ipc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// MessageHandler is called when a new IPC message is received from a container.
// The handler receives the task ID, the parsed message, and the raw JSON bytes.
type MessageHandler func(taskID string, msg interface{}, raw []byte)

// Watcher monitors container IPC output directories for new messages using
// fsnotify (inotify on Linux). Falls back to polling if fsnotify is unavailable.
// See Architecture Section 20.3.
type Watcher struct {
	baseDir  string // .axiom/containers/ipc
	handler  MessageHandler
	fsWatcher *fsnotify.Watcher
	usePoll  bool // true if fsnotify is unavailable

	mu       sync.Mutex
	watched  map[string]bool // taskID -> watching
	stopCh   chan struct{}
	stopped  bool
}

// NewWatcher creates a Watcher that monitors the given base IPC directory.
// It attempts to use fsnotify (inotify) for event-driven notification.
// If fsnotify is unavailable, it falls back to 1-second polling.
func NewWatcher(baseDir string, handler MessageHandler) (*Watcher, error) {
	w := &Watcher{
		baseDir: baseDir,
		handler: handler,
		watched: make(map[string]bool),
		stopCh:  make(chan struct{}),
	}

	// Try to create fsnotify watcher.
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		// Fallback to polling mode.
		w.usePoll = true
		return w, nil
	}
	w.fsWatcher = fsw

	// Start the fsnotify event loop.
	go w.fsnotifyLoop()

	return w, nil
}

// WatchTask begins monitoring the IPC output directory for the given task.
// New JSON files written to .axiom/containers/ipc/<taskID>/output/ will be
// read, parsed, and dispatched to the handler.
func (w *Watcher) WatchTask(taskID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.watched[taskID] {
		return nil // Already watching
	}

	outputDir := filepath.Join(w.baseDir, taskID, "output")

	// Ensure the directory exists.
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output dir for %s: %w", taskID, err)
	}

	if w.fsWatcher != nil {
		if err := w.fsWatcher.Add(outputDir); err != nil {
			return fmt.Errorf("watch %s: %w", outputDir, err)
		}
	}

	w.watched[taskID] = true

	// If polling mode, start a poll goroutine for this task.
	if w.usePoll {
		go w.pollLoop(taskID, outputDir)
	}

	return nil
}

// UnwatchTask stops monitoring the IPC output directory for the given task.
func (w *Watcher) UnwatchTask(taskID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.watched[taskID] {
		return
	}

	outputDir := filepath.Join(w.baseDir, taskID, "output")

	if w.fsWatcher != nil {
		_ = w.fsWatcher.Remove(outputDir)
	}

	delete(w.watched, taskID)
}

// Stop shuts down the watcher and releases all resources.
func (w *Watcher) Stop() error {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return nil
	}
	w.stopped = true
	close(w.stopCh)
	w.mu.Unlock()

	if w.fsWatcher != nil {
		return w.fsWatcher.Close()
	}
	return nil
}

// fsnotifyLoop processes filesystem events from fsnotify.
// When a new JSON file is created in a watched output directory,
// it reads the file, parses the message, and dispatches to the handler.
func (w *Watcher) fsnotifyLoop() {
	for {
		select {
		case event, ok := <-w.fsWatcher.Events:
			if !ok {
				return
			}
			// We only care about Create events for .json files.
			if !event.Has(fsnotify.Create) {
				continue
			}
			if !strings.HasSuffix(event.Name, ".json") {
				continue
			}
			// Skip temp files (atomic write pattern).
			if strings.HasSuffix(event.Name, ".tmp") {
				continue
			}

			// Extract task ID from the path.
			// Path format: <baseDir>/<taskID>/output/<filename>.json
			taskID := w.taskIDFromPath(event.Name)
			if taskID == "" {
				continue
			}

			// Small delay to ensure the file is fully written
			// (rename from .tmp should be atomic, but be safe).
			time.Sleep(5 * time.Millisecond)

			w.processFile(taskID, event.Name)

		case err, ok := <-w.fsWatcher.Errors:
			if !ok {
				return
			}
			// Log errors but continue watching. The watcher should be
			// resilient to transient filesystem errors.
			_ = err

		case <-w.stopCh:
			return
		}
	}
}

// pollLoop implements the fallback polling strategy when fsnotify is unavailable.
// Polls the task's output directory every second for new JSON files.
// See Architecture Section 20.3: fallback polling at 1-second intervals.
func (w *Watcher) pollLoop(taskID, outputDir string) {
	seen := make(map[string]bool)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.mu.Lock()
			watching := w.watched[taskID]
			w.mu.Unlock()
			if !watching {
				return
			}

			entries, err := os.ReadDir(outputDir)
			if err != nil {
				continue
			}

			for _, entry := range entries {
				name := entry.Name()
				if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".tmp") {
					continue
				}
				if seen[name] {
					continue
				}
				seen[name] = true

				fullPath := filepath.Join(outputDir, name)
				w.processFile(taskID, fullPath)
			}
		}
	}
}

// processFile reads a JSON file, parses the IPC message, and dispatches
// to the registered handler.
func (w *Watcher) processFile(taskID, filePath string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return // File may have been cleaned up already
	}

	msg, err := ParseMessage(data)
	if err != nil {
		return // Malformed message; skip
	}

	w.handler(taskID, msg, data)
}

// taskIDFromPath extracts the task ID from an IPC file path.
// Expected format: <baseDir>/<taskID>/output/<filename>.json
func (w *Watcher) taskIDFromPath(path string) string {
	// Get the relative path from baseDir.
	rel, err := filepath.Rel(w.baseDir, path)
	if err != nil {
		return ""
	}

	// Split: taskID/output/filename.json
	parts := strings.SplitN(rel, string(filepath.Separator), 3)
	if len(parts) < 3 {
		return ""
	}
	return parts[0]
}

// WatchedTasks returns the list of currently watched task IDs.
func (w *Watcher) WatchedTasks() []string {
	w.mu.Lock()
	defer w.mu.Unlock()

	tasks := make([]string, 0, len(w.watched))
	for id := range w.watched {
		tasks = append(tasks, id)
	}
	return tasks
}

// IsPolling returns true if the watcher is using fallback polling
// instead of fsnotify.
func (w *Watcher) IsPolling() bool {
	return w.usePoll
}
