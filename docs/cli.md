# CLI Reference

The `axiom` CLI is built with Cobra and provides a hierarchical command structure for managing projects, models, infrastructure, and system health.

---

## Project Commands

### axiom init

Initialize a new Axiom project in the current directory.

```bash
axiom init
```

Creates the `.axiom/` directory structure:
- `.axiom/config.toml` -- Default project configuration
- `.axiom/containers/` -- Container specs, staging, and IPC directories
- `.axiom/logs/` -- Runtime logs
- `.axiom/eco/` -- Engineering Change Order records
- Appropriate `.gitignore` entries

The engine refuses to initialize in a directory that already has `.axiom/`.

---

### axiom run

Start a new project: generate SRS, await approval, execute all tasks.

```bash
axiom run "<prompt>"
axiom run --budget <usd> "<prompt>"
```

**Arguments:**
| Argument | Required | Description |
|----------|----------|-------------|
| `prompt` | Yes | Natural language description of the software to build |

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--budget` | From config | Override budget for this run |

**Behavior:**
1. Verifies the git working tree is clean (refuses on dirty tree)
2. Creates git branch `axiom/<project-slug>`
3. Spawns the orchestrator (based on `[orchestrator].runtime` config)
4. Orchestrator generates an SRS from the prompt
5. SRS is presented for user approval
6. On approval, scope is locked (SRS becomes immutable)
7. Orchestrator decomposes SRS into task tree
8. Engine executes tasks with Meeseeks workers in parallel
9. Each task goes through the full approval pipeline
10. Reports completion with cost summary

**Example:**
```bash
axiom run --budget 10.00 "Build a REST API for user management with authentication"
```

---

### axiom status

Display project status, task tree, active workers, budget, and resources.

```bash
axiom status
```

**Output includes:**
- Current project phase and overall progress percentage
- Task tree with status indicators for each task
- List of active Meeseeks and reviewer containers
- Budget consumed, remaining, and projected total
- System resource usage

---

### axiom pause

Pause project execution.

```bash
axiom pause
```

Active Meeseeks and reviewers are allowed to finish their current work. No new containers are spawned. State is persisted to SQLite. Resume with `axiom resume`.

---

### axiom resume

Resume a paused project.

```bash
axiom resume
```

The engine reads the task tree from SQLite and continues dispatching queued tasks.

---

### axiom cancel

Cancel project execution.

```bash
axiom cancel
```

Kills all active containers, reverts uncommitted changes, and marks the project as cancelled. This is destructive and cannot be undone.

---

### axiom export

Export the complete project state as human-readable JSON.

```bash
axiom export
```

Outputs the task tree, attempt history, cost breakdown, event log, and configuration as a structured JSON document.

---

## Model Commands

### axiom models refresh

Update the model registry from all sources.

```bash
axiom models refresh
```

Fetches the latest model list and pricing from OpenRouter, scans locally available BitNet models, and merges with the curated `models.json` capability index.

---

### axiom models list

List all registered models.

```bash
axiom models list
axiom models list --tier standard
axiom models list --family anthropic
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--tier` | Filter by tier: `local`, `cheap`, `standard`, `premium` |
| `--family` | Filter by model family: `anthropic`, `openai`, `meta`, `google`, `local` |

---

### axiom models info

Show detailed information for a specific model.

```bash
axiom models info <model-id>
```

**Example:**
```bash
axiom models info anthropic/claude-sonnet-4
```

Displays: model family, tier, context window, pricing, strengths, weaknesses, recommended use cases, and historical performance statistics (if available).

---

## BitNet Commands

### axiom bitnet start

Start the local BitNet inference server.

```bash
axiom bitnet start
```

On first run, if model weights are not present, prompts for download confirmation and downloads Falcon3 1-bit weights to `~/.axiom/bitnet/models/`.

---

### axiom bitnet stop

Stop the local BitNet inference server.

```bash
axiom bitnet stop
```

---

### axiom bitnet status

Show BitNet server status, loaded model, memory usage, and active requests.

```bash
axiom bitnet status
```

---

### axiom bitnet models

List locally available BitNet models.

```bash
axiom bitnet models
```

---

## API & Tunnel Commands

### axiom api start / stop

Start or stop the REST + WebSocket API server.

```bash
axiom api start
axiom api stop
```

The API server listens on the port configured in `[api].port` (default 3000). It exposes all REST endpoints and WebSocket event streaming for external orchestrators.

---

### axiom api token generate

Generate a new API authentication token.

```bash
axiom api token generate
axiom api token generate --scope read-only
axiom api token generate --scope full-control --expires 8h
```

**Flags:**
| Flag | Default | Description |
|------|---------|-------------|
| `--scope` | `full-control` | Token scope: `read-only` (GET only) or `full-control` (all endpoints) |
| `--expires` | `24h` | Token expiration duration |

Tokens are formatted as `axm_sk_<32-hex-chars>` and stored in `~/.axiom/api-tokens/`.

---

### axiom api token list

List all active (non-revoked) API tokens.

```bash
axiom api token list
```

Shows token IDs, scopes, creation dates, and expiration dates. Token values are not displayed.

---

### axiom api token revoke

Revoke a specific API token immediately.

```bash
axiom api token revoke <token-id>
```

---

### axiom tunnel start / stop

Start or stop a Cloudflare Tunnel for remote Claw access.

```bash
axiom tunnel start
# Outputs: https://<random>.trycloudflare.com

