package broker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBitNetServerDefaults(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{Enabled: true})

	if srv.config.Host != "localhost" {
		t.Errorf("default host = %s, want localhost", srv.config.Host)
	}
	if srv.config.Port != 3002 {
		t.Errorf("default port = %d, want 3002", srv.config.Port)
	}
	if srv.config.CPUThreads != 4 {
		t.Errorf("default threads = %d, want 4", srv.config.CPUThreads)
	}
	if srv.config.MaxConcurrentRequests != 4 {
		t.Errorf("default max concurrent = %d, want 4", srv.config.MaxConcurrentRequests)
	}
}

func TestBitNetServerStatus(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{
		Enabled:    true,
		Host:       "localhost",
		Port:       3002,
		CPUThreads: 8,
	})

	status := srv.Status()
	if !status.Enabled {
		t.Error("expected enabled")
	}
	if status.Running {
		t.Error("should not be running before Start()")
	}
	if status.Port != 3002 {
		t.Errorf("port = %d", status.Port)
	}
	if status.CPUThreads != 8 {
		t.Errorf("threads = %d", status.CPUThreads)
	}
}

func TestBitNetServerDisabled(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{Enabled: false})

	err := srv.Start()
	if err == nil {
		t.Error("expected error when starting disabled server")
	}
}

func TestBitNetServerAlreadyRunning(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{Enabled: true})
	srv.running = true // Simulate running

	err := srv.Start()
	if err == nil {
		t.Error("expected error when already running")
	}
}

func TestBitNetServerFirstRunNoWeights(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-bitnet-*")
	defer os.RemoveAll(tmpDir)

	srv := NewBitNetServer(BitNetServerConfig{
		Enabled:   true,
		ModelsDir: filepath.Join(tmpDir, "models"),
	})

	err := srv.Start()
	if err == nil {
		t.Fatal("expected error for missing weights")
	}
	if !NeedsFirstRun(err) {
		t.Errorf("expected FirstRunError, got: %v", err)
	}

	fre := err.(*FirstRunError)
	if fre.ModelsDir == "" {
		t.Error("FirstRunError should include models dir")
	}
}

func TestBitNetServerHasWeights(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-bitnet-*")
	defer os.RemoveAll(tmpDir)

	modelsDir := filepath.Join(tmpDir, "models")
	os.MkdirAll(modelsDir, 0755)

	// No weights yet.
	srv := NewBitNetServer(BitNetServerConfig{
		Enabled:   true,
		ModelsDir: modelsDir,
	})

	status := srv.Status()
	if status.HasWeights {
		t.Error("should have no weights")
	}

	// Create a fake weight file.
	os.WriteFile(filepath.Join(modelsDir, "falcon3-1b.gguf"), []byte("fake weights"), 0644)

	status = srv.Status()
	if !status.HasWeights {
		t.Error("should detect weights after creating .gguf file")
	}
}

func TestBitNetServerStopWhenNotRunning(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{Enabled: true})

	// Stop when not running should be a no-op.
	if err := srv.Stop(); err != nil {
		t.Errorf("stop on not-running should not error: %v", err)
	}
}

func TestBitNetServerSetupModelsDir(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "axiom-bitnet-setup-*")
	defer os.RemoveAll(tmpDir)

	modelsDir := filepath.Join(tmpDir, "deep", "nested", "models")
	srv := NewBitNetServer(BitNetServerConfig{
		Enabled:   true,
		ModelsDir: modelsDir,
	})

	path, err := srv.SetupModelsDir()
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if path != modelsDir {
		t.Errorf("path = %s, want %s", path, modelsDir)
	}

	// Directory should exist.
	info, err := os.Stat(modelsDir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestBitNetResourceUsage(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{
		Enabled:    true,
		CPUThreads: 4,
	})

	usage := srv.GetResourceUsage()
	if usage.CPUThreads != 4 {
		t.Errorf("threads = %d", usage.CPUThreads)
	}
	if usage.TotalCPUs <= 0 {
		t.Error("should detect system CPUs")
	}
	if usage.CPUPercent <= 0 {
		t.Error("should calculate CPU percent")
	}
}

func TestBitNetRequestTracking(t *testing.T) {
	srv := NewBitNetServer(BitNetServerConfig{Enabled: true})

	srv.TrackRequest()
	srv.TrackRequest()
	if srv.Status().ActiveRequests != 2 {
		t.Errorf("expected 2 active, got %d", srv.Status().ActiveRequests)
	}

	srv.UntrackRequest()
	if srv.Status().ActiveRequests != 1 {
		t.Errorf("expected 1 active, got %d", srv.Status().ActiveRequests)
	}

	srv.UntrackRequest()
	srv.UntrackRequest() // Extra untrack should not go negative.
	if srv.Status().ActiveRequests != 0 {
		t.Errorf("expected 0 active, got %d", srv.Status().ActiveRequests)
	}
}

func TestCheckOverload(t *testing.T) {
	// 4 BitNet threads + 6 container CPUs on 8 total = 10/8 = 125% > 90%
	if !CheckOverload(4, 6, 8) {
		t.Error("expected overload at 125% utilization")
	}

	// 2 threads + 2 containers on 8 total = 4/8 = 50% < 90%
	if CheckOverload(2, 2, 8) {
		t.Error("should not be overloaded at 50%")
	}

	// 4 threads + 3 containers on 8 total = 7/8 = 87.5% < 90%
	if CheckOverload(4, 3, 8) {
		t.Error("should not be overloaded at 87.5%")
	}

	// 4 threads + 4 containers on 8 total = 8/8 = 100% > 90%
	if !CheckOverload(4, 4, 8) {
		t.Error("expected overload at 100% utilization")
	}
}

func TestGrammarForJSON(t *testing.T) {
	grammar := GrammarForJSON()
	if grammar == "" {
		t.Error("expected non-empty JSON grammar")
	}
	// Should contain basic JSON rules.
	if !contains(grammar, "object") || !contains(grammar, "array") || !contains(grammar, "string") {
		t.Error("JSON grammar should contain object, array, string rules")
	}
}

func TestGrammarForGoCode(t *testing.T) {
	grammar := GrammarForGoCode()
	if grammar == "" {
		t.Error("expected non-empty Go grammar")
	}
	if !contains(grammar, "package") || !contains(grammar, "func") {
		t.Error("Go grammar should contain package and func rules")
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
