package broker

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
// When the vendored BitNet directory is available, the server is started via
// run_inference_server.py which handles correct initialization of the 1.58-bit
// kernels. Direct binary invocation is used as a fallback when Python is unavailable.
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
			Message:   "Model weights not found. Run 'axiom bitnet start' interactively to download.",
		}
	}

	// Find the model file.
	modelPath := s.ResolveModelPath()
	if modelPath == "" {
		return fmt.Errorf("no model file found in %s or vendored BitNet directory", s.config.ModelsDir)
	}

	bitnetDir := s.resolveBitNetDir()

	// Prefer the Python wrapper (run_inference_server.py) for reliable startup.
	// The wrapper handles correct initialization of the 1.58-bit kernels and
	// ensures proper library loading. Direct binary invocation crashes during
	// inference with the custom kernel builds.
	if bitnetDir != "" {
		if err := s.startViaPythonWrapper(bitnetDir, modelPath); err == nil {
			return nil
		}
		// Fall through to direct binary invocation if Python wrapper fails.
		fmt.Fprintf(os.Stderr, "warning: Python wrapper unavailable, falling back to direct binary invocation\n")
	}

	// Fallback: direct binary invocation.
	if err := s.startDirectBinary(bitnetDir, modelPath); err != nil {
		return err
	}

	return nil
}

// startViaPythonWrapper starts the BitNet server using run_inference_server.py.
// This is the preferred method as it handles correct initialization of the
// 1.58-bit kernels that the direct binary invocation fails to do properly.
func (s *BitNetServer) startViaPythonWrapper(bitnetDir, modelPath string) error {
	wrapperScript := filepath.Join(bitnetDir, "run_inference_server.py")
	if _, err := os.Stat(wrapperScript); err != nil {
		return fmt.Errorf("run_inference_server.py not found: %w", err)
	}

	// Find Python in the BitNet venv, fall back to system python3.
	pythonPath := filepath.Join(bitnetDir, ".venv", "bin", "python3")
	if _, err := os.Stat(pythonPath); err != nil {
		pythonPath = "python3"
		if _, err := exec.LookPath(pythonPath); err != nil {
			return fmt.Errorf("python3 not found")
		}
	}

	// Make the model path relative to bitnetDir if it's inside it,
	// since run_inference_server.py runs from the BitNet directory.
	relModelPath := modelPath
	if rel, err := filepath.Rel(bitnetDir, modelPath); err == nil && !strings.HasPrefix(rel, "..") {
		relModelPath = rel
	}

	args := []string{
		"run_inference_server.py",
		"-m", relModelPath,
		"-t", fmt.Sprintf("%d", s.config.CPUThreads),
		"-c", "2048",
		"-n", "4096",
		"--host", s.config.Host,
		"--port", fmt.Sprintf("%d", s.config.Port),
	}

	s.cmd = exec.Command(pythonPath, args...)
	s.cmd.Dir = bitnetDir
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start BitNet server via Python wrapper: %w", err)
	}

	s.running = true
	s.startedAt = time.Now()

	if err := s.waitForReady(30 * time.Second); err != nil {
		s.stopInternal()
		return fmt.Errorf("BitNet server (Python wrapper) failed to become ready: %w", err)
	}

	return nil
}

// startDirectBinary starts the BitNet server by invoking llama-server directly.
// This is a fallback when the Python wrapper is unavailable.
func (s *BitNetServer) startDirectBinary(bitnetDir, modelPath string) error {
	args := []string{
		"--host", s.config.Host,
		"--port", fmt.Sprintf("%d", s.config.Port),
		"--threads", fmt.Sprintf("%d", s.config.CPUThreads),
		"--ctx-size", "2048",
		"--model", modelPath,
		"-ngl", "0",
		"-n", "4096",
		"-cb",
	}

	binaryPath := s.resolveBinaryPath()

	s.cmd = exec.Command(binaryPath, args...)
	if bitnetDir != "" {
		s.cmd.Dir = bitnetDir
	}
	s.cmd.Stdout = os.Stdout
	s.cmd.Stderr = os.Stderr

	if err := s.cmd.Start(); err != nil {
		return fmt.Errorf("start BitNet server: %w", err)
	}

	s.running = true
	s.startedAt = time.Now()

	if err := s.waitForReady(30 * time.Second); err != nil {
		s.stopInternal()
		return fmt.Errorf("BitNet server failed to become ready: %w", err)
	}

	return nil
}

// Stop shuts down the BitNet server.
func (s *BitNetServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopInternal()
}

