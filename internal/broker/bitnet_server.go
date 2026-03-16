package broker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// BitNetServerConfig holds configuration for the local BitNet inference server.
// See Architecture Section 19.6.
type BitNetServerConfig struct {
	Enabled               bool
	Host                  string
	Port                  int
	MaxConcurrentRequests int
	CPUThreads            int
	ModelsDir             string // Path to model weights (default ~/.axiom/bitnet/models/)
	BinaryPath            string // Path to bitnet.cpp binary
}

// BitNetServer manages the lifecycle of the local BitNet inference server.
// BitNet provides free, zero-latency inference for trivial tasks using
// 1-bit quantized models (Falcon3).
//
// See Architecture Section 19.
type BitNetServer struct {
	config BitNetServerConfig

	mu        sync.Mutex
	cmd       *exec.Cmd
	running   bool
	startedAt time.Time
	activeReq int
}

// NewBitNetServer creates a BitNet server manager.
func NewBitNetServer(config BitNetServerConfig) *BitNetServer {
	if config.Host == "" {
		config.Host = "localhost"
	}
	if config.Port == 0 {
		config.Port = 3002
	}
	if config.CPUThreads == 0 {
		config.CPUThreads = 4
	}
	if config.MaxConcurrentRequests == 0 {
		config.MaxConcurrentRequests = 4
	}
	if config.ModelsDir == "" {
		home, _ := os.UserHomeDir()
		config.ModelsDir = filepath.Join(home, ".axiom", "bitnet", "models")
	}
	return &BitNetServer{config: config}
}

// Start launches the BitNet server process.
// If model weights are not present, returns an error indicating first-run setup is needed.
// See Architecture Section 19.10.
func (s *BitNetServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("BitNet server is already running")
	}

	if !s.config.Enabled {
		return fmt.Errorf("BitNet is disabled in configuration")
	}

	// Check for model weights.
	if !s.hasModelWeights() {
		return &FirstRunError{
			ModelsDir: s.config.ModelsDir,
			Message:   "No model weights found. Run 'axiom bitnet start' to download Falcon3 1-bit weights.",
		}
	}

	// Build the server command.
	// In production, this invokes bitnet.cpp with the appropriate flags.
	// The server exposes an OpenAI-compatible API at the configured port.
	args := []string{
		"--host", s.config.Host,
		"--port", fmt.Sprintf("%d", s.config.Port),
		"--threads", fmt.Sprintf("%d", s.config.CPUThreads),
		"--ctx-size", "4096",
	}

	// Find the model file.
	modelPath := s.findModelPath()
	if modelPath != "" {
		args = append(args, "--model", modelPath)
	}

	binaryPath := s.config.BinaryPath
	if binaryPath == "" {
		binaryPath = "bitnet-server" // Assume on PATH
	}

	s.cmd = exec.Command(binaryPath, args...)
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start BitNet server: %w", err)
	}

	s.running = true
	s.startedAt = time.Now()

	// Wait for server to be ready.
	if err := s.waitForReady(10 * time.Second); err != nil {
		s.Stop()
		return fmt.Errorf("BitNet server failed to become ready: %w", err)
	}

	return nil
}

// Stop shuts down the BitNet server.
func (s *BitNetServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Signal(os.Interrupt)
		// Give it 5 seconds to shut down gracefully.
		done := make(chan error, 1)
		go func() { done <- s.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			s.cmd.Process.Kill()
		}
	}

	s.running = false
	s.cmd = nil
	return nil
}

// Status returns the current server status.
func (s *BitNetServer) Status() *BitNetStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := &BitNetStatus{
		Enabled:        s.config.Enabled,
		Running:        s.running,
		Host:           s.config.Host,
		Port:           s.config.Port,
		CPUThreads:     s.config.CPUThreads,
		HasWeights:     s.hasModelWeights(),
		ActiveRequests: s.activeReq,
	}

	if s.running {
		status.Uptime = time.Since(s.startedAt)
	}

	return status
}

// BitNetStatus holds the current server status information.
type BitNetStatus struct {
	Enabled        bool
	Running        bool
	Host           string
	Port           int
	CPUThreads     int
	HasWeights     bool
	ActiveRequests int
	Uptime         time.Duration
	MemoryUsageMB  float64
}

