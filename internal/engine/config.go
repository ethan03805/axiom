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
type BitNetConfig struct {
	Enabled              bool   `toml:"enabled"`
	Host                 string `toml:"host"`
	Port                 int    `toml:"port"`
	MaxConcurrentRequests int   `toml:"max_concurrent_requests"`
	CPUThreads           int    `toml:"cpu_threads"`
}

// DockerConfig holds container runtime settings.
type DockerConfig struct {
	Image          string `toml:"image"`
	TimeoutMinutes int    `toml:"timeout_minutes"`
	CPULimit       string `toml:"cpu_limit"`
	MemLimit       string `toml:"mem_limit"`
	NetworkMode    string `toml:"network_mode"`
}

// ValidationConfig holds validation and testing settings.
type ValidationConfig struct {
	TimeoutMinutes       int                        `toml:"timeout_minutes"`
	CPULimit             string                     `toml:"cpu_limit"`
	MemLimit             string                     `toml:"mem_limit"`
	Network              string                     `toml:"network"`
	AllowDependencyInstall bool                     `toml:"allow_dependency_install"`
	SecurityScan         bool                       `toml:"security_scan"`
	WarmPoolEnabled      bool                       `toml:"warm_pool_enabled"`
	WarmPoolSize         int                        `toml:"warm_pool_size"`
	WarmColdInterval     int                        `toml:"warm_cold_interval"`
	Integration          ValidationIntegrationConfig `toml:"integration"`
}

// ValidationIntegrationConfig holds integration test settings.
type ValidationIntegrationConfig struct {
	Enabled         bool              `toml:"enabled"`
	AllowedServices []string          `toml:"allowed_services"`
	Secrets         map[string]string `toml:"secrets"`
	NetworkEgress   bool              `toml:"network_egress"`
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

// ObservabilityConfig holds logging and metrics settings.
type ObservabilityConfig struct {
	LogPrompts     bool `toml:"log_prompts"`
	LogTokenCounts bool `toml:"log_token_counts"`
}

// DefaultConfig returns a Config with sensible default values.
func DefaultConfig() *Config {
	return &Config{
		Budget: BudgetConfig{
			MaxUSD:        10.0,
			WarnAtPercent: 80.0,
		},
		Concurrency: ConcurrencyConfig{
			MaxMeeseeks: 3,
		},
		Orchestrator: OrchestratorConfig{
			Runtime: "docker",
		},
		BitNet: BitNetConfig{
			Host: "localhost",
			Port: 8080,
		},
		Docker: DockerConfig{
			Image:          "axiom-worker:latest",
			TimeoutMinutes: 30,
			CPULimit:       "2",
			MemLimit:       "4g",
			NetworkMode:    "none",
		},
		Validation: ValidationConfig{
			TimeoutMinutes: 10,
			CPULimit:       "1",
			MemLimit:       "2g",
			Network:        "none",
			SecurityScan:   true,
			WarmPoolSize:   2,
		},
		Git: GitConfig{
			AutoCommit:   true,
			BranchPrefix: "axiom/",
		},
		API: APIConfig{
			Port:         7700,
			RateLimitRPM: 60,
		},
		Observability: ObservabilityConfig{
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
