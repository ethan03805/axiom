package doctor

import (
	"testing"
)

func TestDoctorRunsAllChecks(t *testing.T) {
	d := New(DoctorConfig{})
	report := d.Run()

	// Should have at least the core checks: Docker, Git, resources, BitNet server,
	// BitNet local inference, OpenRouter key, OpenRouter connectivity, disk space,
	// config, secret patterns = 10.
	if len(report.Checks) < 10 {
		t.Errorf("expected at least 10 checks, got %d", len(report.Checks))
	}

	// Verify check names are populated.
	for _, c := range report.Checks {
		if c.Name == "" {
			t.Error("check name should not be empty")
		}
		if c.Message == "" {
			t.Error("check message should not be empty")
		}
		if c.Status != StatusPass && c.Status != StatusFail && c.Status != StatusWarning {
			t.Errorf("invalid status: %s", c.Status)
		}
	}
}

func TestDoctorChecksGit(t *testing.T) {
	d := New(DoctorConfig{})
	result := d.checkGit()

	// Git should be available in test environments.
	if result.Status != StatusPass {
		t.Skipf("Git not available: %s", result.Message)
	}
	if result.Name != "Git" {
		t.Errorf("name = %s", result.Name)
	}
}

func TestDoctorChecksResources(t *testing.T) {
	d := New(DoctorConfig{})
	result := d.checkSystemResources()

	if result.Name != "System Resources" {
		t.Errorf("name = %s", result.Name)
	}
	// Should pass or warn, never fail for resources.
	if result.Status == StatusFail {
		t.Error("resource check should not fail (pass or warn)")
	}
}

func TestDoctorChecksBitNetNotConfigured(t *testing.T) {
	d := New(DoctorConfig{}) // No BitNet configured
	result := d.checkBitNetServer()

	if result.Status != StatusWarning {
		t.Errorf("expected warning for unconfigured BitNet, got %s", result.Status)
	}
}

func TestDoctorChecksProjectConfigMissing(t *testing.T) {
	d := New(DoctorConfig{})
	result := d.checkProjectConfig()

	// In test environment, .axiom/config.toml likely doesn't exist.
	if result.Status == StatusFail {
		t.Error("missing config should be warning, not fail")
	}
}

func TestDoctorChecksSecretPatternsValid(t *testing.T) {
	d := New(DoctorConfig{
		SensitivePatterns: []string{`\.env`, `credentials`, `sk-[a-zA-Z0-9]+`},
	})
	result := d.checkSecretPatterns()

	if result.Status != StatusPass {
		t.Errorf("valid patterns should pass: %s", result.Message)
	}
}

func TestDoctorChecksSecretPatternsInvalid(t *testing.T) {
	d := New(DoctorConfig{
		SensitivePatterns: []string{"[invalid regex"},
	})
	result := d.checkSecretPatterns()

	if result.Status != StatusFail {
		t.Errorf("invalid regex should fail: %s", result.Message)
	}
}

func TestDoctorChecksDefaultPatterns(t *testing.T) {
	d := New(DoctorConfig{}) // No custom patterns
	result := d.checkSecretPatterns()

	if result.Status != StatusPass {
		t.Errorf("default patterns should pass: %s", result.Message)
	}
}

func TestDoctorWarmPoolNotEnabled(t *testing.T) {
	d := New(DoctorConfig{WarmPoolEnabled: false})
	report := d.Run()

	// Warm pool check should NOT be included when disabled.
	for _, c := range report.Checks {
		if c.Name == "Warm Pool Images" {
			t.Error("warm pool check should not run when disabled")
		}
	}
}

func TestDoctorChecksBitNet(t *testing.T) {
	// BitNet is typically not running in test environments, expect warning.
	d := New(DoctorConfig{})
	result := d.checkBitNet()

	if result.Name != "BitNet Local Inference" {
		t.Errorf("name = %s", result.Name)
	}
	// Since BitNet is not running in test, expect warning.
	if result.Status != StatusWarning && result.Status != StatusPass {
		t.Errorf("expected warning or pass, got %s", result.Status)
	}
}

func TestDoctorChecksBitNetCustomHostPort(t *testing.T) {
	d := New(DoctorConfig{
		BitNetHost: "127.0.0.1",
		BitNetPort: 9999,
	})
	result := d.checkBitNet()

	if result.Name != "BitNet Local Inference" {
		t.Errorf("name = %s", result.Name)
	}
	// Unreachable host/port should produce a warning.
	if result.Status != StatusWarning {
		t.Errorf("expected warning for unreachable BitNet, got %s: %s", result.Status, result.Message)
	}
}

func TestDoctorChecksOpenRouterKey(t *testing.T) {
	d := New(DoctorConfig{})
	result := d.checkOpenRouterKey()

	if result.Name != "OpenRouter API Key" {
		t.Errorf("name = %s", result.Name)
	}
	// The result depends on environment. Just verify it returns a valid status.
	if result.Status != StatusPass && result.Status != StatusWarning {
		t.Errorf("expected pass or warning, got %s", result.Status)
	}
}

func TestDoctorChecksDiskSpace(t *testing.T) {
	d := New(DoctorConfig{ProjectRoot: "/"})
	result := d.checkDiskSpace()

	if result.Name != "Disk Space" {
		t.Errorf("name = %s", result.Name)
	}
	// Root partition should almost always have space in a test environment.
	if result.Status == StatusWarning {
		t.Skipf("disk space check could not run: %s", result.Message)
	}
}

func TestDoctorChecksDiskSpaceDefaultPath(t *testing.T) {
	d := New(DoctorConfig{}) // Empty ProjectRoot defaults to "."
	result := d.checkDiskSpace()

	if result.Name != "Disk Space" {
		t.Errorf("name = %s", result.Name)
	}
	// Should be able to check current directory.
	if result.Status != StatusPass && result.Status != StatusFail {
		// Warning means syscall error, which is unexpected for "."
		t.Logf("disk space check status: %s - %s", result.Status, result.Message)
	}
}

func TestDoctorReportStatus(t *testing.T) {
	report := &Report{
		AllPass: true,
		Checks: []CheckResult{
			{Name: "Test", Status: StatusPass, Message: "ok"},
		},
	}

	// PrintReport should not panic.
	PrintReport(report)
}
