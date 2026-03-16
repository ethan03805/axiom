package doctor

import (
	"testing"
)

func TestDoctorRunsAllChecks(t *testing.T) {
	d := New(DoctorConfig{})
	report := d.Run()

	// Should have at least the core checks (Docker, Git, resources, BitNet,
	// OpenRouter, config, secret patterns).
	if len(report.Checks) < 7 {
		t.Errorf("expected at least 7 checks, got %d", len(report.Checks))
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
