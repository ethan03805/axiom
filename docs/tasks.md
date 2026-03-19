# Task System

The task system manages the hierarchical tree of work that Axiom decomposes from the SRS. It handles task creation, dependency resolution, write-set locking, scope expansion, and the state machine governing task lifecycle.

---

## Task Tree

The orchestrator decomposes the approved SRS into a hierarchical task tree stored in SQLite. Each leaf task is an atomic unit of work assigned to a single Meeseeks.

```
Root Task (project-level)
  +-- Backend Module
  |     +-- Database Schema (task-001, local tier)
  |     +-- User Model (task-002, cheap tier)
  |     +-- Auth Handler (task-003, standard tier)
  |     +-- API Routes (task-004, standard tier)
  |     +-- Tests for Auth (task-005, standard tier, different model family)
  +-- Frontend Module
        +-- Login Page (task-006, standard tier)
        +-- Dashboard (task-007, premium tier)
```

---

## Task States

```
queued --> in_progress --> in_review --> done
               |              |
               +-> failed     +-> failed
               |     |        |     |
               |     v        |     v
               |   queued     |   queued    (retry/escalation)
               |              |
               +-> blocked    +-> blocked
               |
               +-> waiting_on_lock --> queued  (when locks release)

Any active state --> cancelled_eco  (when ECO invalidates the task)
```

| State | Description |
|-------|-------------|
| `queued` | Waiting for dependencies + available Meeseeks slot |
| `in_progress` | Meeseeks container actively executing |
| `in_review` | Output passed validation, being reviewed |
| `done` | Output committed to project branch |
| `failed` | Failed validation or review, awaiting retry |
| `blocked` | Exhausted all retries and escalations |
| `waiting_on_lock` | Needs files locked by another task |
| `cancelled_eco` | Invalidated by an Engineering Change Order |

---

## TaskSpec Format

Every Meeseeks receives a self-contained TaskSpec:

```markdown
# TaskSpec: task-042

## Base Snapshot
git_sha: abc123def456

## Objective
Implement the JWT authentication middleware for the Gin framework.

## Context

### Symbol Context (tier: symbol)
func GenerateToken(userID string) (string, error)
type Claims struct { UserID string; jwt.StandardClaims }

### File Context (tier: file)
// src/config/config.go (complete file)
...

## Interface Contract
func AuthMiddleware() gin.HandlerFunc
// Must validate JWT from Authorization header
// Must set userID in gin.Context

## Constraints
- Language: Go 1.22
- Framework: Gin v1.9
- Dependencies: golang-jwt/jwt/v5
- Max file length: 500 lines

## Acceptance Criteria
- [ ] Middleware extracts JWT from Authorization: Bearer header
- [ ] Invalid/expired tokens return 401
- [ ] Valid tokens set userID in context
- [ ] Unit tests cover valid, expired, and missing token cases

## Output Format
Write all output files to /workspace/staging/
Include a manifest.json describing all file operations.
```

---

## Output Manifest

Every Meeseeks produces a `manifest.json` alongside its code:

```json
{
    "task_id": "task-042",
    "base_snapshot": "abc123def",
    "files": {
        "added": [
            {"path": "src/middleware/auth.go", "binary": false}
        ],
        "modified": [
            {"path": "src/routes/api.go", "binary": false}
        ],
        "deleted": ["src/handlers/old_auth.go"],
        "renamed": [
            {"from": "src/utils/hash.go", "to": "src/crypto/hash.go"}
        ]
    }
}
```

The manifest enables:
- File deletions and renames (not possible via raw filesystem output)
- Early write-set conflict detection
- Scope validation (Meeseeks only touched declared files)
- Binary file handling (skip compile/lint, enforce size limits)

---

## Write-Set Locking

When the engine dispatches a task, it acquires write-set locks for the task's target files:

```sql
-- task_locks table
resource_type | resource_key     | task_id  | locked_at
file          | src/auth/jwt.go  | task-042 | 2026-03-19T...
file          | src/routes/api.go| task-042 | 2026-03-19T...
```

### Rules

| Rule | Behavior |
|------|----------|
| Acquisition | All locks acquired atomically (all-or-nothing) |
| Ordering | Locks acquired in alphabetical order by path (prevents deadlocks) |
| Conflict | If any file is locked, the task stays `queued` until all locks are available |
| Release | Locks released after merge queue commits or task fails |
| Granularity | File-level by default; package/module/schema levels available |

### Lock Scope Escalation

| Trigger | Lock Scope |
|---------|------------|
| Task modifies implementation internals | File-level lock |
| Task modifies exported symbols/interfaces | Package-level lock |
| Task modifies API schemas or routes | Module-level lock |
| Task involves database migrations | Schema-level lock |

---

## Dependency Resolution

Tasks have explicit dependencies stored in the `task_dependencies` table:

```
task-005 (Auth Tests) depends_on task-003 (Auth Handler)
task-004 (API Routes)  depends_on task-002 (User Model)
```

A task cannot leave `queued` unless ALL tasks in its dependency set have status `done`.

Circular dependencies are detected at task creation time and rejected with an error.

---

## Test Authorship Separation

Tests are never written by the same Meeseeks that wrote the implementation:

1. Implementation task executes and produces code
2. Implementation passes validation and merges to HEAD
3. Test generation task spawns with a **different model family**
4. Test Meeseeks sees the committed implementation via semantic indexer
5. If generated tests fail, a follow-up implementation-fix task is created

A feature is not considered `done` until both implementation and tests converge (all tests green).

---

## Task Decomposition Principles

The orchestrator follows these principles:

1. **Appropriately sized** -- Small enough for the model tier, large enough for code coherence
2. **Independent** -- Minimize dependencies between tasks
3. **Traceable** -- Every task references one or more SRS requirements (FR-001, AC-003)
4. **Test-separated** -- Test tasks are separate from implementation, different model family
5. **Coherence-preserving** -- Interconnected code (e.g., multi-endpoint API) stays as one task

---

## Retry and Escalation

| Tier | Action | Limit |
|------|--------|-------|
| **Retry** | Destroy Meeseeks, spawn fresh container with feedback | Max 3 at same tier |
| **Escalation** | Destroy Meeseeks, spawn fresh container with higher-tier model | Max 2 escalations |
| **Block** | Mark task blocked, notify orchestrator | Orchestrator restructures or cancels |

Every retry and escalation spawns a fresh container. Feedback from prior attempts is structured context in the new TaskSpec, not implicit state.
