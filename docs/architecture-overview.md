# Architecture Overview

This document explains how Axiom works at a high level. For implementation details, see the individual component documents.

---

## Design Philosophy

Axiom is built on two foundational insights:

1. **The Misinterpretation Loop**: When users correct AI agents mid-execution, agents blend contradictory instructions rather than cleanly replacing them. Axiom eliminates this by enforcing a single-prompt, specification, approval, autonomous execution flow. Once the SRS is approved, scope is locked. Environmental changes go through a controlled ECO process.

2. **Context Overload & Hallucination**: As AI agents accumulate context, they hallucinate references to nonexistent code. Axiom prevents this by giving each worker only the minimum structured context for its specific task, then destroying the worker immediately after completion.

---

## Three-Layer Architecture

Axiom separates concerns into three distinct layers:

```
+-----------------------------------------------------------+
|                    TRUSTED ENGINE                          |
|                   (Go Control Plane)                      |
|                                                           |
|  SQLite State | Git Manager | File Router | Container Mgr |
|  Budget Enforcer | Semantic Indexer | Inference Broker    |
|  Model Registry | Merge Queue | Event Emitter | API      |
+-------------------+-------------------+-------------------+
                    |                   |
                    v                   v
+-------------------+---+  +-----------+-----------------------+
|  UNTRUSTED AGENT      |  |  UNTRUSTED ARTIFACT EXECUTION    |
|  PLANE (Docker)       |  |  PLANE (Docker)                  |
|                       |  |                                   |
|  Orchestrator         |  |  Validation Sandbox               |
|  Sub-Orchestrators    |  |  (compile, lint, test)            |
|  Meeseeks Workers     |  |  No network, no secrets           |
|  Reviewers            |  |  Runs untrusted generated code    |
+-----------------------+  +-----------------------------------+
```

### Trusted Engine (Control Plane)

The Go binary running on the host machine. It has full authority over all privileged operations: filesystem writes, git commits, container spawning, model API calls, budget enforcement, and merge queue serialization. It is the single source of truth.

### Untrusted Agent Plane

All AI agents (orchestrator, sub-orchestrators, Meeseeks workers, reviewers) running inside hardened Docker containers. They propose actions through structured IPC requests. The engine validates, authorizes, and executes those actions. No agent has direct access to the filesystem, Docker, git, or model APIs.

### Untrusted Artifact Execution Plane

Validation sandboxes that run untrusted generated code (compilation, testing, linting). Distinct from the Agent Plane -- sandboxes execute code, not LLM reasoning. They have no network, no secrets, and resource limits.

---

## Core Flow

```
User writes a prompt
    |
    v
Orchestrator generates an SRS (Software Requirements Specification)
    |
    v
User reviews and approves the SRS --> scope is locked
    |
    v
Orchestrator decomposes the SRS into a hierarchical task tree
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

---

## Key Subsystems

### Coordinator (`internal/engine/coordinator.go`)

The central orchestrator wiring all 13 subsystems together. Manages startup, shutdown, and subsystem lifecycle.

### State Layer (`internal/state/`)

SQLite in WAL mode with 18 tables tracking tasks, attempts, validation runs, reviews, artifacts, containers, events, costs, ECOs, locks, and semantic index data. The database is the single source of truth for all project state.

### Container Manager (`internal/container/`)

Spawns, tracks, and destroys Docker containers with full hardening: `--read-only`, `--cap-drop=ALL`, `--network=none`, `--pids-limit=256`, non-root execution, resource limits. Enforces concurrency limits and timeout enforcement.

### IPC Protocol (`internal/ipc/`)

Filesystem-based inter-process communication between the engine and containers. JSON files are written to `/workspace/ipc/input/` (engine to container) and `/workspace/ipc/output/` (container to engine). Uses `fsnotify` for sub-100ms detection of new messages, with polling fallback.

### Inference Broker (`internal/broker/`)

Mediates ALL model API calls. Containers submit inference requests via IPC; the broker validates model allowlists, checks budget, enforces rate limits, routes to the appropriate provider (OpenRouter or BitNet), and logs every request. Supports streaming via chunked IPC files.

### Task System (`internal/state/tasks.go`)

Hierarchical task trees with dependency resolution, write-set locking (file-level mutual exclusion), scope expansion handling, and a state machine enforcing valid transitions: `queued -> in_progress -> in_review -> done` with retry and escalation paths.

### Approval Pipeline (`internal/pipeline/`)

Five-stage pipeline: manifest validation, validation sandbox, reviewer evaluation, orchestrator validation, merge queue. Each stage can trigger fresh-container retries (max 3) or model tier escalation (max 2).

### Merge Queue (`internal/merge/`)

Serializes commits to prevent stale-context conflicts. Validates base snapshots against HEAD, attempts three-way merge when stale, runs integration checks before every commit, and releases write-set locks after successful merge.

### Semantic Indexer (`internal/index/`)

Go AST-based code parser extracting functions, types, interfaces, constants, imports, exports, and dependency relationships. Provides five typed query types: `lookup_symbol`, `reverse_dependencies`, `list_exports`, `find_implementations`, `module_graph`. Enables precise TaskSpec context construction.

### Budget Enforcer (`internal/budget/`)

Per-request budget verification before every inference call. Tracks costs at all granularities: per-request, per-attempt, per-task, per-model, per-agent-type, per-project. Emits warnings at configurable thresholds and pauses execution on exhaustion.

### Security (`internal/security/`)

Secret scanning with regex-based detection and `[REDACTED]` replacement. Prompt injection mitigation with `<untrusted_repo_content>` wrapping, instruction separation, provenance labels, and comment sanitization. File safety validation with path canonicalization, symlink rejection, and scope enforcement.

### API Server (`internal/api/`)

REST + WebSocket server for external orchestrators (Claws). Token authentication, rate limiting, IP restrictions, and audit logging. 16+ endpoints covering the full project lifecycle.

### Event System (`internal/events/`)

In-process pub-sub event bus with 21+ event types. Broadcasts to GUI (via Wails), API WebSocket clients, and internal consumers. All events are logged to the `events` table for audit.

---

## Meeseeks: The Worker Model

Named after Rick and Morty's Mr. Meeseeks, each worker is:

- **Summoned** for a single task
- **Given** a self-contained TaskSpec with minimum necessary context
- **Isolated** in a hardened Docker container with no network, no filesystem access
- **Destroyed** immediately after completion

Meeseeks never persist between tasks. They never accumulate context. Every retry spawns a fresh container with an explicit, deterministic TaskSpec. This prevents the context overload and hallucination that plague long-lived agents.

---

## Model Tiers

| Tier | Models | Use Cases | Cost |
|------|--------|-----------|------|
| **Local** | BitNet/Falcon3 | Variable renames, imports, config changes, boilerplate | Free |
| **Cheap** | Haiku, Flash, small open-source | Simple functions, small modifications | Low |
| **Standard** | Sonnet, GPT-4o, mid-tier open-source | Most coding tasks, refactoring, multi-file changes | Moderate |
| **Premium** | Opus, o1, large open-source | Complex algorithms, API construction, critical-path code | High |

The orchestrator selects the appropriate tier for each task. Failed tasks automatically retry (up to 3 times), then escalate to a more capable model tier (up to 2 escalations).

---

## Test Authorship Separation

Tests are never written by the same AI model that wrote the implementation. Test generation is a separate task assigned to a different model family. This prevents circular validation where a model writes both the code and the tests that validate it.

---

## Engineering Change Orders (ECOs)

When the real world contradicts an SRS assumption (a library was deprecated, an API changed), the orchestrator proposes an ECO. ECOs are limited to six environmental categories:

| Code | Category | Example |
|------|----------|---------|
| `ECO-DEP` | Dependency Unavailable | Package removed from npm |
| `ECO-API` | API Breaking Change | REST endpoint returns 404 |
| `ECO-SEC` | Security Vulnerability | CVE in auth library |
| `ECO-PLT` | Platform Incompatibility | Library doesn't support OS |
| `ECO-LIC` | License Conflict | GPL in MIT project |
| `ECO-PRV` | Provider Limitation | API rate limit too low |

ECOs cannot add features, change scope, or alter acceptance criteria. They address environmental changes only.

---

## Data Flow Diagram

```
User Prompt
    |
    v
