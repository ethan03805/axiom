package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestWriterSend verifies that the Writer correctly writes JSON files
// with the expected naming convention and content.
func TestWriterSend(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-writer-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create the input directory structure.
	inputDir := filepath.Join(tmpDir, "task-001", "input")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("create input dir: %v", err)
	}

	writer := NewWriter(tmpDir)

	// Send a TaskSpec message.
	msg := &TaskSpecMessage{
		Header: Header{Type: TypeTaskSpec, TaskID: "task-001"},
		Spec:   "# TaskSpec: task-001\n\n## Objective\nBuild something.",
	}
	if err := writer.Send("task-001", msg); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Verify the file was created with correct name.
	expectedFile := filepath.Join(inputDir, "task_spec-0000.json")
	data, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	// Verify the content is valid JSON with correct fields.
	var parsed TaskSpecMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Type != TypeTaskSpec {
		t.Errorf("type = %s, want %s", parsed.Type, TypeTaskSpec)
	}
	if parsed.TaskID != "task-001" {
		t.Errorf("task_id = %s, want task-001", parsed.TaskID)
	}
	if parsed.Spec == "" {
		t.Error("spec should not be empty")
	}
}

// TestWriterSequencing verifies that multiple messages to the same task
// get incrementing sequence numbers.
func TestWriterSequencing(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-seq-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	inputDir := filepath.Join(tmpDir, "task-seq", "input")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("create input dir: %v", err)
	}

	writer := NewWriter(tmpDir)

	// Send 3 messages.
	for i := 0; i < 3; i++ {
		msg := &ShutdownMessage{
			Header: Header{Type: TypeShutdown, TaskID: "task-seq"},
			Reason: "test",
		}
		if err := writer.Send("task-seq", msg); err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	// Verify sequential file names.
	for i, expected := range []string{"shutdown-0000.json", "shutdown-0001.json", "shutdown-0002.json"} {
		path := filepath.Join(inputDir, expected)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %d: %s", i, expected)
		}
	}
}

// TestStreamWriter verifies chunked streaming response files.
func TestStreamWriter(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-stream-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	inputDir := filepath.Join(tmpDir, "task-stream", "input")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("create input dir: %v", err)
	}

	sw := NewStreamWriter(tmpDir, "task-stream")

	// Write 3 chunks.
	for i := 0; i < 3; i++ {
		chunk := &InferenceStreamChunk{
			Header:  Header{Type: TypeInferenceResponse, TaskID: "task-stream"},
			Content: "partial content",
			Done:    i == 2,
		}
		if err := sw.WriteChunk(chunk); err != nil {
			t.Fatalf("write chunk %d: %v", i, err)
		}
	}

	// Verify sequential chunk files.
	for i, expected := range []string{"response-001.json", "response-002.json", "response-003.json"} {
		path := filepath.Join(inputDir, expected)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read chunk %d: %v", i, err)
		}

		var chunk InferenceStreamChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			t.Fatalf("unmarshal chunk %d: %v", i, err)
		}
		if chunk.ChunkIndex != i+1 {
			t.Errorf("chunk %d: index = %d, want %d", i, chunk.ChunkIndex, i+1)
		}
		if i == 2 && !chunk.Done {
			t.Error("last chunk should have done=true")
		}
	}
}

// TestWatcherDetectsNewFiles verifies that the Watcher detects new JSON files
// written to a container's output directory and dispatches them to the handler.
func TestWatcherDetectsNewFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-watcher-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var mu sync.Mutex
	var received []string

	handler := func(taskID string, msg interface{}, raw []byte) {
		mu.Lock()
		defer mu.Unlock()
		received = append(received, taskID)
	}

	watcher, err := NewWatcher(tmpDir, handler)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	defer watcher.Stop()

	// Start watching a task.
	if err := watcher.WatchTask("task-watch"); err != nil {
		t.Fatalf("watch task: %v", err)
	}

	// Write a message to the output directory (simulating a container writing).
	outputDir := filepath.Join(tmpDir, "task-watch", "output")
	msg := &TaskOutputMessage{
		Header:       Header{Type: TypeTaskOutput, TaskID: "task-watch"},
		BaseSnapshot: "abc123",
		Manifest:     json.RawMessage(`{"files":{}}`),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Write atomically (tmp + rename) as the container would.
	tmpFile := filepath.Join(outputDir, "task_output-0000.json.tmp")
	finalFile := filepath.Join(outputDir, "task_output-0000.json")
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmpFile, finalFile); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Wait for the watcher to detect the file.
	// fsnotify should detect within 100ms; allow up to 2s for CI environments.
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		count := len(received)
		mu.Unlock()
		if count > 0 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("watcher did not detect file within 2s")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}

	mu.Lock()
	if len(received) != 1 {
		t.Errorf("expected 1 message, got %d", len(received))
	}
	if received[0] != "task-watch" {
		t.Errorf("expected task-watch, got %s", received[0])
	}
	mu.Unlock()
}

