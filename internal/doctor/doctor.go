// Package doctor implements system health checks that validate all
// prerequisites for running Axiom.
//
// See Architecture Section 22.3 and BUILD_PLAN step 19.3.
package doctor

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// CheckStatus represents the outcome of a health check.
type CheckStatus string

const (
	StatusPass    CheckStatus = "pass"
	StatusFail    CheckStatus = "fail"
	StatusWarning CheckStatus = "warning"
)

// CheckResult holds the result of a single diagnostic check.
type CheckResult struct {
	Name    string
	Status  CheckStatus
	Message string
}

// Report holds the complete diagnostic report.
type Report struct {
	Checks  []CheckResult
	AllPass bool
}

// DoctorConfig holds configuration for the doctor checks.
type DoctorConfig struct {
	BitNetHost       string
	BitNetPort       int
	DockerImage      string
	SensitivePatterns []string
	WarmPoolEnabled  bool
	WarmPoolImage    string
}

// Doctor performs comprehensive system diagnostics.
type Doctor struct {
	config DoctorConfig
}

// New creates a Doctor with the given configuration.
func New(config DoctorConfig) *Doctor {
	return &Doctor{config: config}
}

// Run executes all diagnostic checks and returns a report.
func (d *Doctor) Run() *Report {
	report := &Report{AllPass: true}

	checks := []func() CheckResult{
		d.checkDocker,
		d.checkGit,
		d.checkSystemResources,
		d.checkBitNetServer,
		d.checkOpenRouterConnectivity,
		d.checkProjectConfig,
		d.checkSecretPatterns,
	}

	if d.config.WarmPoolEnabled {
		checks = append(checks, d.checkWarmPoolImages)
	}

	for _, check := range checks {
		result := check()
		report.Checks = append(report.Checks, result)
		if result.Status == StatusFail {
			report.AllPass = false
		}
	}

	return report
}

// PrintReport formats and prints the diagnostic report.
func PrintReport(report *Report) {
	fmt.Println("Axiom Doctor")
	fmt.Println("============")
	fmt.Println()

	for _, c := range report.Checks {
		var icon string
		switch c.Status {
		case StatusPass:
			icon = "[PASS]"
		case StatusFail:
			icon = "[FAIL]"
		case StatusWarning:
			icon = "[WARN]"
		}
		fmt.Printf("  %s %s: %s\n", icon, c.Name, c.Message)
	}

	fmt.Println()
	if report.AllPass {
		fmt.Println("All checks passed. Axiom is ready to run.")
	} else {
		fmt.Println("Some checks failed. Please resolve the issues above.")
	}
}

// --- Individual checks ---

func (d *Doctor) checkDocker() CheckResult {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			Status:  StatusFail,
			Message: "Docker not found or daemon not running. Install Docker and start the daemon.",
		}
	}
	version := strings.TrimSpace(string(out))
	return CheckResult{
		Name:    "Docker",
		Status:  StatusPass,
		Message: fmt.Sprintf("Docker %s", version),
	}
}

func (d *Doctor) checkGit() CheckResult {
	out, err := exec.Command("git", "--version").Output()
	if err != nil {
		return CheckResult{
			Name:    "Git",
			Status:  StatusFail,
			Message: "Git not found. Install git.",
		}
	}
	return CheckResult{
		Name:    "Git",
		Status:  StatusPass,
		Message: strings.TrimSpace(string(out)),
	}
}

func (d *Doctor) checkSystemResources() CheckResult {
	cpus := runtime.NumCPU()
	if cpus < 2 {
		return CheckResult{
			Name:    "System Resources",
			Status:  StatusWarning,
			Message: fmt.Sprintf("%d CPU(s) -- at least 2 recommended for concurrent Meeseeks", cpus),
		}
	}
	return CheckResult{
		Name:    "System Resources",
		Status:  StatusPass,
		Message: fmt.Sprintf("%d CPUs available", cpus),
	}
}

func (d *Doctor) checkBitNetServer() CheckResult {
	if d.config.BitNetHost == "" {
		return CheckResult{
			Name:    "BitNet Server",
			Status:  StatusWarning,
			Message: "BitNet not configured (local inference unavailable)",
		}
	}

	url := fmt.Sprintf("http://%s:%d/v1/models", d.config.BitNetHost, d.config.BitNetPort)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return CheckResult{
			Name:    "BitNet Server",
			Status:  StatusWarning,
			Message: fmt.Sprintf("BitNet server not reachable at %s:%d (run 'axiom bitnet start')", d.config.BitNetHost, d.config.BitNetPort),
		}
	}
	resp.Body.Close()

	return CheckResult{
		Name:    "BitNet Server",
		Status:  StatusPass,
		Message: fmt.Sprintf("BitNet server running at %s:%d", d.config.BitNetHost, d.config.BitNetPort),
	}
}

func (d *Doctor) checkOpenRouterConnectivity() CheckResult {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://openrouter.ai/api/v1/models")
	if err != nil {
		return CheckResult{
			Name:    "OpenRouter Connectivity",
			Status:  StatusWarning,
			Message: "Cannot reach OpenRouter API (external inference unavailable)",
		}
	}
	resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized {
		return CheckResult{
			Name:    "OpenRouter Connectivity",
			Status:  StatusPass,
			Message: "OpenRouter API reachable",
		}
	}

	return CheckResult{
		Name:    "OpenRouter Connectivity",
		Status:  StatusWarning,
		Message: fmt.Sprintf("OpenRouter returned status %d", resp.StatusCode),
	}
}

func (d *Doctor) checkProjectConfig() CheckResult {
	if _, err := os.Stat(".axiom/config.toml"); os.IsNotExist(err) {
		return CheckResult{
			Name:    "Project Configuration",
			Status:  StatusWarning,
			Message: "No project config found. Run 'axiom init' to create one.",
		}
	}
	return CheckResult{
		Name:    "Project Configuration",
		Status:  StatusPass,
		Message: "Project config found at .axiom/config.toml",
	}
}

func (d *Doctor) checkSecretPatterns() CheckResult {
	patterns := d.config.SensitivePatterns
	if len(patterns) == 0 {
		return CheckResult{
			Name:    "Secret Scanner Patterns",
			Status:  StatusPass,
			Message: "Using default patterns (no custom patterns configured)",
		}
	}

	for _, p := range patterns {
		_, err := regexp.Compile(p)
		if err != nil {
			return CheckResult{
				Name:    "Secret Scanner Patterns",
				Status:  StatusFail,
				Message: fmt.Sprintf("Invalid regex pattern '%s': %v", p, err),
			}
		}
	}

	return CheckResult{
		Name:    "Secret Scanner Patterns",
		Status:  StatusPass,
		Message: fmt.Sprintf("%d custom patterns valid", len(patterns)),
	}
}

func (d *Doctor) checkWarmPoolImages() CheckResult {
	image := d.config.WarmPoolImage
	if image == "" {
		image = d.config.DockerImage
	}
	if image == "" {
		return CheckResult{
			Name:    "Warm Pool Images",
			Status:  StatusWarning,
			Message: "No Docker image configured for warm pool",
		}
	}

	out, err := exec.Command("docker", "images", "-q", image).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return CheckResult{
			Name:    "Warm Pool Images",
			Status:  StatusFail,
			Message: fmt.Sprintf("Image '%s' not found. Run 'make docker-images' to build.", image),
		}
	}

	return CheckResult{
		Name:    "Warm Pool Images",
		Status:  StatusPass,
		Message: fmt.Sprintf("Image '%s' available", image),
	}
}
