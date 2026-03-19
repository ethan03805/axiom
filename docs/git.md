# Merge Queue & Git Integration

Axiom uses a serialized merge queue to ensure every commit is validated against the actual current state of the project. All git operations are performed exclusively by the Trusted Engine.

---

## Branch Strategy

On project start, the engine creates a dedicated branch:

```
axiom/<project-slug>
```

Example: `axiom/my-project`

All task commits go to this branch. The user's current branch is **never modified** during execution. When the user is satisfied with the result, they merge the branch themselves.

The engine refuses to start on a dirty git working tree. Uncommitted changes must be committed or stashed first.

---

## Merge Queue

**File:** `internal/merge/queue.go`

The merge queue processes approved outputs one at a time (serialized). This prevents stale-context conflicts that arise from parallel task execution.

### Processing Steps

```
1. Receive approved output from approval pipeline
2. Validate base_snapshot against current HEAD
3. If stale: attempt three-way merge
4. If merge conflicts: re-queue task with updated context
5. If clean merge: apply output to working copy of HEAD
6. Run integration checks in validation sandbox:
   - Full build
   - Full test suite
   - Linting
7. If integration fails: revert, re-queue task with failure details
8. If integration passes: commit to project branch
9. Re-index project via semantic indexer
10. Release write-set locks for the task's target files
11. Unblock dependent tasks (waiting_on_lock -> queued)
12. Process next item in queue
```

---

## Base Snapshot Pinning

Every TaskSpec includes a `base_snapshot` field -- the git SHA of the project state it was generated against. This SHA is recorded in the `task_attempts` table.

When the merge queue processes an output:

1. Compare the output's `base_snapshot` against current HEAD
2. If they match: proceed directly
3. If HEAD has advanced (another task committed since this TaskSpec was generated):
   - Attempt a clean three-way merge
   - If clean: proceed with integration checks
   - If conflicting: re-queue the task with updated base snapshot and fresh semantic index context

---

## Commit Protocol

After output passes integration checks, the engine commits with this format:

```
[axiom] Implement JWT authentication middleware

Task: task-042
SRS Refs: FR-001, AC-003
Meeseeks Model: anthropic/claude-sonnet-4
Reviewer Model: openai/gpt-4o
Attempt: 2/3
Cost: $0.0234
Base Snapshot: abc123d
```

Each commit message includes:
- Task title
- Task ID
- SRS requirement references for traceability
- Models used for implementation and review
- Which attempt succeeded
- Cost of the successful attempt
- Base snapshot the output was validated against

---

## Integration Checks

Before every commit, the merge queue runs project-wide integration checks in a validation sandbox:

1. **Full build** -- Language-appropriate compilation
2. **Full test suite** -- All existing tests must pass
3. **Linting** -- Project-configured linters

If integration fails:
1. The commit does **not** proceed
2. The task is re-queued with integration failure details in an updated TaskSpec
3. A new Meeseeks is spawned to resolve the conflict
4. If the conflict persists after escalation, the orchestrator restructures affected tasks

---

## Lock Release and Task Unblocking

After a successful commit:

1. Write-set locks for the task's target files are released
2. The engine queries for `waiting_on_lock` tasks blocked by those files
3. Those tasks transition back to `queued`
4. The engine checks for `queued` tasks whose dependencies are all `done`
5. Ready tasks are dispatched to the work queue

This cascading unblock is what enables parallel execution to progress efficiently.

---

## Conflict Handling

**File:** `internal/merge/conflict.go`

When a task's base snapshot is stale (HEAD has advanced):

### Three-Way Merge

The engine attempts a three-way merge:
- Base: the task's `base_snapshot`
- Ours: current HEAD
- Theirs: the Meeseeks output

If the merge is clean (no conflicting hunks), the merged result proceeds through integration checks.

### Re-Queue on Conflict

If the merge has conflicts:
1. The task is re-queued with status `queued`
2. The new TaskSpec includes:
   - Updated base snapshot (current HEAD)
   - Fresh context from the semantic indexer reflecting recent commits
   - The merge conflict details as feedback
3. A fresh Meeseeks is spawned to produce updated output

---

## Project Completion

When all tasks reach `done`:

1. The orchestrator generates a final status report
2. The user reviews the full diff: `git diff main..axiom/<slug>`
3. The user merges when satisfied: `git merge axiom/<slug>`

Axiom never automatically merges to main or pushes to remote repositories.
