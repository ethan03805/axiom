# Database Schema

Axiom uses SQLite in WAL (Write-Ahead Logging) mode for all project state. The database file is at `.axiom/axiom.db`.

---

## Database Configuration

```go
db.Exec("PRAGMA journal_mode=WAL")    // Concurrent reads without blocking writes
db.Exec("PRAGMA busy_timeout=5000")   // 5-second retry on lock contention
db.SetMaxOpenConns(10)                  // Connection pool size
```

---

## Migration

The schema is defined in `schemas/001_initial.sql` and embedded in the Go binary. Migrations run automatically on first database open.

---

## Tables

### tasks

The core task tree. Each row is a task in the project.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | Task identifier (e.g., "task-042") |
| `parent_id` | TEXT FK | Parent task (for hierarchy) |
| `title` | TEXT | Human-readable task title |
| `description` | TEXT | Detailed task description |
| `status` | TEXT | Current state (queued, in_progress, in_review, done, failed, blocked, waiting_on_lock, cancelled_eco) |
| `tier` | TEXT | Model tier (local, cheap, standard, premium) |
| `task_type` | TEXT | implementation, test, or review |
| `base_snapshot` | TEXT | Git SHA this task was planned against |
| `eco_ref` | TEXT FK | ECO that cancelled this task (if cancelled_eco) |
| `created_at` | DATETIME | Creation timestamp |
| `completed_at` | DATETIME | Completion timestamp |

### task_srs_refs

Junction table linking tasks to SRS requirements for traceability.

| Column | Type | Description |
|--------|------|-------------|
| `task_id` | TEXT FK | Task identifier |
| `srs_ref` | TEXT | SRS reference (e.g., "FR-001", "AC-003") |

### task_dependencies

Dependency graph between tasks.

| Column | Type | Description |
|--------|------|-------------|
| `task_id` | TEXT FK | Dependent task |
| `depends_on` | TEXT FK | Task it depends on |

### task_target_files

Files each task is allowed to modify, with lock scope.

| Column | Type | Description |
|--------|------|-------------|
| `task_id` | TEXT FK | Task identifier |
| `file_path` | TEXT | Target file path |
| `lock_scope` | TEXT | Lock granularity: file, package, module, schema |

### task_locks

Active write-set locks preventing concurrent modification.

| Column | Type | Description |
|--------|------|-------------|
| `resource_type` | TEXT | Lock scope: file, package, module, schema |
| `resource_key` | TEXT | Canonical identifier (file path, package name) |
| `task_id` | TEXT FK | Task holding the lock |
| `locked_at` | DATETIME | When the lock was acquired |

Primary key: `(resource_type, resource_key)`

### task_attempts

Individual execution attempts preserving full retry/escalation history.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `task_id` | TEXT FK | Parent task |
| `attempt_number` | INTEGER | Attempt sequence (1, 2, 3...) |
| `model_id` | TEXT | Model used (e.g., "anthropic/claude-sonnet-4") |
| `model_family` | TEXT | Model family (anthropic, openai, meta, local) |
| `base_snapshot` | TEXT | Git SHA for this attempt |
| `status` | TEXT | running, passed, failed, escalated |
| `input_tokens` | INTEGER | Tokens in prompt |
| `output_tokens` | INTEGER | Tokens in response |
| `cost_usd` | REAL | Cost of this attempt |
| `failure_reason` | TEXT | Why it failed (if applicable) |
| `feedback` | TEXT | Feedback given for retry |
| `started_at` | DATETIME | Start time |
| `completed_at` | DATETIME | End time |

### validation_runs

Automated check results per attempt.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `attempt_id` | INTEGER FK | Parent attempt |
| `check_type` | TEXT | compile, lint, test, security |
| `status` | TEXT | pass, fail, skip |
| `output` | TEXT | Error output if failed |
| `duration_ms` | INTEGER | Check duration |
| `timestamp` | DATETIME | When the check ran |

### review_runs

