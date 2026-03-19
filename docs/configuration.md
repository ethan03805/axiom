# Configuration Reference

Axiom uses TOML configuration files at two levels:

1. **Global** (`~/.axiom/config.toml`) -- Defaults for all projects on the machine
2. **Project** (`.axiom/config.toml`) -- Per-project overrides

Project configuration takes precedence over global configuration. CLI flags (e.g., `--budget`) override both.

---

## Complete Configuration File

```toml
# =============================================================================
# Project Settings
# =============================================================================
[project]
name = "my-project"                     # Human-readable project name
slug = "my-project"                     # URL-safe identifier (used in branch names)

# =============================================================================
# Budget Settings
# =============================================================================
[budget]
max_usd = 10.00                         # Maximum total spend for this project
warn_at_percent = 80                    # Emit budget_warning event at this %

# =============================================================================
# Concurrency Settings
# =============================================================================
[concurrency]
max_meeseeks = 10                       # Maximum parallel Meeseeks containers

# =============================================================================
# Orchestrator Settings
# =============================================================================
[orchestrator]
runtime = "claw"                        # Orchestrator runtime: claw | claude-code | codex | opencode
srs_approval_delegate = "user"          # Who approves the SRS: user | claw

# =============================================================================
# BitNet Local Inference
# =============================================================================
[bitnet]
enabled = true                          # Enable local inference via BitNet
host = "localhost"                      # BitNet server host
port = 3002                             # BitNet server port
max_concurrent_requests = 4             # Max parallel requests to BitNet
cpu_threads = 4                         # CPU threads allocated to BitNet

# =============================================================================
# Docker Container Settings
# =============================================================================
[docker]
image = "axiom-meeseeks-multi:latest"   # Default Meeseeks container image
timeout_minutes = 30                    # Hard timeout per container
cpu_limit = 0.5                         # CPU cores per container
mem_limit = "2g"                        # Memory limit per container
network_mode = "none"                   # Network mode (should always be "none")

# =============================================================================
# Validation Sandbox Settings
# =============================================================================
[validation]
timeout_minutes = 10                    # Validation sandbox timeout
cpu_limit = 1.0                         # CPU cores for validation
mem_limit = "4g"                        # Memory limit for validation
network = "none"                        # Network access (must be "none")
allow_dependency_install = true         # Allow lockfile-based dependency install
security_scan = false                   # Run security scanning (trivy/gosec)
warm_pool_enabled = false               # Enable warm sandbox pool (experimental)
warm_pool_size = 3                      # Number of pre-warmed containers
warm_cold_interval = 10                 # Full cold build every N warm runs

# Integration sandbox (opt-in, not the default)
[validation.integration]
enabled = false                         # Enable integration testing sandbox
allowed_services = []                   # Allowed network services (e.g., ["postgres:5432"])
secrets = []                            # Secrets to inject (e.g., ["DATABASE_URL"])
network_egress = []                     # Allowed network ranges (e.g., ["10.0.0.0/8"])

# =============================================================================
# Security Settings
# =============================================================================
[security]
force_local_for_sensitive = true        # Route sensitive-file tasks to BitNet
sensitive_patterns = [                  # File patterns treated as sensitive
    "*.env*",
    "*credentials*",
    "**/secrets/**"
]

# =============================================================================
# Git Settings
# =============================================================================
[git]
auto_commit = true                      # Automatically commit approved output
branch_prefix = "axiom"                 # Branch name prefix (branch = axiom/<slug>)

# =============================================================================
# API Server Settings
# =============================================================================
[api]
port = 3000                             # API server port
rate_limit_rpm = 120                    # Requests per minute per token
allowed_ips = []                        # IP allowlist (empty = allow all)

# =============================================================================
# Observability Settings
# =============================================================================
[observability]
log_prompts = false                     # Capture full prompts + responses to disk
log_token_counts = true                 # Always log token counts (no content)
```

---

## Section Details

### [project]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `name` | string | Required | Human-readable project name displayed in status and GUI |
| `slug` | string | Required | URL-safe identifier used in git branch name (`axiom/<slug>`) |

### [budget]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_usd` | float | Required | Hard budget ceiling. Execution pauses when reached. |
| `warn_at_percent` | int | `80` | Percentage at which a `budget_warning` event is emitted |

Budget enforcement is per-request. Before every inference call, the engine calculates the maximum possible cost and verifies it fits within the remaining budget. This is dynamic pre-authorization, not a fixed reservation.

### [concurrency]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `max_meeseeks` | int | `10` | Maximum number of parallel Meeseeks containers. BitNet tasks count against a separate local resource limit. |

