# Axiom

**AI Agent Orchestration for Autonomous Software Development**

Axiom coordinates multiple AI agents -- running in isolated Docker containers -- to autonomously build software from a single user prompt. You describe what you want, Axiom generates a specification, and then a fleet of disposable AI workers builds it while you watch.

---

## Why Axiom?

AI coding agents have two fundamental problems:

1. **The Misinterpretation Loop.** When you correct an AI agent mid-execution, it blends contradictory instructions rather than cleanly replacing them. The more you correct, the worse it gets. Axiom eliminates this by enforcing a single-prompt -> specification -> approval -> autonomous execution flow. Once you approve the spec, it executes without interruption. If the real world contradicts an assumption (a dependency was removed, an API changed), a controlled Engineering Change Order process handles it -- not ad-hoc scope changes.

2. **Context Overload and Hallucination.** As AI agents accumulate context about a project, they begin hallucinating references to nonexistent code, APIs, and files. Axiom prevents this by giving each worker only the minimum structured context it needs for its specific task, then destroying the worker immediately after completion. Workers never accumulate stale context because they do not persist.

---

## How It Works

```
You write a prompt
    |
    v
Orchestrator generates an SRS (Software Requirements Specification)
    |
    v
You review and approve the SRS (scope is now locked)
    |
    v
Orchestrator decomposes the SRS into a task tree
    |
    v
Engine dispatches tasks to Meeseeks (disposable AI workers)
    |                                        |
    |   Each Meeseeks:                       |
    |   - Receives a self-contained TaskSpec  |
    |   - Runs in an isolated Docker container|
    |   - Writes code to a staging area       |
    |   - Is destroyed after completion       |
    |                                        |
    v                                        v
Validation sandbox runs compilation, linting, tests
    |
    v
Reviewer (different AI model) evaluates the output
    |
    v
Orchestrator performs final validation against the SRS
    |
    v
Merge queue commits to the project branch
    |
    v
Repeat until all tasks are done
```

Up to 10 Meeseeks can run in parallel. The engine handles all the coordination: write-set locking prevents file conflicts, base snapshot pinning catches stale context, and a serialized merge queue ensures every commit is validated against the actual current state of the project.

---

## Key Concepts

### Meeseeks (Workers)

Named after Rick and Morty's Mr. Meeseeks. Each Meeseeks is summoned into existence for a single task, completes that task, and is immediately destroyed. They do not persist between tasks. They do not accumulate context. They exist to serve.

Meeseeks run inside hardened Docker containers with no network access, no project filesystem access, and no ability to call model APIs directly. All communication goes through the engine via filesystem-based IPC.

### Trusted Engine vs. Untrusted Agents

The Go engine running on your machine is the only component with authority to write files, make git commits, spawn containers, call model APIs, and enforce budgets. All AI agents (orchestrator, workers, reviewers) are untrusted -- they can only propose actions through structured requests. The engine validates, authorizes, and executes those actions.

This means a hallucinating orchestrator cannot write arbitrary files, a rogue worker cannot spawn other containers, and budget limits cannot be silently exceeded.

### Immutable Scope with Controlled Change

Once you approve the SRS, the project scope is locked. You can pause or cancel, but you cannot change requirements mid-run. When the real world contradicts an assumption (a library was deprecated, an API endpoint changed), the orchestrator proposes an Engineering Change Order (ECO) through an auditable process. ECOs can only address environmental changes -- not feature changes.

### Model Tiers

Axiom uses different AI models for different tasks based on complexity:

| Tier | Models | Use Cases |
|------|--------|-----------|
| **Local** | BitNet/Falcon3 (free, on your machine) | Variable renames, imports, config changes, boilerplate |
| **Cheap** | Haiku, Flash, small open-source | Simple functions, small modifications |
| **Standard** | Sonnet, GPT-4o, mid-tier open-source | Most coding tasks, refactoring, multi-file changes |
| **Premium** | Opus, o1, large open-source | Complex algorithms, API construction, critical-path code |

Failed tasks automatically retry up to 3 times, then escalate to a more capable model tier (up to 2 escalations).

### Test Authorship Separation

Tests are never written by the same AI model that wrote the implementation. Test generation is always a separate task assigned to a different model family. This prevents circular validation -- "tests pass" is meaningless if the same model wrote both the code and the tests.

---

## Supported Orchestrators

Axiom separates the orchestrator (the AI that plans and coordinates) from the engine (the Go binary that executes). You can use different AI assistants as the orchestrator:

| Orchestrator | How It Connects | Recommended? |
|-------------|----------------|--------------|
| **Claw** (OpenClaw / NanoClaw) | REST API + WebSocket | Yes -- primary recommendation |
| **Claude Code** | Embedded container via IPC | Supported |
| **Codex** | Embedded container via IPC | Supported |
| **OpenCode** | Embedded container via IPC | Supported |

Claw-based orchestrators connect via the Axiom API and use their own model provider. Embedded orchestrators run inside Docker containers with all inference brokered through the engine.

---

## Quick Start

### Prerequisites

- Go 1.22+
- Docker
- An OpenRouter API key (for cloud inference) or local hardware for BitNet

### Install and Initialize

```bash
# Build from source
git clone https://github.com/ethan03805/axiom.git
cd axiom
make build

# Check system requirements
./bin/axiom doctor

# Initialize a project
cd /path/to/your/project
axiom init

# Edit configuration
$EDITOR .axiom/config.toml
```

### Configure

Edit `.axiom/config.toml`:

```toml
[project]
name = "my-project"
slug = "my-project"

[budget]
max_usd = 10.00          # Maximum spend for this project
warn_at_percent = 80      # Warning at 80% budget consumed

[concurrency]
max_meeseeks = 10         # Parallel workers

[orchestrator]
runtime = "claw"          # claw | claude-code | codex | opencode
```

Store your OpenRouter API key in the global config:

```toml
# ~/.axiom/config.toml
[openrouter]
api_key = "sk-or-..."
```

### Run a Project

```bash
axiom run --budget 10.00 "Build me a REST API for user management with authentication"
```

Axiom will:
1. Generate an SRS and present it for your approval
2. Decompose the approved SRS into tasks
3. Execute tasks with Meeseeks workers
4. Validate, review, and merge each task's output
5. Report completion with a cost summary

All work happens on a dedicated `axiom/my-project` git branch. Your current branch is never modified.

---

## CLI Reference

### Project Commands

| Command | Description |
|---------|-------------|
| `axiom init` | Initialize a new Axiom project in the current directory |
| `axiom run "<prompt>"` | Start a project: generate SRS, await approval, execute |
| `axiom run --budget <usd> "<prompt>"` | Start with a specific budget |
| `axiom status` | Show task tree, active workers, budget, resources |
| `axiom pause` | Pause execution (active workers finish, no new ones spawn) |
| `axiom resume` | Resume a paused project |
| `axiom cancel` | Cancel execution, kill containers, revert uncommitted changes |
| `axiom export` | Export project state as JSON |

### Model Commands

| Command | Description |
|---------|-------------|
| `axiom models refresh` | Update model registry from OpenRouter + local |
| `axiom models list` | List all registered models |
| `axiom models list --tier standard` | Filter by tier |
| `axiom models list --family anthropic` | Filter by model family |
| `axiom models info <model-id>` | Show model details and historical performance |

### BitNet Commands (Local Inference)

| Command | Description |
|---------|-------------|
| `axiom bitnet start` | Start local inference server |
| `axiom bitnet stop` | Stop server |
| `axiom bitnet status` | Show server status and resource usage |
| `axiom bitnet models` | List available local models |

### API and Tunnel Commands

| Command | Description |
|---------|-------------|
| `axiom api start` | Start REST + WebSocket API server |
| `axiom api stop` | Stop API server |
| `axiom api token generate` | Generate authentication token |
| `axiom api token generate --scope read-only` | Generate read-only token |
| `axiom api token list` | List active tokens |
| `axiom api token revoke <id>` | Revoke a token |
| `axiom tunnel start` | Start Cloudflare Tunnel for remote access |
| `axiom tunnel stop` | Stop tunnel |

### Utility Commands

| Command | Description |
|---------|-------------|
| `axiom version` | Show version |
| `axiom doctor` | Check Docker, Git, resources, configuration |
| `axiom skill generate --runtime claw` | Generate orchestrator instruction file |
| `axiom index refresh` | Rebuild the semantic code index |
| `axiom config reload` | Reload configuration without restart |

---

## Budget and Cost Management

Axiom enforces a hard budget ceiling on every project. Before each inference request, the engine calculates the maximum possible cost and verifies it fits within the remaining budget. You are never charged more than you authorize.

```bash
# Set budget at project start
axiom run --budget 15.00 "Build me a blog platform"

# Check current spend
axiom status

# If budget runs out, execution pauses and you're prompted
# You can increase the budget or cancel
```

Cost tracking is granular: per-request, per-task, per-model, per-agent-type, and per-project. The GUI dashboard shows real-time cost breakdowns and projected totals.