// TestWatcherIgnoresTmpFiles verifies that .tmp files are not processed.
func TestWatcherIgnoresTmpFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-tmp-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	var mu sync.Mutex
	var received int

	handler := func(taskID string, msg interface{}, raw []byte) {
		mu.Lock()
		received++
		mu.Unlock()
	}

	watcher, err := NewWatcher(tmpDir, handler)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	defer watcher.Stop()

	if err := watcher.WatchTask("task-tmp"); err != nil {
		t.Fatalf("watch: %v", err)
	}

	// Write a .tmp file (should be ignored).
	outputDir := filepath.Join(tmpDir, "task-tmp", "output")
	tmpFile := filepath.Join(outputDir, "task_output-0000.json.tmp")
	if err := os.WriteFile(tmpFile, []byte(`{"type":"task_output"}`), 0644); err != nil {
		t.Fatalf("write tmp: %v", err)
	}

	// Wait a bit and verify nothing was received.
	time.Sleep(500 * time.Millisecond)
	mu.Lock()
	if received != 0 {
		t.Errorf("expected 0 messages for .tmp file, got %d", received)
	}
	mu.Unlock()
}

// TestWatcherUnwatch verifies that unwatching a task stops processing.
func TestWatcherUnwatch(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-unwatch-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	handler := func(taskID string, msg interface{}, raw []byte) {}

	watcher, err := NewWatcher(tmpDir, handler)
	if err != nil {
		t.Fatalf("create watcher: %v", err)
	}
	defer watcher.Stop()

	if err := watcher.WatchTask("task-uw"); err != nil {
		t.Fatalf("watch: %v", err)
	}

	tasks := watcher.WatchedTasks()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 watched task, got %d", len(tasks))
	}

	watcher.UnwatchTask("task-uw")

	tasks = watcher.WatchedTasks()
	if len(tasks) != 0 {
		t.Errorf("expected 0 watched tasks after unwatch, got %d", len(tasks))
	}
}

// TestDispatcherRouting verifies that the Dispatcher correctly routes
// messages to registered handlers and writes responses.
func TestDispatcherRouting(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-dispatch-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create input directory for response writing.
	inputDir := filepath.Join(tmpDir, "task-disp", "input")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("create input dir: %v", err)
	}

	writer := NewWriter(tmpDir)
	dispatcher := NewDispatcher(writer)

	var handledType MessageType
	dispatcher.Register(TypeInferenceRequest, func(taskID string, msg interface{}, raw []byte) (interface{}, error) {
		handledType = TypeInferenceRequest
		return &InferenceResponseMessage{
			Header:       Header{Type: TypeInferenceResponse, TaskID: taskID},
			Content:      "response",
			FinishReason: "stop",
		}, nil
	})

	// Dispatch an inference request.
	raw := `{"type":"inference_request","task_id":"task-disp","model_id":"test","messages":[],"max_tokens":100}`
	msg, _ := ParseMessage([]byte(raw))
	dispatcher.Dispatch("task-disp", msg, []byte(raw))

	if handledType != TypeInferenceRequest {
		t.Errorf("expected handler called for %s, got %s", TypeInferenceRequest, handledType)
	}

	// Verify response was written to input directory.
	entries, err := os.ReadDir(inputDir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 response file, got %d", len(entries))
	}

	// Verify response content.
	data, err := os.ReadFile(filepath.Join(inputDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	parsed, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}
	resp, ok := parsed.(*InferenceResponseMessage)
	if !ok {
		t.Fatalf("expected InferenceResponseMessage, got %T", parsed)
	}
	if resp.Content != "response" {
		t.Errorf("content = %s, want 'response'", resp.Content)
	}
}

// TestDispatcherFallback verifies the fallback handler for unregistered types.
func TestDispatcherFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-fallback-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	writer := NewWriter(tmpDir)
	dispatcher := NewDispatcher(writer)

	var fallbackCalled bool
	dispatcher.SetFallback(func(taskID string, msg interface{}, raw []byte) (interface{}, error) {
		fallbackCalled = true
		return nil, nil
	})

	// Dispatch a type with no specific handler.
	raw := `{"type":"shutdown","task_id":"task-fb","reason":"test"}`
	msg, _ := ParseMessage([]byte(raw))
	dispatcher.Dispatch("task-fb", msg, []byte(raw))

	if !fallbackCalled {
		t.Error("expected fallback handler to be called")
	}
}

// TestWriterReadRoundTrip verifies a complete write-read cycle.
func TestWriterReadRoundTrip(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "axiom-ipc-roundtrip-*")
	if err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	inputDir := filepath.Join(tmpDir, "task-rt", "input")
	if err := os.MkdirAll(inputDir, 0755); err != nil {
		t.Fatalf("create dir: %v", err)
	}

	writer := NewWriter(tmpDir)

	// Write a scope expansion response.
	original := &ScopeExpansionResponseMessage{
		Header:        Header{Type: TypeScopeExpansionResponse, TaskID: "task-rt"},
		Status:        "approved",
		ExpandedFiles: []string{"src/main.go", "src/utils.go"},
		LocksAcquired: true,
	}
	if err := writer.Send("task-rt", original); err != nil {
		t.Fatalf("send: %v", err)
	}

	// Read the written file.
	filePath := filepath.Join(inputDir, "scope_expansion_response-0000.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Parse it back.
	parsed, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	resp, ok := parsed.(*ScopeExpansionResponseMessage)
	if !ok {
		t.Fatalf("expected *ScopeExpansionResponseMessage, got %T", parsed)
	}
	if resp.Status != "approved" {
		t.Errorf("status = %s, want approved", resp.Status)
	}
	if len(resp.ExpandedFiles) != 2 {
		t.Errorf("expanded_files count = %d, want 2", len(resp.ExpandedFiles))
	}
	if !resp.LocksAcquired {
		t.Error("locks_acquired should be true")
	}
}
