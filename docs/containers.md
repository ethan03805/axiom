# Container System

Axiom uses Docker containers to isolate all AI agents from the host system. No agent has direct access to the project filesystem, network, or model APIs.

---

## Container Types

| Type | Purpose | Spawned By |
|------|---------|-----------|
| **Meeseeks** | Execute a single TaskSpec (write code) | Engine, on behalf of orchestrator |
| **Reviewer** | Evaluate Meeseeks output against TaskSpec | Engine, after validation passes |
| **Sub-Orchestrator** | Manage a delegated subtree of tasks | Engine, on behalf of orchestrator |
| **Validator** | Run compilation, linting, tests on generated code | Engine, as part of approval pipeline |
| **Orchestrator** (embedded) | Plan and coordinate the project (Claude Code, Codex, OpenCode) | Engine, at project start |

---

## Container Images

Four language-specific images are provided:

| Image | Contents |
|-------|----------|
| `axiom-meeseeks-go` | Go toolchain + golangci-lint |
| `axiom-meeseeks-node` | Node.js + npm + TypeScript + eslint |
| `axiom-meeseeks-python` | Python 3 + pip + ruff + mypy |
| `axiom-meeseeks-multi` | Go + Node.js + Python (default) |

All images include:
- Non-root user for execution
- IPC watcher script (reads from `/workspace/ipc/input/`, writes to `/workspace/ipc/output/`)
- Minimal shell for the LLM agent
- No pre-installed API keys or secrets

---

## Container Hardening

Every container spawned by the engine includes the following security flags:

```
--read-only                              # Read-only root filesystem
--cap-drop=ALL                           # Drop all Linux capabilities
--security-opt=no-new-privileges         # No privilege escalation via setuid/setgid
--pids-limit=256                         # Prevent fork bombs
--tmpfs /tmp:rw,noexec,size=256m         # Writable scratch via tmpfs (noexec)
--network=none                           # No outbound network access
--user <uid>:<gid>                       # Non-root execution
--cpus <cpu_limit>                       # CPU limit from config
--memory <mem_limit>                     # Memory limit from config
```

Writable paths are limited to:
- `/workspace/staging/` -- Meeseeks output
- `/workspace/ipc/output/` -- IPC responses
- `/tmp` -- Scratch space (tmpfs, noexec)

An optional seccomp profile (`docker/axiom-seccomp.json`) further restricts syscall access.

---

## Volume Mounts

| Host Path | Container Path | Mode | Purpose |
|-----------|---------------|------|---------|
| `.axiom/containers/specs/<task-id>/` | `/workspace/spec/` | read-only | TaskSpec or ReviewSpec input |
| `.axiom/containers/staging/<task-id>/` | `/workspace/staging/` | read-write | Meeseeks output staging area |
| `.axiom/containers/ipc/<task-id>/input/` | `/workspace/ipc/input/` | read-write | Engine-to-container messages |
| `.axiom/containers/ipc/<task-id>/output/` | `/workspace/ipc/output/` | read-write | Container-to-engine messages |

The project filesystem is **never** mounted into any container.

---

## Container Lifecycle

**File:** `internal/container/manager.go`

### Spawning

```
Engine receives dispatch request
    |
    v
Create host directories for specs, staging, IPC
    |
    v
Write TaskSpec/ReviewSpec to spec directory
    |
    v
docker run --rm with hardening flags + volume mounts
    |
    v
Log to container_sessions table + emit container_spawned event
```

### Naming Convention

Pattern: `axiom-<task-id>-<timestamp>`

Example: `axiom-task-042-1710600000`

### Concurrency Control

- Maximum parallel Meeseeks enforced by `max_meeseeks` config (default 10)
- When at limit, spawn requests queue until a slot opens
- BitNet tasks count against a separate local resource limit
- Active container count tracked in real-time

### Timeout Enforcement

- Each container has a hard timeout (default 30 minutes, configurable)
- Background goroutine monitors container age
- Containers exceeding timeout are killed immediately
- Timeout kills logged as events with `exit_reason = "timeout"`

### Destruction

Containers are destroyed when:
- Task completes (success or final failure)
- Task is retried (fresh container for every attempt)
- Container timeout expires
- Project is cancelled
- Engine shuts down

### Orphan Cleanup

On engine startup:
1. Find all `axiom-*` Docker containers from previous sessions
2. Destroy each one
3. Update `container_sessions` table: mark orphaned sessions with `exit_reason = "orphaned"`

---

## The Meeseeks Principle

Every retry gets a **fresh container**. No container reuse between attempts. This aligns with the Meeseeks principle: born, complete task, die. No state accumulation.

- **Retry**: Engine destroys current container, spawns new one with updated TaskSpec (original spec + failure feedback). Max 3 retries at same tier.
- **Escalation**: Engine destroys current container, spawns new one with higher-tier model. Max 2 escalations.

Feedback from prior attempts is included in the **new TaskSpec** as structured context, not carried as implicit container state.

---

## Scope Expansion

During execution, a Meeseeks may discover it needs files outside its declared scope. Rather than silently failing:

1. Meeseeks sends `request_scope_expansion` IPC message
2. Engine checks if requested files are locked by another task
3. If available: acquires locks, notifies orchestrator for approval
4. If approved: Meeseeks continues with expanded scope
5. If locked: engine destroys Meeseeks, marks task as `waiting_on_lock`, re-queues when locks release with expanded scope in the new TaskSpec

This avoids mid-flight denial (LLMs handle that poorly) by cleanly restarting with the expanded scope from the beginning.

---

## Validation Sandbox

A specialized container class for running automated checks against untrusted code:

- Read-only project snapshot at HEAD + writable overlay with Meeseeks output
- Same hardening as Meeseeks containers plus higher resource limits
- Same language-specific image (toolchain versions match exactly)
- Runs sequentially: dependency install, compilation, linting, unit tests, security scan (optional)
- No network, no secrets
- Destroyed after checks complete

### Warm Sandbox Pools (Experimental)

Pre-warmed validation containers synced to current HEAD for reduced latency:
- Push file diffs to warm sandbox instead of cold-starting
- Incremental build/test instead of full build
- Overlay filesystem fully reset between runs
- Periodic cold validation every N warm runs (default 10)
- Behind feature flag: `warm_pool_enabled = false` by default
