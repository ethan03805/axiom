# Approval Pipeline

No file reaches the project filesystem without passing through the complete five-stage approval pipeline. All pipeline operations are executed by the Trusted Engine.

---

## Pipeline Stages

```
Meeseeks Output (/workspace/staging/)
        |
        v
+-------------------+
| Stage 1: Extract  |  Manifest validation, path safety
| & Validate        |  Scope compliance, size limits
+--------+----------+
         |
         v
+-------------------+
| Stage 2:          |  Compile, lint, test in
| Validation        |  isolated Docker container
| Sandbox           |  No network, no secrets
+--------+----------+
         |
         | (if fail: retry with feedback, max 3)
         v
+-------------------+
| Stage 3:          |  Different model family evaluates
| Reviewer          |  output against TaskSpec
| Evaluation        |
+--------+----------+
         |
         | (if reject: fresh Meeseeks + fresh reviewer)
         v
+-------------------+
| Stage 4:          |  Orchestrator validates
| Orchestrator      |  against SRS requirements
| Validation        |
+--------+----------+
         |
         | (if reject: fresh Meeseeks)
         v
+-------------------+
| Stage 5:          |  Validate base snapshot,
| Merge Queue       |  integration checks, commit
+-------------------+
```

---

## Stage 1: Extraction & Manifest Validation

**File:** `internal/pipeline/manifest.go`, `internal/pipeline/router.go`

The engine extracts files from `/workspace/staging/<task-id>/` and validates the manifest:

| Check | Description |
|-------|-------------|
| File existence | All files listed in manifest exist in staging |
| No extras | No files in staging are unlisted in manifest |
| Path safety | All paths canonicalized, no `..` traversal, no symlinks |
| No special files | Reject device files, FIFOs |
| Size limits | Reject files exceeding max size (default 1MB) |
| Scope compliance | All paths within the task's declared `target_files` |
| Expanded scope | Expanded-scope files validated if scope expansion was approved |
| Binary handling | Binary files have `binary: true` flag and `size_bytes` |

Invalid manifests produce clear rejection messages and the Meeseeks output is discarded.

---

## Stage 2: Validation Sandbox

The engine spawns a validation sandbox container with:
- Read-only snapshot of the project at current HEAD
- Writable overlay with Meeseeks output applied
- No network access, no secrets, resource-limited

The sandbox runs checks sequentially:

1. **Dependency install** -- From lockfile only (no network)
2. **Compilation** -- Language-appropriate: `go build`, `tsc`, etc.
3. **Linting** -- Language-appropriate: `golangci-lint`, `eslint`, `ruff`, etc.
4. **Unit tests** -- Existing tests only (new tests come from separate test-generation tasks)
5. **Security scan** -- Optional, if configured

Binary files (manifest `binary: true`) skip compilation and linting but still enforce size limits and path validity.

### On Failure

- Errors are packaged as structured feedback
- Current Meeseeks container is destroyed
- A **fresh** Meeseeks container is spawned with a new TaskSpec containing the original spec + failure details
- Max 3 retries at the same model tier before escalation

### Language-Specific Profiles

| Profile | Dependency Strategy |
|---------|-------------------|
| Go | Vendored modules or read-only GOMODCACHE |
| Node | `npm ci --ignore-scripts` + read-only node_modules cache |
| Python | Pre-built wheels, `pip install --no-index --find-links` |
| Rust | Cargo with pre-populated registry |

---

## Stage 3: Reviewer Evaluation

The engine spawns a reviewer container with a ReviewSpec containing:
- The original TaskSpec
- The Meeseeks output + manifest
- Validation sandbox results

### ReviewSpec Format

```markdown
# ReviewSpec: task-042

## Original TaskSpec
<Complete TaskSpec>

## Meeseeks Output
<All output files + manifest>

## Automated Check Results
  Compilation: PASS
  Linting: PASS (0 errors, 2 warnings)
  Unit Tests: PASS (12/12)

## Review Instructions
Evaluate against TaskSpec acceptance criteria.
Check for: correctness, interface compliance, bugs, security issues, code quality.

### Verdict: APPROVE | REJECT

### Criterion Evaluation
- AC-001: PASS | FAIL -- <explanation>

### Feedback (if REJECT)
<Specific, actionable feedback with line numbers>
```

### Model Family Diversification

For standard and premium tier tasks, the reviewer is from a **different model family** than the Meeseeks. This prevents correlated blind spots.

Example: If Meeseeks used Claude Sonnet, the reviewer uses GPT-4o or an open-source model.

### On Rejection

- Current Meeseeks container is destroyed
- A fresh Meeseeks container is spawned with new TaskSpec + reviewer feedback
- Reviewer container is also destroyed; a new reviewer is spawned for the next round

### Batched Review for Trivial Tasks

Local-tier (BitNet) tasks may be batched into a single ReviewSpec if:
- All tasks in the batch are functionally related (same module)
- The batch is reviewed as a coherent unit
- If any task fails, the entire batch is returned for revision

---

## Stage 4: Orchestrator Validation

The orchestrator receives the approved output and validates it against SRS requirements via IPC.

If the orchestrator rejects:
- Current Meeseeks container is destroyed
- Fresh Meeseeks spawned with orchestrator feedback

---

## Stage 5: Merge Queue

Approved files are submitted to the serialized merge queue (see [Merge Queue & Git](git.md)):

1. Validate `base_snapshot` against current HEAD
2. If stale: attempt three-way merge or re-queue
3. Apply output to working copy of HEAD
4. Run integration checks in validation sandbox
5. If integration fails: revert, re-queue task
6. If integration passes: commit, update HEAD
7. Re-index via semantic indexer
8. Release write-set locks
9. Unblock dependent tasks

---

## Escalation Policy

| Stage | Failure Count | Action |
|-------|--------------|--------|
| Validation (Stage 2) | 1-3 | Retry with same tier, fresh container |
| Validation (Stage 2) | 4-6 | Escalate model tier (max 2) |
| Validation (Stage 2) | 7+ | Mark task `blocked` |
| Review (Stage 3) | 1-3 | Retry with fresh Meeseeks + reviewer |
| Review (Stage 3) | 4-6 | Escalate model tier |
| Review (Stage 3) | 7+ | Mark task `blocked` |
| Merge (Stage 5) | Any | Re-queue with updated context |

---

## Risky File Escalation

Certain file types always require standard-tier or higher review, regardless of task tier:

- CI/CD configuration (`.github/workflows/`, `Jenkinsfile`)
- Package manifests (`package.json`, `go.mod`, `requirements.txt`)
- Authentication and authorization code
- Security-related code (encryption, hashing, tokens)
- Infrastructure definitions (Dockerfile, Terraform)
- Build scripts and Makefiles
- Database migration files