axiom tunnel stop
```

The tunnel exposes the local API server to the internet for remote NanoClaw connections. Requires `cloudflared` to be installed.

---

## Skill Commands

### axiom skill generate

Generate an orchestrator instruction file for a specific runtime.

```bash
axiom skill generate --runtime <runtime>
```

**Runtimes:**
| Runtime | Generated File |
|---------|---------------|
| `claw` | `axiom-skill.md` |
| `claude-code` | `.claude/CLAUDE.md` + hook config |
| `codex` | `codex-instructions.md` |
| `opencode` | `opencode-instructions.md` |

The generated skill file teaches the orchestrator how to use Axiom's API, construct TaskSpecs, manage budgets, and follow the approval pipeline.

---

## Index Commands

### axiom index refresh

Force a full re-index of the project's source code.

```bash
axiom index refresh
```

Parses all source files using Go AST (tree-sitter planned for other languages), extracts functions, types, interfaces, constants, imports, exports, and dependency relationships.

---

### axiom index query

Query the semantic code index.

```bash
axiom index query --type <query_type> [--name <symbol>] [--package <pkg>]
```

**Query types:**
| Type | Description | Required Parameters |
|------|-------------|-------------------|
| `lookup_symbol` | Find a symbol by name | `--name`, optionally `--type` (function/type/interface) |
| `reverse_dependencies` | Find all references to a symbol | `--name` |
| `list_exports` | List all exports in a package | `--package` |
| `find_implementations` | Find types implementing an interface | `--name` |
| `module_graph` | Show dependency graph | `--package` (optional) |

**Example:**
```bash
axiom index query --type reverse_dependencies --name HandleAuth
```

---

## Utility Commands

### axiom version

Show the Axiom version.

```bash
axiom version
```

---

### axiom doctor

Run system health checks.

```bash
axiom doctor
```

Validates:
- Docker daemon availability and version
- BitNet server status
- Network connectivity to OpenRouter
- System resources (CPU, memory, disk)
- Docker image availability for configured Meeseeks image
- Warm-pool images match project configuration
- Secret scanner regex patterns are valid

Each check reports pass, fail, or warning.

---

### axiom config reload

Reload configuration without restarting the engine.

```bash
axiom config reload
```

Re-reads global and project configuration. API keys are updated in memory. Skill files are regenerated if configuration changed.