### [orchestrator]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `runtime` | string | `"claw"` | Which orchestrator runtime to use. Options: `claw`, `claude-code`, `codex`, `opencode` |
| `srs_approval_delegate` | string | `"user"` | Who approves the SRS. `"user"` requires manual approval. `"claw"` delegates to the Claw orchestrator. ECO approval follows the same setting. |

**Orchestrator deployment modes:**

- **Claw** (recommended): Connects via REST API. Uses its own inference provider. Partial budget tracking (engine-side only).
- **Claude Code / Codex / OpenCode**: Run inside Docker containers. All inference brokered through the engine. Full budget tracking.

### [bitnet]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `true` | Enable BitNet local inference |
| `host` | string | `"localhost"` | BitNet server hostname |
| `port` | int | `3002` | BitNet server port |
| `max_concurrent_requests` | int | `4` | Maximum parallel inference requests |
| `cpu_threads` | int | `4` | CPU threads allocated to BitNet server |

When enabled, trivial tasks (variable renames, imports, config changes) are routed to the local BitNet server for free, zero-latency inference.

### [docker]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `image` | string | `"axiom-meeseeks-multi:latest"` | Default Docker image for Meeseeks containers |
| `timeout_minutes` | int | `30` | Hard timeout per container (killed if exceeded) |
| `cpu_limit` | float | `0.5` | CPU cores per container |
| `mem_limit` | string | `"2g"` | Memory limit per container |
| `network_mode` | string | `"none"` | Network mode. Must be `"none"` for security. |

All containers are spawned with hardening flags: `--read-only`, `--cap-drop=ALL`, `--security-opt=no-new-privileges`, `--pids-limit=256`, `--tmpfs /tmp:rw,noexec,size=256m`, `--network=none`, and non-root user execution.

### [validation]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `timeout_minutes` | int | `10` | Validation sandbox timeout |
| `cpu_limit` | float | `1.0` | CPU cores for validation containers |
| `mem_limit` | string | `"4g"` | Memory limit for validation containers |
| `network` | string | `"none"` | Network access. Must be `"none"`. |
| `allow_dependency_install` | bool | `true` | Allow dependency install from lockfiles |
| `security_scan` | bool | `false` | Run security scanning tools |
| `warm_pool_enabled` | bool | `false` | Enable pre-warmed validation containers |
| `warm_pool_size` | int | `3` | Number of pre-warmed containers |
| `warm_cold_interval` | int | `10` | Full cold build interval |

### [validation.integration]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `enabled` | bool | `false` | Enable integration testing sandbox |
| `allowed_services` | string[] | `[]` | Allowed network services |
| `secrets` | string[] | `[]` | Secrets injected into integration sandbox |
| `network_egress` | string[] | `[]` | Allowed network CIDR ranges |

The integration sandbox provides scoped network access and secrets for tests that require live service interaction. It is opt-in and disabled by default.

### [security]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `force_local_for_sensitive` | bool | `true` | Route tasks touching sensitive files to BitNet |
| `sensitive_patterns` | string[] | `["*.env*", "*credentials*", "**/secrets/**"]` | Glob patterns for sensitive file detection |

### [git]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `auto_commit` | bool | `true` | Automatically commit approved output |
| `branch_prefix` | string | `"axiom"` | Branch name prefix. Branch is `<prefix>/<slug>`. |

### [api]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `port` | int | `3000` | API server listen port |
| `rate_limit_rpm` | int | `120` | Requests per minute per token |
| `allowed_ips` | string[] | `[]` | IP allowlist. Empty means allow all. Supports CIDR notation. |

### [observability]

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `log_prompts` | bool | `false` | Capture full prompt/response content to disk |
| `log_token_counts` | bool | `true` | Log token counts (no content) in task_attempts |

When `log_prompts` is true, full prompts and responses are saved to `.axiom/logs/prompts/<task-id>-<attempt>.json`. Secrets are redacted before logging.

---

## Global Configuration

The global configuration at `~/.axiom/config.toml` provides defaults for all projects. It typically contains:

```toml
[openrouter]
api_key = "sk-or-v1-your-key-here"

[budget]
max_usd = 10.00
warn_at_percent = 80

[bitnet]
enabled = true
host = "localhost"
port = 3002
cpu_threads = 4
```

The `[openrouter]` section with `api_key` is the most important global setting. This key is used by the Inference Broker to call cloud models via OpenRouter. It is never injected into containers.

---

## Configuration Precedence

1. **CLI flags** (e.g., `--budget 15.00`) -- highest priority
2. **Project config** (`.axiom/config.toml`) -- per-project settings
3. **Global config** (`~/.axiom/config.toml`) -- user defaults
4. **Built-in defaults** -- lowest priority

---

## Configuration Reload

Configuration can be reloaded without restarting the engine:

```bash
axiom config reload
```

This re-reads both global and project configuration files. API keys are updated in-memory, and skill files are regenerated if configuration changed.
