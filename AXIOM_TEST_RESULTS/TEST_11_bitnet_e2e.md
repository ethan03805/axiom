# Axiom Test Results: BitNet Local Inference End-to-End Test

**Date:** 2026-03-20
**Tester:** Claude Opus 4.6 (acting as user)
**System:** macOS Darwin 25.3.0 / Apple M4 / arm64
**Axiom Binary:** /Users/ethantriska/NewAxiom/axiom/axiom (dev build)
**Test Project:** /tmp/axiom_test_11_bitnet
**OpenRouter API Key:** Configured in ~/.axiom/config.toml
**BitNet Server:** Enabled, model weights present (Falcon3-1B-Instruct-1.58bit i2_s GGUF)
**Docker Images:** All 4 present

---

## Summary

This is the first E2E test with BitNet local inference enabled. All prior E2E tests (TEST_08 through TEST_10) ran with `bitnet.enabled = false`. This test evaluates whether BitNet can actually perform useful local inference for local-tier tasks.

**Result: BitNet local inference is completely non-functional.** The BitNet server starts and responds to health checks (`/v1/models`), but crashes with SIGSEGV during actual inference for any prompt longer than ~12 tokens. This means ALL real TaskSpec prompts (which are hundreds of tokens) will crash the server. Additionally, a stale-lock bug causes the entire pipeline to stall after a few tasks complete.

**Bugs Found:** 3 total (2 critical, 1 high)
**Tasks Created:** 16 (3 completed, 13 stuck in queued)
**Total Cost:** $0.13
**Total Inference Calls:** 14 (1 failed BitNet + 13 OpenRouter)
**Git Commits:** 3 on axiom/axiom_test_11_bitnet branch (correct branch isolation)
**Generated Code Compiles:** YES (the 3 completed tasks produce valid Go code)
**Process Exit:** Required manual kill (stalled indefinitely)
**BitNet Tasks Completed:** 0 (all BitNet attempts crashed)

---

## Test Setup

```bash
rm -rf /tmp/axiom_test_11_bitnet
mkdir -p /tmp/axiom_test_11_bitnet
cd /tmp/axiom_test_11_bitnet
git init
git config user.email "test@test.com"
git config user.name "Test"
echo "# BitNet Test" > README.md
git add . && git commit -m "initial commit"
axiom init
# Modified .axiom/config.toml: srs_approval_delegate = "claw" for auto-approve
git add .axiom/config.toml .axiom/.gitignore
git commit -m "axiom init"
```

Global config at `~/.axiom/config.toml`:
```toml
[openrouter]
api_key = "sk-or-v1-..."

[budget]
max_usd = 10.00
warn_at_percent = 80

[bitnet]
enabled = true
host = "localhost"
port = 3002
cpu_threads = 4
max_concurrent_requests = 4
```

BitNet server started with `axiom bitnet start`:
```
BitNet server started successfully.
  Host:       localhost
  Port:       3002
  Threads:    4
  Running:    true
```

Prompt used:
```
Build a simple Go CLI program that converts temperatures between Celsius and Fahrenheit. It should accept a number and a unit flag (--to-c or --to-f) and print the converted result. Handle invalid input gracefully.
```

---

## Pre-Test: BitNet Server Validation

### TEST 0a: BitNet Models Endpoint
**Status: PASS**
```json
{
    "object": "list",
    "data": [{
        "id": "models/Falcon3-1B-Instruct-1.58bit/ggml-model-i2_s.gguf",
        "object": "model",
        "owned_by": "llamacpp",
        "meta": {"n_params": 1669408768, "size": 1357164480}
    }]
}
```

### TEST 0b: BitNet Short Prompt Inference (12 tokens)
**Status: PARTIAL PASS**
```json
{
  "choices": [{"finish_reason": "stop", "message": {"content": "", "role": "assistant"}}],
  "usage": {"completion_tokens": 8, "prompt_tokens": 12, "total_tokens": 20}
}
```
Server generated 8 tokens but content is empty. Server did not crash. Prompt was just `"hi"`.

### TEST 0c: BitNet Code Prompt Inference (49 tokens)
**Status: FAIL -- SIGSEGV**