// stopInternal shuts down the BitNet server without acquiring the mutex.
// Used by Start() when cleanup is needed while the lock is already held.
func (s *BitNetServer) stopInternal() error {
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

// EnsureWeights checks if model weight files exist in the models directory
// or in the vendored BitNet models directory.
// Returns (true, nil) if weights are present, (false, nil) if absent,
// or (false, error) on filesystem errors.
// See Architecture Section 19.9.
func (s *BitNetServer) EnsureWeights() (bool, error) {
	// Check user models dir.
	entries, err := os.ReadDir(s.config.ModelsDir)
	if err == nil {
		for _, e := range entries {
			ext := filepath.Ext(e.Name())
			if !e.IsDir() && (ext == ".gguf" || ext == ".bin") {
				return true, nil
			}
		}
	}
	// Check vendored model path.
	if p := s.ResolveModelPath(); p != "" {
		return true, nil
	}
	return false, nil
}

// ListModels returns a list of model file names found in the models directory.
// Returns only .gguf and .bin files.
func (s *BitNetServer) ListModels() ([]string, error) {
	entries, err := os.ReadDir(s.config.ModelsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read models dir: %w", err)
	}
	var models []string
	for _, e := range entries {
		ext := filepath.Ext(e.Name())
		if !e.IsDir() && (ext == ".gguf" || ext == ".bin") {
			models = append(models, e.Name())
		}
	}
	return models, nil
}

// GetModelsDir returns the configured models directory path.
func (s *BitNetServer) GetModelsDir() string {
	return s.config.ModelsDir
}

// GetConfig returns the server configuration.
func (s *BitNetServer) GetConfig() BitNetServerConfig {
	return s.config
}

// hasModelWeights checks if any model weight files exist in the models directory
// or in the vendored BitNet models directory.
func (s *BitNetServer) hasModelWeights() bool {
	// Check user models dir
	entries, err := os.ReadDir(s.config.ModelsDir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && (filepath.Ext(e.Name()) == ".gguf" || filepath.Ext(e.Name()) == ".bin") {
				return true
			}
		}
	}
	// Check vendored model path
	if p := s.ResolveModelPath(); p != "" {
		return true
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

// resolveBinaryPath determines the path to the llama-server binary packaged
// with BitNet. Resolution order:
//  1. Explicit BinaryPath in config (user override)
//  2. vendor/BitNet/build/bin/llama-server relative to the axiom binary
//  3. vendor/BitNet/build/bin/llama-server relative to the working directory
//  4. "llama-server" on PATH (fallback for system-installed llama.cpp)
func (s *BitNetServer) resolveBinaryPath() string {
	// 1. Explicit config override
	if s.config.BinaryPath != "" {
		return s.config.BinaryPath
	}

	// 2. Relative to the axiom binary itself
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		// The axiom binary lives in bin/ or at the project root.
		// Try going up from bin/ to find vendor/BitNet/
		candidates := []string{
			filepath.Join(exeDir, "..", "third_party", "BitNet", "build", "bin", "llama-server"),
			filepath.Join(exeDir, "third_party", "BitNet", "build", "bin", "llama-server"),
		}
		for _, c := range candidates {
			if resolved, err := filepath.Abs(c); err == nil {
				if _, err := os.Stat(resolved); err == nil {
					return resolved
				}
			}
		}
	}

	// 3. Relative to working directory
	candidates := []string{
		filepath.Join("third_party", "BitNet", "build", "bin", "llama-server"),
	}
	for _, c := range candidates {
		if resolved, err := filepath.Abs(c); err == nil {
			if _, err := os.Stat(resolved); err == nil {
				return resolved
			}
		}
	}

	// 4. Fallback: try llama-server on PATH (system install via brew, etc.)
	if p, err := exec.LookPath("llama-server"); err == nil {
		return p
	}

	// Last resort: return the name and let exec.Command fail with a clear error
	return "llama-server"
}

// resolveBitNetDir finds the root of the third_party/BitNet directory.
func (s *BitNetServer) resolveBitNetDir() string {
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates := []string{
			filepath.Join(exeDir, "..", "third_party", "BitNet"),
			filepath.Join(exeDir, "third_party", "BitNet"),
		}
		for _, c := range candidates {
			if abs, err := filepath.Abs(c); err == nil {
				if info, err := os.Stat(abs); err == nil && info.IsDir() {
					return abs
				}
			}
		}
	}
	if abs, err := filepath.Abs(filepath.Join("third_party", "BitNet")); err == nil {
		if info, err := os.Stat(abs); err == nil && info.IsDir() {
			return abs
		}
	}
	return ""
}

// ResolveModelPath returns the path to the GGUF model file to use.
// It checks the configured ModelsDir first, then the vendored model directory.
func (s *BitNetServer) ResolveModelPath() string {
	// Check user models dir first
	if p := s.findModelPath(); p != "" {
		return p
	}

	// Check vendored model directory
	vendoredPaths := []string{}
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		vendoredPaths = append(vendoredPaths,
			filepath.Join(exeDir, "..", "third_party", "BitNet", "models"),
			filepath.Join(exeDir, "third_party", "BitNet", "models"),
		)
	}
	vendoredPaths = append(vendoredPaths, filepath.Join("third_party", "BitNet", "models"))

	for _, base := range vendoredPaths {
		if abs, err := filepath.Abs(base); err == nil {
			if entries, err := os.ReadDir(abs); err == nil {
				for _, d := range entries {
					if d.IsDir() && strings.Contains(strings.ToLower(d.Name()), "falcon") {
						modelDir := filepath.Join(abs, d.Name())
						if files, err := os.ReadDir(modelDir); err == nil {
							for _, f := range files {
								if !f.IsDir() && strings.HasSuffix(f.Name(), ".gguf") && strings.Contains(f.Name(), "i2_s") {
									return filepath.Join(modelDir, f.Name())
								}
							}
						}
					}
				}
			}
		}
	}

	return ""
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