When using an external Claw orchestrator, Axiom tracks costs for engine-managed operations only (workers, reviewers, sub-orchestrators). The orchestrator's own inference cost is not tracked by Axiom.

---

## Security Model

Axiom is designed with the assumption that all AI agents are untrusted:

- **Docker isolation.** Every worker runs in a hardened container with read-only root filesystem, all Linux capabilities dropped, no network access, PID limits, and resource caps.
- **No project access.** The project filesystem is never mounted into worker containers. Workers write to a staging area; the engine validates and copies approved output.
- **Engine-mediated inference.** Workers never call model APIs directly. All inference goes through the engine's broker with per-task model allowlists, budget limits, and rate limiting.
- **Secret scanning.** The engine scans all context for API keys, tokens, and credentials before packaging it into prompts. Detected secrets are replaced with `[REDACTED]`. Sensitive files are automatically routed to local inference (BitNet) so secrets never leave your machine.
- **Prompt injection mitigation.** All repository content in prompts is wrapped in explicit `<untrusted_repo_content>` tags with source attribution. Comments containing instruction-like patterns are flagged.
- **Validation sandbox.** Generated code is compiled, linted, and tested in an isolated container before it reaches the project. No network, no secrets, resource-limited.
- **Manifest-based file operations.** Every file write, deletion, and rename must be declared in a manifest. The engine rejects undeclared operations, path traversal, symlinks, and out-of-scope modifications.
- **SRS integrity.** The approved specification is SHA-256 hashed and set to read-only. The engine verifies the hash on every startup.

---

## Engineering Change Orders (ECOs)

When the orchestrator discovers that the real world contradicts an assumption in the SRS, it proposes an ECO. ECOs are strictly limited to six environmental categories:

| Category | Code | Example |
|----------|------|---------|
| Dependency Unavailable | `ECO-DEP` | `left-pad` removed from npm |
| API Breaking Change | `ECO-API` | REST endpoint returns 404, schema changed |
| Security Vulnerability | `ECO-SEC` | CVE discovered in chosen auth library |
| Platform Incompatibility | `ECO-PLT` | Library does not support specified OS |
| License Conflict | `ECO-LIC` | GPL dependency in MIT project |
| Provider Limitation | `ECO-PRV` | Free tier API rate limit too low |

ECOs cannot add features, change acceptance criteria, or alter scope. They are presented to you for approval. If you reject an ECO, the orchestrator must find an alternative within the original specification, or you cancel the project.

---

## Project Structure

When you run `axiom init`, the following structure is created:

```
your-project/
  .axiom/
    config.toml          # Project configuration (committed to git)
    srs.md               # Approved SRS (committed, read-only after approval)
    srs.md.sha256        # Integrity hash (committed)
    eco/                 # ECO addendum records (committed)
    axiom.db             # Runtime state (gitignored)
    containers/          # Ephemeral container data (gitignored)
    logs/                # Runtime logs (gitignored)
```

All Axiom-generated code is committed to a dedicated branch (`axiom/<project-slug>`). Your current branch is never modified. When you are satisfied with the result, you merge the branch yourself.

---

## GUI Dashboard

Axiom includes a desktop dashboard (Wails v2 + React) with nine views:

- **Project Overview** -- SRS summary, budget gauge, progress, ECO history
- **Task Tree** -- Hierarchical visualization with color-coded status
- **Active Containers** -- Live list of running workers with model and resource info
- **Cost Dashboard** -- Spend breakdowns, budget gauge, projected total
- **File Diff Viewer** -- Side-by-side diff of worker output with pipeline status
- **Log Stream** -- Real-time scrolling event log
- **Timeline** -- Chronological event visualization
- **Model Registry** -- Browsable model catalog with pricing and performance history
- **Resource Monitor** -- System CPU/memory, container resources, BitNet load

All views update in real-time via the engine's event system. The GUI never polls the database directly.

---

## Technology Stack

| Component | Technology |
|-----------|-----------|
| Core engine | Go |
| State store | SQLite (WAL mode) |
| Worker isolation | Docker (hardened containers) |
| Local inference | BitNet + Falcon3 (1-bit quantized) |
| Cloud inference | OpenRouter API |
| GUI | Wails v2 (Go + React + TypeScript) |
| Semantic indexer | Go `go/parser` + `go/ast` (tree-sitter planned) |
| Remote orchestration | REST API + WebSocket + Cloudflare Tunnel |

---

## Development

```bash
# Build the binary
make build

# Run all tests (~170+ tests across 22 packages)
make test

# Run linter
make lint

# Build Docker images
make docker-images

# Build GUI
make gui

# Build everything
make all
```

---

## License

See LICENSE file for details.