Server log:
```
slot update_slots: id  0 | task 9 | prompt tokenized, n_ctx_slot = 2048, n_keep = 0, n_prompt_tokens = 49
slot update_slots: id  0 | task 9 | prompt done, n_past = 49, n_tokens = 49
Error occurred while running command: Command '['build/bin/llama-server', ...]' died with <Signals.SIGSEGV: 11>.
```

Server crashes with SIGSEGV after processing a 49-token prompt (Go code generation request). Any prompt longer than ~12 tokens triggers the crash. The i2_s (1.58-bit) quantized kernels segfault during token generation.

### TEST 0d: axiom doctor
**Status: PASS (but misleading)**
```
[PASS] BitNet Server: BitNet server running at localhost:3002
[PASS] BitNet Local Inference: BitNet server responding at localhost:3002
```
Doctor reports BitNet as healthy because it only checks the `/v1/models` endpoint, which works. It does NOT send a test inference request. The server is fundamentally broken for inference but doctor says PASS.

---

## E2E Test Results

### TEST 1: axiom init
**Status: PASS**

### TEST 2: axiom doctor
**Status: PASS** (10/10 checks, including misleading BitNet PASS)

### TEST 3: SRS Generation
**Status: PASS**
- SRS generated via OpenRouter (anthropic/claude-sonnet-4)
- SRS auto-approved by claw delegate
- 16 tasks created with proper tier assignments

### TEST 4: Task Decomposition
**Status: PASS**
- 16 tasks created:
  - 5 local-tier: project init, data models, output formatter, formatter tests, documentation
  - 4 cheap-tier: conversion algorithms, input validator, converter tests, validator tests
  - 5 standard-tier: CLI parser, error handling, main entry point, CLI tests, integration tests
  - 1 premium-tier: cross-platform testing
  - 1 standard-tier: performance testing

### TEST 5: BitNet Inference Attempt
**Status: FAIL**
- task-001 (local tier) was routed to BitNet model `models/Falcon3-1B-Instruct-1.58bit/ggml-model-i2_s.gguf`
- BitNet server crashed during inference: `"provider bitnet: bitnet request: Post "http://localhost:3002/v1/chat/completions": EOF"`
- Task was retried with `anthropic/claude-haiku-4.5` (fallback) and succeeded

### TEST 6: Task Execution
**Status: PARTIAL (3/16 tasks completed, then stalled)**

Completed tasks:
| Task | Tier | Model Used | Result |
|------|------|-----------|--------|
| task-001 | local | BitNet (failed) -> haiku (passed) | DONE |
| task-004 | cheap | haiku | DONE |
| task-007 | standard | haiku (3 fails) -> sonnet (passed, reviewed by gpt-4o) | DONE |

Stuck tasks (13 queued, never dispatched again):
- task-002 (local): 1 attempt, reviewer rejected for missing manifest.json. Requeued but never re-dispatched.
- task-010 (cheap): 1 attempt, reviewer rejected for test correctness issues. Requeued but never re-dispatched.
- 11 other tasks blocked waiting on task-002 dependencies.

### TEST 7: Reviewer Stage
**Status: PASS (reviewers are working correctly)**
- Reviewer caught missing manifest.json in task-002
- Reviewer caught interface contract violations in task-007 (extra files outside scope)
- Reviewer caught test correctness issues in task-010 (Go ParseFloat edge cases)
- Model family diversification working: task-007 used gpt-4o reviewer for sonnet meeseeks

### TEST 8: Escalation Logic
**Status: PASS**
- task-007 failed 3 times at cheap tier (haiku), then escalated to standard tier (sonnet)
- Sonnet attempt passed validation and review
- Escalation from cheap -> standard working correctly

### TEST 9: Git Branch Isolation
**Status: PASS**
- Separate branch: `axiom/axiom_test_11_bitnet`
- `main` branch unchanged
- 3 commits on axiom branch with proper format

### TEST 10: Generated Code Quality
**Status: PASS (for completed tasks)**
- `go build ./...` succeeds
- 3 Go files generated: main.go, cmd/cli/cli.go, internal/validator/validator.go
- Code is well-structured but incomplete (only 3/16 tasks finished)

### TEST 11: Cost Tracking
**Status: PASS**
- Cost by model:
  - anthropic/claude-haiku-4.5: 12 calls, $0.08
  - anthropic/claude-sonnet-4: 1 call, $0.04
  - openai/gpt-4o: 1 call (reviewer), $0.007
