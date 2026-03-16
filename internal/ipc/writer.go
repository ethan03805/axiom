package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Writer handles writing IPC messages to container input directories.
// Messages are written as JSON files with the naming convention:
//
//	<message-type>-<sequence-number>.json
//
// The Writer tracks sequence numbers per task to ensure ordered delivery.
// See Architecture Section 20.3.
type Writer struct {
	baseDir string // e.g. ".axiom/containers/ipc"
	mu      sync.Mutex
	seqNums map[string]int // taskID -> next sequence number
}

// NewWriter creates a Writer rooted at the given base IPC directory.
// The baseDir should be the path to .axiom/containers/ipc/.
func NewWriter(baseDir string) *Writer {
	return &Writer{
		baseDir: baseDir,
		seqNums: make(map[string]int),
	}
}

// Send writes an IPC message to the specified task's input directory.
// The message is serialized to JSON and written atomically (write to temp
// file, then rename) to prevent the watcher from reading partial files.
func (w *Writer) Send(taskID string, msg interface{}) error {
	data, err := json.MarshalIndent(msg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// Get the message type for the filename.
	msgType, err := ParseMessageType(data)
	if err != nil {
		return fmt.Errorf("determine message type: %w", err)
	}

	// Get next sequence number for this task.
	w.mu.Lock()
	seq := w.seqNums[taskID]
	w.seqNums[taskID] = seq + 1
	w.mu.Unlock()

	// Build the target file path.
	inputDir := filepath.Join(w.baseDir, taskID, "input")
	filename := fmt.Sprintf("%s-%04d.json", msgType, seq)
	targetPath := filepath.Join(inputDir, filename)

	// Atomic write: write to a temp file, then rename.
	// This prevents the container's watcher from reading a partial file.
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename to %s: %w", targetPath, err)
	}

	return nil
}

// StreamWriter handles writing chunked streaming responses as sequential
// numbered files. Used by the Inference Broker for streaming model responses.
// See Architecture Section 19.5 (Streaming).
type StreamWriter struct {
	baseDir    string
	taskID     string
	chunkIndex int
	mu         sync.Mutex
}

// NewStreamWriter creates a StreamWriter for the given task's input directory.
func NewStreamWriter(baseDir, taskID string) *StreamWriter {
	return &StreamWriter{
		baseDir: baseDir,
		taskID:  taskID,
	}
}

// WriteChunk writes a single streaming chunk to the task's input directory.
// Chunk files are named: response-001.json, response-002.json, etc.
func (sw *StreamWriter) WriteChunk(chunk *InferenceStreamChunk) error {
	sw.mu.Lock()
	sw.chunkIndex++
	idx := sw.chunkIndex
	sw.mu.Unlock()

	chunk.ChunkIndex = idx

	data, err := json.MarshalIndent(chunk, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal stream chunk: %w", err)
	}

	inputDir := filepath.Join(sw.baseDir, sw.taskID, "input")
	filename := fmt.Sprintf("response-%03d.json", idx)
	targetPath := filepath.Join(inputDir, filename)

	// Atomic write.
	tmpPath := targetPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write stream chunk: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename stream chunk: %w", err)
	}

	return nil
}

// Reset resets the chunk counter. Called when starting a new streaming session.
func (sw *StreamWriter) Reset() {
	sw.mu.Lock()
	sw.chunkIndex = 0
	sw.mu.Unlock()
}