[Orchestrator] --submit_srs--> [Engine] --> .axiom/srs.md
    |                                            |
    | (after approval)                           |
    v                                            v
[Orchestrator] --create_task_batch--> [Engine] --> SQLite tasks table
    |                                            |
    v                                            v
[Engine Work Queue] --> Lock acquisition --> Spawn Meeseeks container
                                                |
                                                v
[Meeseeks] --inference_request--> [Engine Broker] --> OpenRouter/BitNet
    |                                                      |
    | (writes code)                                        v
    v                                              (inference_response)
/workspace/staging/ + manifest.json
    |
    v
[Engine] --> Validation Sandbox (compile, lint, test)
    |
    v
[Engine] --> Reviewer container (different model family)
    |
    v
[Engine] --> Orchestrator final validation
    |
    v
[Merge Queue] --> Integration checks --> Git commit
    |
    v
[Semantic Indexer] --> Re-index --> Release locks --> Unblock dependents
```

---

## Technology Stack

| Component | Technology | Rationale |
|-----------|-----------|-----------|
| Core Engine | Go | Performance, static binary, strong concurrency |
| State Store | SQLite (WAL) | Single-file, ACID, no external dependencies |
| Worker Isolation | Docker | OS-level process/network/filesystem isolation |
| Local Inference | BitNet + Falcon3 | Free, zero-latency for trivial tasks |
| Cloud Inference | OpenRouter | Unified access to multiple model providers |
| GUI | Wails v2 (Go + React) | Desktop app with Go backend bindings |
| Code Indexing | Go AST parser | Native Go parsing; tree-sitter planned for other languages |
| CLI | Cobra | Standard Go CLI framework |
| Remote Access | Cloudflare Tunnel + REST/WebSocket | Secure tunnel for remote orchestrators |

---

## Source Code Organization

```
axiom/
  cmd/axiom/          # CLI entrypoint and command definitions
  internal/
    api/              # REST + WebSocket API server
    broker/           # Inference Broker (OpenRouter + BitNet)
    budget/           # Budget enforcement and tracking
    container/        # Docker container lifecycle
    doctor/           # System health checks
    engine/           # Core Coordinator + configuration
    events/           # Event emission system
    git/              # Git branch/commit operations
    index/            # Semantic code indexer
    ipc/              # Filesystem-based IPC protocol
    merge/            # Serialized merge queue
    orchestrator/     # Embedded orchestrator runtime
    pipeline/         # File router & approval pipeline
    registry/         # Model registry
    security/         # Secret scanning + injection mitigation
    skill/            # Skill file generator
    srs/              # SRS & ECO management
    state/            # SQLite state layer
    tunnel/           # Cloudflare tunnel integration
  gui/frontend/       # React TypeScript frontend (Wails)
  docker/             # Dockerfile definitions for Meeseeks images
  schemas/            # SQL migration files
  skills/             # Skill templates for orchestrator runtimes
  models.json         # Curated model capability index
```