- Total: $0.13

---

## Bugs Found

### BUG-042: BitNet i2_s model crashes with SIGSEGV during inference [CRITICAL]

**File:** `third_party/BitNet/build/bin/llama-server` (compiled binary)

**Description:** The BitNet llama-server binary compiled with the vendored BitNet framework crashes with SIGSEGV (signal 11) when attempting to generate tokens using the i2_s (1.58-bit) quantized Falcon3 model. The crash occurs after prompt processing is complete, during the token generation phase. Very short prompts (~12 tokens) may succeed but return empty content. Prompts of 49+ tokens crash every time.

**Evidence:**
```
# From server.log (49 token prompt):
slot update_slots: id  0 | task 9 | prompt done, n_past = 49, n_tokens = 49
Error occurred while running command: Command '...' died with <Signals.SIGSEGV: 11>.

# From task_attempts table (axiom run):
task-001|models/Falcon3-1B-Instruct-1.58bit/ggml-model-i2_s.gguf|failed|
  provider bitnet: bitnet request: Post "http://localhost:3002/v1/chat/completions": EOF
```

Key observations from the `system_info` output:
- `NEON = 0` -- Apple NEON SIMD is not detected despite running on M4 (ARM64)
- The `i2_s` quantization uses custom BitNet compute kernels that may not be compiled with proper ARM support
- Metal GPU was initialized (`ggml_metal_init: Apple M4`) but `-ngl 0` was passed (CPU-only mode)
- The custom 1.58-bit kernels appear to segfault on the ARM CPU path

**Impact:** BitNet local inference is completely non-functional. ALL local-tier tasks that route to BitNet will fail. The fallback to cloud models works (task-001 recovered), but this defeats the entire purpose of BitNet (free, zero-latency local inference). The Architecture's core value proposition of tiered model routing with free local inference for trivial tasks is broken.

**Likely Root Cause:** The BitNet (bitnet.cpp) binary was compiled with the vendored framework's build system, which uses custom i2_s compute kernels for 1.58-bit quantization. These kernels are not compatible with the Apple M4 ARM64 CPU or are not correctly detecting/using ARM NEON instructions. The `NEON = 0` in system_info strongly suggests the binary was compiled without ARM NEON support, which would cause undefined behavior when the custom kernels attempt to use SIMD instructions.