// IsRunning returns true if the server is currently running.
func (s *BitNetServer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// GetResourceUsage returns current CPU and memory usage estimates.
// See Architecture Section 19.6 and BUILD_PLAN step 14.4.
func (s *BitNetServer) GetResourceUsage() *ResourceUsage {
	s.mu.Lock()
	defer s.mu.Unlock()

	usage := &ResourceUsage{
		CPUThreads:     s.config.CPUThreads,
		TotalCPUs:      runtime.NumCPU(),
		ActiveRequests: s.activeReq,
		MaxConcurrent:  s.config.MaxConcurrentRequests,
	}

	if usage.TotalCPUs > 0 {
		usage.CPUPercent = float64(s.config.CPUThreads) / float64(usage.TotalCPUs) * 100
	}

	return usage
}

// ResourceUsage holds resource consumption data for BitNet.
type ResourceUsage struct {
	CPUThreads     int
	TotalCPUs      int
	CPUPercent     float64
	ActiveRequests int
	MaxConcurrent  int
	IsOverloaded   bool
}

// CheckOverload returns true if the combined load of BitNet and containers
// exceeds system capacity. See Architecture Section 19.6.
func CheckOverload(bitnetThreads, containerCPUs, totalCPUs int) bool {
	totalUsed := float64(bitnetThreads) + float64(containerCPUs)
	return totalUsed > float64(totalCPUs)*0.9 // Warn at 90% utilization
}

// FirstRunError indicates that model weights need to be downloaded.
type FirstRunError struct {
	ModelsDir string
	Message   string
}

func (e *FirstRunError) Error() string {
	return e.Message
}

// NeedsFirstRun returns true if the error indicates first-run setup is needed.
func NeedsFirstRun(err error) bool {
	_, ok := err.(*FirstRunError)
	return ok
}

// SetupModelsDir creates the models directory and returns the path.
// See Architecture Section 19.9.
func (s *BitNetServer) SetupModelsDir() (string, error) {
	if err := os.MkdirAll(s.config.ModelsDir, 0755); err != nil {
		return "", fmt.Errorf("create models dir: %w", err)
	}
	return s.config.ModelsDir, nil
}

// hasModelWeights checks if any model weight files exist in the models directory.
func (s *BitNetServer) hasModelWeights() bool {
	entries, err := os.ReadDir(s.config.ModelsDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && (filepath.Ext(e.Name()) == ".gguf" || filepath.Ext(e.Name()) == ".bin") {
			return true
		}
	}
	return false
}

// findModelPath returns the path to the first model weight file found.
func (s *BitNetServer) findModelPath() string {
	entries, err := os.ReadDir(s.config.ModelsDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.IsDir() && (ext == ".gguf" || ext == ".bin") {
			return filepath.Join(s.config.ModelsDir, e.Name())
		}
	}
	return ""
}

// waitForReady polls the server's health endpoint until it responds or times out.
func (s *BitNetServer) waitForReady(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	url := fmt.Sprintf("http://%s:%d/v1/models", s.config.Host, s.config.Port)
	client := &http.Client{Timeout: 1 * time.Second}

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server at %s", url)
		default:
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
			time.Sleep(500 * time.Millisecond)
		}
	}
}

// TrackRequest increments the active request counter.
func (s *BitNetServer) TrackRequest() {
	s.mu.Lock()
	s.activeReq++
	s.mu.Unlock()
}

// UntrackRequest decrements the active request counter.
func (s *BitNetServer) UntrackRequest() {
	s.mu.Lock()
	if s.activeReq > 0 {
		s.activeReq--
	}
	s.mu.Unlock()
}

// GrammarForJSON returns a GBNF grammar that constrains output to valid JSON.
// See Architecture Section 19.3.
func GrammarForJSON() string {
	return `root   ::= object
value  ::= object | array | string | number | "true" | "false" | "null"
object ::= "{" ws (string ":" ws value ("," ws string ":" ws value)*)? ws "}"
array  ::= "[" ws (value ("," ws value)*)? ws "]"
string ::= "\"" ([^"\\] | "\\" .)* "\""
number ::= "-"? [0-9]+ ("." [0-9]+)?
ws     ::= [ \t\n]*`
}

// GrammarForGoCode returns a basic GBNF grammar for Go source code structure.
func GrammarForGoCode() string {
	return `root ::= package-decl imports? decls
package-decl ::= "package " [a-z]+ "\n"
imports ::= "import (" [^)]* ")\n"
decls ::= (func-decl | type-decl | var-decl)*
func-decl ::= "func " [^\n]+ "{" [^}]* "}\n"
type-decl ::= "type " [^\n]+ "\n"
var-decl ::= "var " [^\n]+ "\n"`
}
