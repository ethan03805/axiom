package engine

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// Config holds the complete Axiom configuration, combining project-level
// and global settings. Project config overrides global config.
type Config struct {
	Project       ProjectConfig       `toml:"project"`
	Budget        BudgetConfig        `toml:"budget"`
	Concurrency   ConcurrencyConfig   `toml:"concurrency"`
	Orchestrator  OrchestratorConfig  `toml:"orchestrator"`
	BitNet        BitNetConfig        `toml:"bitnet"`
	Docker        DockerConfig        `toml:"docker"`
	Validation    ValidationConfig    `toml:"validation"`
	Security      SecurityConfig      `toml:"security"`
	Git           GitConfig           `toml:"git"`
	API           APIConfig           `toml:"api"`
	Observability ObservabilityConfig `toml:"observability"`
	OpenRouter    OpenRouterConfig    `toml:"openrouter"`
}

// ProjectConfig holds project identification settings.
type ProjectConfig struct {
	Name string `toml:"name"`
	Slug string `toml:"slug"`
}

// BudgetConfig holds cost management settings.
type BudgetConfig struct {
	MaxUSD        float64 `toml:"max_usd"`
	WarnAtPercent float64 `toml:"warn_at_percent"`
}

// ConcurrencyConfig controls parallel execution limits.
type ConcurrencyConfig struct {
	MaxMeeseeks int `toml:"max_meeseeks"`
}

// OrchestratorConfig controls the orchestrator behavior.
type OrchestratorConfig struct {
	Runtime             string `toml:"runtime"`
	SRSApprovalDelegate string `toml:"srs_approval_delegate"`
}

// BitNetConfig controls the local BitNet model server.
// BinaryPath and ModelsDir allow overriding the packaged defaults.
// When empty, Axiom resolves these relative to the axiom binary location.
type BitNetConfig struct {
	Enabled               bool   `toml:"enabled"`
	Host                  string `toml:"host"`
	Port                  int    `toml:"port"`
	MaxConcurrentRequests int    `toml:"max_concurrent_requests"`
	CPUThreads            int    `toml:"cpu_threads"`
	BinaryPath            string `toml:"binary_path"` // Path to llama-server binary (default: vendor/BitNet/build/bin/llama-server relative to axiom binary)
	ModelsDir             string `toml:"models_dir"`  // Path to model weights directory (default: ~/.axiom/bitnet/models/)
	ModelRepo             string `toml:"model_repo"`  // HuggingFace repo for model download (default: tiiuae/Falcon3-1B-Instruct-1.58bit)
}

// DockerConfig holds container runtime settings.
// CPULimit is a float representing CPU cores (e.g. 0.5 = half a core).
type DockerConfig struct {
	Image          string  `toml:"image"`
	TimeoutMinutes int     `toml:"timeout_minutes"`
	CPULimit       float64 `toml:"cpu_limit"`
	MemLimit       string  `toml:"mem_limit"`
	NetworkMode    string  `toml:"network_mode"`
}

// ValidationConfig holds validation and testing settings.
// CPULimit is a float representing CPU cores (e.g. 1.0 = one core).
type ValidationConfig struct {
	TimeoutMinutes         int                        `toml:"timeout_minutes"`
	CPULimit               float64                    `toml:"cpu_limit"`
	MemLimit               string                     `toml:"mem_limit"`
	Network                string                     `toml:"network"`
	AllowDependencyInstall bool                       `toml:"allow_dependency_install"`
	SecurityScan           bool                       `toml:"security_scan"`
	WarmPoolEnabled        bool                       `toml:"warm_pool_enabled"`
	WarmPoolSize           int                        `toml:"warm_pool_size"`
	WarmColdInterval       int                        `toml:"warm_cold_interval"`
	Integration            ValidationIntegrationConfig `toml:"integration"`
}

