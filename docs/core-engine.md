# Core Engine

The core engine is the Go control plane that performs all privileged operations in Axiom. It is the single source of truth for project state and the sole authority for filesystem writes, git commits, container spawning, inference brokering, and budget enforcement.

---

## Coordinator

**File:** `internal/engine/coordinator.go` (442 lines)

The Coordinator is the central struct that wires all 13 subsystems together:

```go
type Coordinator struct {
    Config         *Config
    State          *state.Store
    ContainerMgr   *container.Manager
    IPCHandler     *ipc.Handler
    Broker         *broker.Broker
    Pipeline       *pipeline.Pipeline
    MergeQueue     *merge.Queue
    Indexer        *index.Indexer
    Budget         *budget.Enforcer
    Registry       *registry.Registry
    Security       *security.Scanner
    Events         *events.Emitter
    Git            *git.Manager
}
```

### Lifecycle

1. **`New(config)`** -- Constructor initializes all subsystems in dependency order
2. **`Start()`** -- Opens the SQLite database, runs migrations, starts the event loop, runs crash recovery
3. **`Shutdown()`** -- Graceful shutdown: kills active containers, flushes state, closes the database

### Crash Recovery

On startup, the Coordinator runs a 5-step crash recovery procedure:

1. **Kill orphaned containers** -- Find and destroy any `axiom-*` Docker containers from previous sessions
2. **Reconcile task state** -- Reset `in_progress` or `in_review` tasks with no running container back to `queued`
3. **Release stale locks** -- Remove write-set locks held by dead containers
4. **Clean staging** -- Remove staged files that were never committed
5. **Verify SRS integrity** -- Check SHA-256 hash of `.axiom/srs.md` against stored hash

---

## Configuration

**File:** `internal/engine/config.go`

Configuration is loaded from two TOML files, with project config overriding global config:

1. `~/.axiom/config.toml` -- Global defaults
2. `.axiom/config.toml` -- Project-specific settings

The `Config` struct maps all sections defined in the TOML files:

```go
type Config struct {
    Project       ProjectConfig
    Budget        BudgetConfig
    Concurrency   ConcurrencyConfig
    Orchestrator  OrchestratorConfig
    BitNet        BitNetConfig
    Docker        DockerConfig
    Validation    ValidationConfig
    Security      SecurityConfig
    Git           GitConfig
    API           APIConfig
    Observability ObservabilityConfig
}
```

Configuration is validated at load time. Required fields are checked, and sensible defaults are applied for optional fields.

Configuration can be reloaded at runtime via `axiom config reload`, which re-reads both files and updates in-memory values without restarting the engine.

---

## State Layer

**Directory:** `internal/state/`

All project state is persisted in SQLite at `.axiom/axiom.db` in WAL (Write-Ahead Logging) mode.

### Database Initialization (`db.go`)

```go
db.Exec("PRAGMA journal_mode=WAL")
db.Exec("PRAGMA busy_timeout=5000")
db.SetMaxOpenConns(10)
```

WAL mode enables concurrent reads without blocking writes. The busy timeout prevents immediate failures on lock contention. Connection pooling is set to 10.

Migrations are embedded in the Go binary from `schemas/001_initial.sql` and run on first database open.

### Task Operations (`tasks.go`)

Full CRUD for the tasks table:
- **Create** -- Single task or batch creation (atomic)
- **Get** -- By ID, with optional joins for dependencies, target files, SRS refs
- **List** -- By status, by parent, by dependency readiness
- **Update** -- Status transitions with state machine enforcement
- **Bulk operations** -- `create_task_batch` for atomic multi-task insertion

State machine enforcement rejects invalid transitions:
- Valid: `queued -> in_progress -> in_review -> done`
- Side paths: `in_progress -> failed -> queued`, `in_progress -> blocked`, `in_progress -> waiting_on_lock -> queued`
- Special: `cancelled_eco` from any active state

Dependency enforcement: a task cannot leave `queued` unless all tasks in its dependency set have status `done`. Circular dependencies are detected at creation time.

### Event Logging (`events.go`)

Insert events with type, task_id, agent_type, agent_id, and JSON details. Query events with type and time filtering. Events are the complete audit trail.

### Cost Tracking (`costs.go`)

Insert cost records and query aggregations: per-task, per-model, per-agent-type, per-project total. Integrates with the Budget Enforcer for real-time budget checks.

### ECO Records (`eco.go`)

Insert, update, and query Engineering Change Order records. Track ECO status (proposed, approved, rejected) and link to affected tasks.

### Write-Set Locks (`locks.go`)

Atomic lock acquisition (all-or-nothing) using the `task_locks` table. Deterministic alphabetical ordering prevents deadlocks. Lock release on task completion or failure.

### Task Attempts (`attempts.go`)

Track individual execution attempts with model, tokens, cost, failure reason, and feedback. Preserve full retry and escalation history for audit.

---

## Event Emission System

**Directory:** `internal/events/`

### Event Types (`types.go`)

21+ event types defined as constants:

| Event Type | Description |
|-----------|-------------|
| `task_created` | New task added to tree |
| `task_started` | Meeseeks container spawned |
| `task_completed` | Task output committed |
| `task_failed` | Task failed validation or review |
| `task_blocked` | Retries and escalations exhausted |
| `container_spawned` | Docker container started |
| `container_destroyed` | Docker container stopped |
| `review_started` | Reviewer container spawned |
| `review_completed` | Review verdict received |
| `merge_started` | Merge queue processing item |
| `merge_completed` | Successful commit |
| `budget_warning` | Budget threshold reached |
| `budget_exhausted` | Budget fully consumed |
| `eco_proposed` | ECO submitted by orchestrator |
| `eco_approved` | ECO approved |
| `eco_rejected` | ECO rejected |
| `srs_submitted` | SRS submitted for approval |
| `srs_approved` | SRS approved, scope locked |
| `scope_expansion_requested` | Meeseeks requested additional files |
| `scope_expansion_approved` | Scope expansion granted |
| `scope_expansion_denied` | Scope expansion denied |
| `context_invalidation_warning` | Symbol changes affect active Meeseeks |
| `provider_unavailable` | OpenRouter connectivity lost |
| `crash_recovery` | Engine recovered from crash |

### Emitter (`emitter.go`)

In-process pub-sub event bus:

```go
type Emitter struct {
    subscribers     map[string][]func(Event)  // per-type subscribers
    allSubscribers  []func(Event)              // all-events subscribers
}
```

- `Subscribe(eventType, handler)` -- Register for specific event types
- `SubscribeAll(handler)` -- Register for all events
- `Emit(event)` -- Broadcast to matching subscribers asynchronously

Subscribers include: GUI (via Wails events), API WebSocket hub, internal state consumers.

---

## Doctor Command

**File:** `internal/doctor/doctor.go`

System health validation with pass/fail/warning results:

| Check | What It Validates |
|-------|-------------------|
| Docker | Daemon running, version compatible |
| BitNet | Server reachable (if enabled) |
| Network | OpenRouter API connectivity |
| Resources | CPU cores, available memory, disk space |
| Images | Configured Meeseeks image exists locally |
| Warm Pool | Warm pool images match project config |
| Secrets | Scanner regex patterns are valid |
