package tunnel

import (
	"testing"
)

func TestNewManagerCreatesWithCorrectPort(t *testing.T) {
	m := NewManager(3000)
	if m.apiPort != 3000 {
		t.Errorf("expected apiPort=3000, got %d", m.apiPort)
	}

	m2 := NewManager(8080)
	if m2.apiPort != 8080 {
		t.Errorf("expected apiPort=8080, got %d", m2.apiPort)
	}
}

func TestIsRunningReturnsFalseInitially(t *testing.T) {
	m := NewManager(3000)
	if m.IsRunning() {
		t.Error("IsRunning should return false for a newly created manager")
	}
}

func TestPublicURLReturnsEmptyInitially(t *testing.T) {
	m := NewManager(3000)
	if url := m.PublicURL(); url != "" {
		t.Errorf("PublicURL should be empty initially, got %q", url)
	}
}

func TestStopWhenNotRunningReturnsNil(t *testing.T) {
	m := NewManager(3000)
	if err := m.Stop(); err != nil {
		t.Errorf("Stop on non-running manager should return nil, got %v", err)
	}
}

func TestStopWhenNotRunningDoesNotChangeState(t *testing.T) {
	m := NewManager(3000)

	// Call Stop on a manager that was never started.
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	// State should remain unchanged.
	if m.IsRunning() {
		t.Error("IsRunning should still be false after Stop on non-running manager")
	}
	if url := m.PublicURL(); url != "" {
		t.Errorf("PublicURL should still be empty after Stop, got %q", url)
	}
}

func TestNewManagerFieldsInitialized(t *testing.T) {
	m := NewManager(9090)

	// Verify all fields are at their zero/expected values.
	if m.cmd != nil {
		t.Error("cmd should be nil initially")
	}
	if m.running {
		t.Error("running should be false initially")
	}
	if m.publicURL != "" {
		t.Errorf("publicURL should be empty initially, got %q", m.publicURL)
	}
}