Reviewer evaluation results per attempt.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `attempt_id` | INTEGER FK | Parent attempt |
| `reviewer_model` | TEXT | Model used for review |
| `reviewer_family` | TEXT | Model family |
| `verdict` | TEXT | approve or reject |
| `feedback` | TEXT | Reviewer feedback |
| `cost_usd` | REAL | Review cost |
| `timestamp` | DATETIME | When the review completed |

### task_artifacts

File artifacts produced by each attempt, for audit.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `attempt_id` | INTEGER FK | Parent attempt |
| `file_path` | TEXT | Output file path |
| `operation` | TEXT | add, modify, delete, rename |
| `sha256` | TEXT | File content hash |
| `size_bytes` | INTEGER | File size |
| `timestamp` | DATETIME | When the artifact was created |

### container_sessions

Container lifecycle tracking.

| Column | Type | Description |
|--------|------|-------------|
| `id` | TEXT PK | Container name (axiom-task-042-timestamp) |
| `task_id` | TEXT FK | Associated task |
| `container_type` | TEXT | meeseeks, reviewer, validator, sub_orchestrator |
| `image` | TEXT | Docker image used |
| `model_id` | TEXT | Model assigned |
| `cpu_limit` | REAL | CPU cores allocated |
| `mem_limit` | TEXT | Memory limit |
| `started_at` | DATETIME | When the container started |
| `stopped_at` | DATETIME | When the container stopped |
| `exit_reason` | TEXT | completed, timeout, killed, error, orphaned |

### events

Complete audit trail of every action in the project.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `event_type` | TEXT | Event type constant (see event types) |
| `task_id` | TEXT | Associated task (if applicable) |
| `agent_type` | TEXT | orchestrator, sub_orchestrator, meeseeks, reviewer, engine |
| `agent_id` | TEXT | Agent identifier |
| `details` | TEXT | JSON payload with event-specific data |
| `timestamp` | DATETIME | When the event occurred |

### cost_log

Cost tracking at all granularities.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `task_id` | TEXT FK | Associated task |
| `attempt_id` | INTEGER FK | Associated attempt |
| `agent_type` | TEXT | meeseeks, reviewer, sub_orchestrator, orchestrator |
| `model_id` | TEXT | Model used |
| `input_tokens` | INTEGER | Tokens in prompt |
| `output_tokens` | INTEGER | Tokens in response |
| `cost_usd` | REAL | Cost in USD |
| `timestamp` | DATETIME | When the request was made |

### eco_log

Engineering Change Order records.

| Column | Type | Description |
|--------|------|-------------|
| `id` | INTEGER PK | Auto-increment ID |
| `eco_code` | TEXT | Category code (ECO-DEP, ECO-API, ECO-SEC, ECO-PLT, ECO-LIC, ECO-PRV) |
| `category` | TEXT | Human-readable category name |
| `description` | TEXT | Description of the environmental issue |
| `affected_refs` | TEXT | JSON array of affected SRS references |
| `proposed_change` | TEXT | Proposed substitute |
| `status` | TEXT | proposed, approved, rejected |
| `approved_by` | TEXT | "user" or "claw:<identity>" |
| `created_at` | DATETIME | When the ECO was filed |
| `resolved_at` | DATETIME | When the ECO was approved/rejected |

---

## Semantic Index Tables

Additional tables used by the semantic indexer:

| Table | Purpose |
|-------|---------|
| `idx_symbols` | Function, type, interface, constant, variable definitions |
| `idx_fields` | Struct and type fields |
| `idx_imports` | Import relationships between files and packages |
| `idx_dependencies` | Package-level dependency graph |

---

## Entity Relationship Summary

```
tasks
  +--< task_srs_refs (many-to-many with SRS requirements)
  +--< task_dependencies (self-referencing many-to-many)
  +--< task_target_files
  +--< task_locks
  +--< task_attempts
  |       +--< validation_runs
  |       +--< review_runs
  |       +--< task_artifacts
  +--< container_sessions
  +--< events
  +--< cost_log
  +--< eco_log (via eco_ref)
```