// ValidationIntegrationConfig holds integration test settings.
type ValidationIntegrationConfig struct {
	Enabled         bool     `toml:"enabled"`
	AllowedServices []string `toml:"allowed_services"`
	Secrets         []string `toml:"secrets"`
	NetworkEgress   []string `toml:"network_egress"`
}

// SecurityConfig holds security-related settings.
type SecurityConfig struct {
	ForceLocalForSensitive bool     `toml:"force_local_for_sensitive"`
	SensitivePatterns      []string `toml:"sensitive_patterns"`
}

// GitConfig holds git integration settings.
type GitConfig struct {
	AutoCommit   bool   `toml:"auto_commit"`
	BranchPrefix string `toml:"branch_prefix"`
}

// APIConfig holds REST API settings.
type APIConfig struct {
	Port         int      `toml:"port"`
	RateLimitRPM int      `toml:"rate_limit_rpm"`
	AllowedIPs   []string `toml:"allowed_ips"`
}

// OpenRouterConfig holds OpenRouter API settings.
type OpenRouterConfig struct {
	APIKey string `toml:"api_key"`
}

// ObservabilityConfig holds logging and metrics settings.
type ObservabilityConfig struct {
	LogPrompts     bool `toml:"log_prompts"`
	LogTokenCounts bool `toml:"log_token_counts"`
}

// DefaultConfig returns a Config with sensible default values matching
// Architecture.md Appendix A.
func DefaultConfig() *Config {
	return &Config{
		Budget: BudgetConfig{
			MaxUSD:        10.0,
			WarnAtPercent: 80.0,
		},
		Concurrency: ConcurrencyConfig{
			MaxMeeseeks: 10,
		},
		Orchestrator: OrchestratorConfig{
			Runtime:             "claw",
			SRSApprovalDelegate: "user",
		},
		BitNet: BitNetConfig{
			Enabled:               true,
			Host:                  "localhost",
			Port:                  3002,
			MaxConcurrentRequests: 4,
			CPUThreads:            4,
		},
		Docker: DockerConfig{
			Image:          "axiom-meeseeks-multi:latest",
			TimeoutMinutes: 30,
			CPULimit:       0.5,
			MemLimit:       "2g",
			NetworkMode:    "none",
		},
		Validation: ValidationConfig{
			TimeoutMinutes:         10,
			CPULimit:               1.0,
			MemLimit:               "4g",
			Network:                "none",
			AllowDependencyInstall: true,
			SecurityScan:           false,
			WarmPoolEnabled:        false,
			WarmPoolSize:           3,
			WarmColdInterval:       10,
		},
		Security: SecurityConfig{
			ForceLocalForSensitive: true,
			SensitivePatterns:      []string{"*.env*", "*credentials*", "**/secrets/**"},
		},
		Git: GitConfig{
			AutoCommit:   true,
			BranchPrefix: "axiom",
		},
		API: APIConfig{
			Port:         3000,
			RateLimitRPM: 120,
		},
		Observability: ObservabilityConfig{
			LogPrompts:     false,
			LogTokenCounts: true,
		},
	}
}

// LoadConfig loads configuration from both global (~/.axiom/config.toml)
// and project (.axiom/config.toml) paths, with project config overriding global.
func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	// Load global config
	home, err := os.UserHomeDir()
	if err == nil {
		globalPath := filepath.Join(home, ".axiom", "config.toml")
		if _, err := os.Stat(globalPath); err == nil {
			if _, err := toml.DecodeFile(globalPath, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Load project config (overrides global)
	projectPath := filepath.Join(".axiom", "config.toml")
	if _, err := os.Stat(projectPath); err == nil {
		if _, err := toml.DecodeFile(projectPath, cfg); err != nil {
			return nil, err
		}
	}

	return cfg, nil
}

// LoadConfigFrom loads configuration from a specific file path.
func LoadConfigFrom(path string) (*Config, error) {
	cfg := DefaultConfig()
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