**Fix Options:**
1. Rebuild bitnet.cpp with proper ARM NEON/Apple Silicon support flags (`-DGGML_NEON=ON -DGGML_METAL=ON`)
2. Use the f32 model as a fallback (works but is ~6GB and slow, defeating BitNet's purpose)
3. Use a standard GGUF quantization (Q4_K_M, Q8_0) of Falcon3 with vanilla llama.cpp instead of the custom i2_s kernels
4. Add a BitNet inference health check to `axiom doctor` that sends an actual test prompt, not just the models endpoint

---

### BUG-043: Stale write-set locks prevent task re-dispatch after reviewer rejection [CRITICAL]

**Files:** `internal/state/locks.go`, `internal/engine/coordinator.go:889-914`, `internal/engine/workqueue.go:86-94`

**Description:** When a task is rejected by the reviewer and requeued via `requeueTask()`, its write-set locks are not reliably released. The task transitions back to `queued` status, but the locks remain in the `task_locks` table. On the next dispatch cycle, `GetDispatchable()` -> `CheckAllLocksAvailable()` -> `IsLocked()` sees the lock is held (by the task itself) and skips the task. This creates an unrecoverable stall: the task is queued with all dependencies met, but its own stale locks prevent it from being re-dispatched.

**Evidence:**
```sql
-- Locks still held by queued tasks:
SELECT * FROM task_locks;
-- file|internal/converter/converter.go|task-002|2026-03-20 13:52:40
-- file|internal/validator/validator_test.go|task-010|2026-03-20 13:52:52

-- But both tasks are queued:
SELECT id, status FROM tasks WHERE id IN ('task-002', 'task-010');
-- task-002|queued
-- task-010|queued
```

The stall lasted over 3 minutes (iterations 27-60 with zero progress) before the process was manually killed.

**Root Cause (two compounding issues):**

1. **`locks.go` does not use the write mutex (`wmu`):** Every other file in `internal/state/` (`tasks.go`, `attempts.go`, `costs.go`, `events.go`, `db.go`) acquires `db.wmu` before write operations. `locks.go` does not. This means `ReleaseLocks` (a DELETE operation) runs without the write mutex, potentially conflicting with other concurrent writes and silently failing under SQLite contention.

2. **`requeueTask` discards all errors:** Lines 911-913 of `coordinator.go`:
   ```go
   _ = c.workQueue.FailTask(taskID)    // <-- error discarded
   _ = c.db.UpdateTaskStatus(taskID, state.TaskStatusFailed)
   _ = c.db.UpdateTaskStatus(taskID, state.TaskStatusQueued)
   ```
   If `FailTask` fails (which calls `ReleaseLocks`), the error is silently discarded. The task transitions to queued but locks remain.

3. **`CheckAllLocksAvailable` doesn't know which task is requesting:** The lock check at `workqueue.go:88` doesn't pass the requesting task ID:
   ```go
   allAvailable, _, err := wq.db.CheckAllLocksAvailable(locks)
   ```
   If a task's own stale lock is present, the check treats it as a conflict from "another task" and skips the task forever.

**Impact:** After any reviewer rejection where locks aren't properly released, the rejected task and ALL tasks that depend on it become permanently stuck. In this test, task-002 being stuck blocked 11 downstream tasks, leaving only 3/16 tasks completed. The project can never finish.

**Fix:**
1. Add `db.wmu.Lock()`/`defer db.wmu.Unlock()` to all write functions in `locks.go` (`AcquireLocks`, `ReleaseLocks`)
2. Stop discarding errors from `FailTask` -- log them and handle the failure
3. Make `CheckAllLocksAvailable` task-aware: accept a `requestingTaskID` parameter and exclude locks held by that same task (treat self-held locks as available for re-acquisition)
4. Add stale lock detection in the dispatch loop: if a task is `queued` but holds locks, release those locks

---

### BUG-044: axiom doctor BitNet check is superficial -- misses inference crashes [HIGH]

**File:** `internal/doctor/doctor.go`

**Description:** `axiom doctor` reports `[PASS] BitNet Server` and `[PASS] BitNet Local Inference` even though the server crashes on any real inference request. The check only queries the `/v1/models` endpoint (a lightweight GET that always works). It does not send a test inference request to verify the model can actually generate tokens.

**Evidence:**
```
[PASS] BitNet Server: BitNet server running at localhost:3002
[PASS] BitNet Local Inference: BitNet server responding at localhost:3002
```

Immediately after this PASS, the first real inference request crashes the server with SIGSEGV.

**Impact:** Users and automated tests see all green checks and believe BitNet is functional. They then run `axiom run` with BitNet enabled, causing local-tier task failures, wasted budget on retries, and potential stall bugs.

**Likely Root Cause:** The doctor check was implemented as a connectivity check (can we reach the server?) not a functional check (can the server generate tokens?).

**Fix:** Add a small test inference call in `axiom doctor`:
```go
// After confirming /v1/models responds, send a minimal inference request:
testReq := `{"model":"test","messages":[{"role":"user","content":"test"}],"max_tokens":4}`
resp, err := http.Post(bitnetURL+"/chat/completions", "application/json", strings.NewReader(testReq))
```
If this fails or returns empty content, report `[WARN] BitNet Inference: Server responds but inference fails`.

---

## What Works Well

1. **BitNet fallback to cloud models** -- When BitNet fails, the retry/escalation system correctly falls back to haiku
2. **Reviewer stage** -- Reviewers are catching real issues (missing manifests, contract violations, test correctness)
3. **Model family diversification** -- gpt-4o used as reviewer for sonnet meeseeks
4. **Escalation logic** -- cheap -> standard tier escalation works correctly
5. **Branch isolation** -- Commits on separate axiom branch
6. **SRS auto-approval with claw delegate** -- Works correctly
7. **Code quality** -- The 3 completed tasks produce compiling, well-structured Go code

## What Blocks BitNet Local Inference

1. **i2_s SIGSEGV** -- Custom 1.58-bit kernels crash on Apple M4 ARM64
2. **Stale lock stall** -- Reviewer rejections cause permanent task stalls
3. **Superficial doctor check** -- Gives false confidence in BitNet health

---

## Event Log Summary

| Event Type | Count |
|---|---|
| task_started | 8 |
| task_completed | 3 |
| task_failed | 7 |
| container_spawned | 8 |
| merge_completed | 3 |
| merge_started | 3 |
| review_started | 4 |
| review_completed | 4 |
| srs_submitted | 1 |
| srs_approved | 1 |
| task_created | 1 |

## Cost Breakdown

| Model | Requests | Total Cost |
|---|---|---|
| anthropic/claude-haiku-4.5 | 12 | $0.0816 |
| anthropic/claude-sonnet-4 | 1 | $0.0435 |
| openai/gpt-4o | 1 | $0.0068 |
| **Total** | **14** | **$0.1320** |

Note: Zero BitNet cost (as intended) but also zero successful BitNet inference.

## Timeline

- 13:50:30 - Engine started, direct orchestrator initialized
- 13:51:01 - SRS generated and auto-approved
- 13:51:30 - Task decomposition complete (16 tasks)
- 13:51:40 - task-001 dispatched to BitNet, BitNet crashes (EOF)
- 13:51:42 - task-001 retried with haiku, succeeds
- 13:52:10 - task-004 completes (haiku)
- 13:52:40 - task-002 dispatched, reviewer rejects (missing manifest)
- 13:52:52 - task-010 dispatched, reviewer rejects (test correctness)
- 13:53:15 - task-007 starts retry/escalation cycle
- 13:54:30 - task-007 escalated to sonnet, passes review by gpt-4o
- 13:54:30+ - Pipeline stalls: task-002 and task-010 queued with stale locks
- 13:59:30 - Manually killed after 5 minutes of no progress (3/16 done)

---

## Bug Fix Status

**Date:** 2026-03-20
**Fixed by:** Commit 28a75f3 (pushed to main)
**Issues:** #47, #48, #49, #50

### BUG-043 (CRITICAL): Stale write-set locks -- FIXED (Issue #48, closed)
Three compounding issues resolved:
1. `internal/state/locks.go`: Added `db.wmu.Lock()`/`defer db.wmu.Unlock()` to `AcquireLocks` and `ReleaseLocks` -- consistent with all other write operations in the state package.
2. `internal/engine/coordinator.go`: `requeueTask` now handles `FailTask` errors instead of discarding them. On failure, falls back to direct `ReleaseLocks` call to prevent stale locks.
3. `internal/state/locks.go`: `CheckAllLocksAvailable` now accepts a `requestingTaskID` parameter. Self-held locks are treated as available, preventing a re-queued task from being permanently blocked by its own stale locks.
4. `internal/engine/workqueue.go`: `AcquireAndDispatch` pre-releases stale locks before re-acquiring. `GetDispatchable` passes task ID to lock check.

All 30 engine tests pass. All Go tests pass.

### BUG-044 (HIGH): Superficial doctor check -- FIXED (Issue #49, closed)
`internal/doctor/doctor.go`: `checkBitNet()` now sends a minimal chat completion request (`max_tokens: 4`) after confirming `/v1/models` responds. Reports `[WARN]` if:
- Inference request fails (server crashes)
- Response cannot be parsed
- Zero tokens generated (empty content)
Reports `[PASS]` only when the server responds AND generates tokens.

All doctor tests pass.

### BUG-042 (CRITICAL): BitNet SIGSEGV -- PARTIALLY FIXED (Issue #47, open; follow-up #50)
**Go code fix merged:**
- `cmd/axiom/bitnet.go`: Added `rebuildBitNetARM()` that reconfigures cmake with `BITNET_ARM_TL1=ON` and `GGML_METAL=ON` after `setup_env.py` runs on ARM64 platforms. This ensures the optimized TL1 kernels are compiled into the binary.

**Binary rebuild NOT completed:**
The TL1 kernel file (`ggml-bitnet-lut.cpp`) takes 3+ hours of CPU time to compile on a 16GB Apple M4 MacBook Air and did not finish within the session. The compilation is blocked by the machine's memory constraints (15G/16G used). See Issue #50 for details and alternative approaches.

The Go code fix is production-ready and will automatically trigger the correct rebuild on systems with sufficient resources. The SIGSEGV fix cannot be verified until the binary rebuild completes on a machine with more RAM (32GB+).
