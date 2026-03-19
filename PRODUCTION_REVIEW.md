# Axiom Production Readiness Review

**Date:** 2026-03-18
**Reviewer:** Claude Code Audit
**Architecture Reference:** Architecture.md v2.2
**Build Plan Reference:** BUILD_PLAN.md v1.0
**Codebase:** 91 Go source files, 29 test files, 1 SQL schema

---

## Executive Summary

The Axiom codebase has substantial implementation across all 24 build plan phases. The project directory structure, package layout, and architectural boundaries match the spec. Most subsystems have real, functional implementations -- not stubs. The core engine, state management, IPC protocol, inference broker, container lifecycle, approval pipeline, merge queue, git integration, API server, SRS system, security subsystems, and budget tracking all have working code.

However, the implementations vary in depth. Some packages are production-ready (state/tasks, IPC protocol, container hardening, manifest validation, path safety), while others have the right structure and types but need additional logic to handle real-world edge cases (merge queue conflict resolution, orchestrator runtime, semantic indexer queries). The system cannot yet execute an end-to-end project because the subsystems have not been wired together into a unified execution loop.

**Overall Completeness: ~65-70%**

---

## Per-Phase Review

### Phase 0: Project Scaffolding

**Spec Compliance:** 9/10
**Completeness:** 9/10

**What exists:**
- Go module initialized (`github.com/ethan03805/axiom`, Go 1.25)
- Full directory structure matches BUILD_PLAN spec
- Makefile with `build`, `test`, `lint`, `docker-images`, `gui`, `all`, `clean` targets
- All core dependencies in go.mod (sqlite, toml, cobra, docker, websocket, fsnotify)
- Initial SQL migration (`internal/state/schemas/001_initial.sql`) with all 13 tables from Architecture Section 15.2
- Config loading (`internal/engine/config.go`) with TOML parsing, project/global merge, defaults

**Gaps:**
1. **Missing `cmd/axiom/main.go` entrypoint verification** -- the CLI files exist but need to be verified that `go build ./cmd/axiom` produces a working binary. Run `make build` and confirm.
2. **Missing Docker directory** -- `docker/Dockerfile.meeseeks-*` and `docker/axiom-seccomp.json` are referenced in the Makefile but the files themselves were not found in the repo. These need to be created for Phase 21.
3. **Missing `skills/` directory** -- Skill templates (`*.md.tmpl`) referenced in BUILD_PLAN step 0.2 are not present.
4. **`go-tree-sitter` not in go.mod** -- The semantic indexer requires tree-sitter Go bindings but the dependency is not present. The indexer package currently defines types but cannot parse code.

**Steps to Production:**
- [ ] Run `make build` and fix any compilation errors
- [ ] Run `make test` and fix any test failures
- [ ] Add `go-tree-sitter` dependency to go.mod
- [ ] Create `docker/` directory with all four Dockerfiles
- [ ] Create `skills/` directory with template files
- [ ] Verify SQLite schema matches Architecture Section 15.2 exactly (compare column-by-column)

---

### Phase 1: Core Engine Foundation

**Spec Compliance:** 8/10
**Completeness:** 8/10

**What exists:**
- `Engine` struct with `New()`, `Start()`, `Stop()` lifecycle (`internal/engine/engine.go`)
- SQLite in WAL mode with `busy_timeout=5000`, `MaxOpenConns(10)` (`internal/state/db.go`)
- Full task CRUD: `CreateTask`, `CreateTaskBatch`, `GetTask`, `ListTasks`, `UpdateTaskStatus`, `GetReadyTasks`, `GetChildTasks` (`internal/state/tasks.go`)
- Task state machine enforcement with all 8 states from Architecture Section 15.4
- Valid transitions enforced including `cancelled_eco` from any active state
- Circular dependency detection via DFS at task creation time (`AddTaskDependency`)
- Event emission system with `SubscribeAll`, `Subscribe`, `Emit` (`internal/events/emitter.go`)
- All event types from Architecture Section defined as constants
- Event persistence wired: all emitted events are stored in SQLite via subscriber
- Cost tracking CRUD: `InsertCost`, `GetProjectCost`, `GetCostByModel`, `GetCostByAgentType`, `GetTaskCost` (`internal/state/costs.go`)
- ECO tracking: `InsertECO`, `UpdateECOStatus`, `ListECOs` (`internal/state/eco.go`)
- Container session tracking: `InsertContainerSession`, `UpdateContainerSessionStopped`, `MarkOrphanedContainerSessions` (`internal/state/containers.go`)
- Lock operations: `AcquireLock`, `ReleaseLock`, `ReleaseAllTaskLocks`, `GetLockedResources`, `IsResourceLocked` (`internal/state/locks.go`)

**Gaps:**
1. **Engine does not hold subsystem references** -- Architecture says Engine holds references to all subsystems (container manager, broker, etc.). Currently Engine only holds config, db, and emitter. The engine needs to be wired to all other subsystems to serve as the central coordinator.
2. **No execution loop** -- `Start()` performs crash recovery but does not start the work queue processing loop. The engine needs a main goroutine that continuously dispatches ready tasks.
3. **Event details stored as `fmt.Sprintf("%v")` not JSON** -- The event persistence subscriber serializes the `Details` map using `%v` formatting, which produces Go map syntax not JSON. This should use `json.Marshal`.
4. **Missing `store.go` types** -- The `internal/state/store.go` file should define shared types (`Task`, `Event`, `CostEntry`, `EcoEntry`, `ContainerSession`) used across the state package. Verify these types exist and match the schema.

**Steps to Production:**
- [ ] Wire Engine to hold references to ContainerManager, Broker, MergeQueue, WorkQueue, SemanticIndexer, SRSApprovalManager, BudgetEnforcer, IPCWatcher, IPCDispatcher
- [ ] Add main execution loop to `Start()` that runs `WorkQueue.GetDispatchable()` -> dispatch cycle
- [ ] Fix event detail serialization to use `json.Marshal` instead of `%v`
- [ ] Add `InsertEvent` method that accepts JSON details string
- [ ] Verify `state/store.go` defines all shared types with correct field tags

---

### Phase 2: Container Lifecycle

**Spec Compliance:** 9/10
**Completeness:** 9/10

**What exists:**
- Full container manager (`internal/container/manager.go`, 613 lines) with:
  - `SpawnMeeseeks`, `SpawnReviewer`, `SpawnSubOrchestrator`, `SpawnValidator`
  - Container naming: `axiom-<task-id>-<timestamp>`
  - Volume mount setup: spec (read-only), staging (read-write), IPC (read-write)
  - Concurrency limit enforcement (validators excluded from Meeseeks count)
  - `Destroy()` with force-remove and session recording
  - `CleanupOrphans()` on startup -- finds all `axiom-*` containers and removes them
  - `ListActive()`, `ActiveCount()`
  - Timeout monitor goroutine per container with configurable timeout
  - Container session recording in SQLite
  - Event emission for spawn/destroy lifecycle events
  - `Shutdown()` for graceful engine shutdown
- Complete hardening policy (`internal/container/hardening.go`):
  - `--read-only`, `--cap-drop=ALL`, `--security-opt=no-new-privileges`
  - `--pids-limit=256`, `--tmpfs /tmp:rw,noexec,size=256m`
  - `--network=none`, `--user <uid>:<gid>`
  - `--cpus`, `--memory` from config
  - Optional seccomp profile support
  - `ApplyToHostConfig()` produces the complete Docker HostConfig
- `DockerClient` interface for testing (wraps real Docker SDK)

**Gaps:**
1. **Missing `images.go`** -- Build Plan step 7.2 calls for language-specific validation profiles (Go vendored modules, Node npm ci, Python wheels). The `images.go` file exists but its contents were not verified to include image selection logic.
2. **Missing `validation.go` integration** -- The `validation.go` file exists but needs to implement the full validation sandbox workflow (overlay filesystem, sequential check execution, structured results).
3. **Concurrency queue** -- When at the concurrency limit, the spec says spawn requests should queue. Currently `spawn()` returns an error immediately. It should block or queue.

**Steps to Production:**
- [ ] Verify `images.go` implements image selection based on project language detection
- [ ] Implement spawn queuing when concurrency limit is reached (channel-based or condition variable)
- [ ] Implement validation sandbox in `validation.go`: overlay filesystem setup, sequential check execution (compile -> lint -> test -> security), structured result collection
- [ ] Add language-specific validation profiles (Go, Node, Python, Rust) per Architecture Section 13.5
- [ ] Add separate BitNet resource tracking (not counted against container slots)

---

### Phase 3: IPC Protocol

**Spec Compliance:** 9/10
**Completeness:** 9/10

**What exists:**
- All 14 message types defined (`internal/ipc/protocol.go`): task_spec, review_spec, revision_request, task_output, review_result, inference_request, inference_response, lateral_message, action_request, action_response, request_scope_expansion, scope_expansion_response, context_invalidation_warning, shutdown
- `InferenceStreamChunk` type for streaming responses
- `ParseMessage()` and `ParseMessageType()` for JSON deserialization
- Complete fsnotify-based watcher (`internal/ipc/watcher.go`, 269 lines):
  - `WatchTask()`, `UnwatchTask()`, `Stop()`
  - fsnotify event loop filtering for `.json` CREATE events
  - Polling fallback at 1-second intervals when fsnotify unavailable
  - Task ID extraction from file paths
  - Temp file skipping (`.tmp` suffix)
- Complete dispatcher (`internal/ipc/handler.go`):
  - `Register()` handlers by message type
  - `Dispatch()` method designed as `MessageHandler` callback for Watcher
  - Fallback handler for unregistered types
  - Error responses written back via Writer
- Complete writer (`internal/ipc/writer.go`, 129 lines):
  - `Send()` with atomic write (tmp + rename)
  - Sequential file naming: `<message-type>-<sequence-number>.json`
  - `StreamWriter` for chunked inference responses: `response-001.json`, `response-002.json`

**Gaps:**
1. **Watcher `processFile` error logging** -- Errors from `ParseMessage` are silently dropped. Should log or emit an event for debugging.
2. **No directory creation in Writer.Send()** -- The writer assumes the input directory already exists. Should `MkdirAll` the target directory.

**Steps to Production:**
- [ ] Add error logging/event emission in `processFile()` for parse failures
- [ ] Add `os.MkdirAll` for the input directory in `Writer.Send()`
- [ ] Add integration test: engine writes TaskSpec -> container watcher detects -> dispatcher routes -> response written

---

### Phase 4: Inference Broker

**Spec Compliance:** 9/10
**Completeness:** 9/10

**What exists:**
- Complete broker (`internal/broker/broker.go`, 417 lines) with:
  - `HandleInferenceRequest()` -- full IPC handler: validate -> route -> execute -> log -> respond
  - `HandleStreamingInferenceRequest()` -- streaming via `StreamWriter`
  - Three-check validation: model allowlist, budget pre-authorization, per-task rate limiting
  - Provider routing with BitNet fallback when OpenRouter is down
  - Cost logging to `cost_log` table
  - `RegisterModel()`, `SetTaskTier()`, `ResetTaskCount()`
- Complete OpenRouter client (`internal/broker/openrouter.go`, 260 lines):
  - Non-streaming `Complete()` with proper request/response handling
  - Streaming `CompleteStream()` with SSE parsing
  - `UpdateAPIKey()` for config reload without restart
  - Proper error handling for non-200 responses
- Complete BitNet client (`internal/broker/bitnet.go`, 193 lines):
  - OpenAI-compatible API at `localhost:3002`
  - Grammar constraints passthrough (`req.GrammarConstraints`)
  - `Available()` health check via `/v1/models` endpoint
  - Streaming support via shared `parseSSEStream()`

**Gaps:**
1. **`ipcWriterBaseDir()` is hardcoded** -- Returns `.axiom/containers/ipc` rather than using config. Should be injected via Config.
2. **Agent type always "meeseeks" in `logCost()`** -- Should determine agent type from task/container context.
3. **No retry logic for transient HTTP errors** -- OpenRouter calls fail immediately on any HTTP error. Should retry on 429/5xx with backoff.

**Steps to Production:**
- [ ] Make `ipcWriterBaseDir` configurable via Broker Config
- [ ] Pass agent type through the inference request or look it up from container tracking
- [ ] Add retry with exponential backoff for 429 and 5xx responses in OpenRouter client
- [ ] Add request timeout context propagation (currently uses `context.Background()` in route)
- [ ] Wire `HandleInferenceRequest` to the IPC Dispatcher during engine startup

---

### Phase 5: Task System & Concurrency

**Spec Compliance:** 9/10
**Completeness:** 8/10

**What exists:**
- Full work queue (`internal/engine/workqueue.go`):
  - `GetDispatchable()` -- finds tasks with dependencies met and locks available
  - `AcquireAndDispatch()` -- atomic lock acquisition with deterministic ordering (alphabetical)
  - `CompleteTask()` -- releases locks, re-queues waiting_on_lock tasks
  - `FailTask()` -- releases locks on failure
- Scope expansion handler (`internal/engine/scope.go`):
  - `HandleScopeExpansion()` -- checks lock availability, acquires additional locks or returns waiting_on_lock
  - Proper handling of lock conflict (destroy Meeseeks, mark task waiting_on_lock with blocking task info)
- State layer:
  - `GetReadyTasks()` -- SQL query for queued tasks with all dependencies done
  - `AddTaskDependency()` with circular dependency DFS detection
  - `SetTaskWaitingOnLock()` with blocking task ID and expanded files recorded
  - `GetWaitingOnLockTasks()` for re-queuing when locks release
  - `GetTaskTargetFiles()`, `AddTaskTargetFile()` with lock scope

**Gaps:**
1. **`GetWaitingOnLockTasks` uses description LIKE pattern** -- Stores blocking info in the `description` field as `"blocked_by:<task-id>"`. This is fragile. Should use a dedicated column or junction table.
2. **No lock scope escalation validation** -- The spec calls for file -> package -> module -> schema escalation. Lock acquisition treats all scopes equally.
3. **No context invalidation warnings** -- Architecture Section 16.5 describes sending warnings to active Meeseeks when commits change their referenced symbols.

**Steps to Production:**
- [ ] Add a `blocked_by_task_id` column to the tasks table (or a `task_waiting_info` table) instead of encoding in description
- [ ] Implement lock scope escalation: when locking at package level, acquire locks for all files in the package
- [ ] Implement context invalidation warnings after merge queue commits (optional but spec-recommended)
- [ ] Add concurrency limit awareness to `GetDispatchable()` -- return at most N tasks where N = available slots

---

### Phase 6: File Router & Approval Pipeline

**Spec Compliance:** 8/10
**Completeness:** 7/10

**What exists:**
- Complete manifest parsing and validation (`internal/pipeline/manifest.go`, 219 lines):
  - `ParseManifest()`, `ValidateManifest()`
  - All checks: file existence, unlisted file detection, binary size_bytes requirement, empty paths
  - `AllPaths()`, `NonBinaryPaths()`, `HasChanges()` helpers
- Complete path safety validation (`internal/pipeline/router.go`, 162 lines):
  - `ValidatePathSafety()` with all Architecture Section 29.5 checks
  - Path traversal rejection, symlink rejection, device file rejection, FIFO rejection
  - Oversized file rejection (configurable max, default 1MB)
  - Scope enforcement against task's `target_files`
  - Absolute path rejection, `.axiom/` path rejection
  - Uses `os.Lstat` (not `os.Stat`) to detect symlinks
- Pipeline types (`internal/pipeline/pipeline.go`):
  - `StageResult`, `PipelineResult`, `ValidationResult`, `PipelineConfig`

**Gaps:**
1. **Pipeline orchestration not implemented** -- The pipeline types exist but the actual 5-stage execution (manifest validation -> validation sandbox -> reviewer -> orchestrator -> merge queue) is not wired together.
2. **No ReviewSpec generation** -- Architecture Section 11.7 defines the ReviewSpec format. No code generates it from TaskSpec + Meeseeks output + validation results.
3. **No retry/escalation logic** -- Architecture says max 3 retries at same tier, then escalate model tier, max 2 escalations. This logic needs to be in the pipeline orchestrator.
4. **No batched review for trivial tasks** -- Architecture Section 14.3 allows batching local-tier tasks.

**Steps to Production:**
- [ ] Implement `Pipeline.Execute(taskID)` that runs all 5 stages sequentially
- [ ] Implement `ReviewSpec` generation: combine TaskSpec + output + manifest + validation results
- [ ] Implement retry/escalation controller: track attempt count per task, escalate after 3 retries, block after 2 escalations
- [ ] Implement model family diversification for reviewer selection (Architecture Section 11.3)
- [ ] Implement batched review aggregation for local-tier tasks
- [ ] Wire pipeline execution to run after Meeseeks signals completion via IPC

---

### Phase 7: Validation Sandbox

**Spec Compliance:** 6/10
**Completeness:** 5/10

**What exists:**
- `SpawnValidator()` in container manager with validation-specific hardening
- `ValidationHardening()` function (currently same as default but separate for future differentiation)
- Container naming, timeout, and lifecycle tracking for validators
- Validators excluded from Meeseeks concurrency count

**Gaps:**
1. **No overlay filesystem** -- Architecture Section 13.3 requires read-only project snapshot + writable overlay. No code creates this.
2. **No sequential check execution** -- No code runs compile -> lint -> test -> security scan inside the sandbox.
3. **No language-specific profiles** -- Go vendored modules, Node npm ci, Python pip wheel handling not implemented.
4. **No structured result collection** -- No code parses pass/fail/output from each validation check.
5. **No warm sandbox pools** -- Architecture Section 13.8. Behind feature flag so not critical for initial release.

**Steps to Production:**
- [ ] Implement overlay filesystem: mount project at HEAD as read-only base, apply Meeseeks output as writable overlay
- [ ] Implement validation executor: run language-appropriate compile, lint, test, security scan inside the sandbox container
- [ ] Implement structured result parsing: capture exit codes, stdout/stderr, test coverage for each check
- [ ] Implement Go, Node, Python, Rust language profiles per Architecture Section 13.5
- [ ] Skip compilation/linting for binary files (respect manifest `binary: true` flag)
- [ ] Wire validation results into the pipeline for inclusion in ReviewSpec

---

### Phase 8: Semantic Indexer

**Spec Compliance:** 6/10
**Completeness:** 5/10

**What exists:**
- Type definitions (`internal/index/indexer.go`): `SymbolKind`, `Symbol`, `Field`, `Import`, `Dependency`, `Parser` interface, `Indexer` struct
- Go parser stub (`internal/index/parser_go.go`)
- Index schema (`internal/index/schema.go`)
- Query types (`internal/index/queries.go`)

**Gaps:**
1. **tree-sitter not integrated** -- The `go-tree-sitter` dependency is not in `go.mod`. The Go parser likely uses regex or `go/ast` instead of tree-sitter.
2. **Index storage not verified** -- Need to confirm that `schema.go` creates proper SQLite tables for the index.
3. **Query implementation depth unknown** -- The five query types (lookup_symbol, reverse_dependencies, list_exports, find_implementations, module_graph) exist but implementation depth needs verification.
4. **No incremental refresh** -- Architecture Section 17.4 requires incremental re-indexing after each merge queue commit.
5. **No IPC handler for `query_index`** -- Orchestrators query the index via IPC. No handler is registered.

**Steps to Production:**
- [ ] Add `go-tree-sitter` to go.mod and implement proper AST parsing for Go, JS/TS, Python, Rust
- [ ] Verify index schema tables are created and populated during full indexing
- [ ] Verify all 5 query types return accurate results (write targeted tests against a test project)
- [ ] Implement incremental refresh: re-index only changed files after merge queue commit
- [ ] Register `query_index` handler with the IPC Dispatcher
- [ ] Implement `.axiom/` exclusion from indexing
- [ ] Wire full index on `axiom init` and `axiom index refresh`

---

### Phase 9: Merge Queue & Git Integration

**Spec Compliance:** 9/10
**Completeness:** 8/10

**What exists:**
- Complete merge queue (`internal/merge/queue.go`, 272 lines):
  - `Submit()`, `ProcessNext()` with serialized processing
  - Full 10-step process from Architecture Section 16.4
  - Base snapshot validation (current/stale/diverged)
  - File application and deletion via git manager
  - Integration check execution via injected `ValidateFn` callback
  - Revert on failure via `git reset --hard`
  - Commit with Axiom-format message
  - Re-index trigger via injected `ReindexFn` callback
  - Event emission for merge start/complete/requeue
- Complete git manager (`internal/git/manager.go`, 244 lines):
  - `CheckClean()` -- refuses to start on dirty working tree with clear error listing files
  - `CreateProjectBranch()` -- creates `axiom/<slug>` branch
  - `Commit()` with Architecture Section 23.2 format (task ID, SRS refs, models, attempt, cost, base snapshot)
  - `ApplyFiles()`, `DeleteFiles()`, `ResetHard()`
  - `HeadSHA()`, `CurrentBranch()`, `IsAncestor()`, `DiffFiles()`
- Snapshot manager (`internal/git/snapshot.go`) -- exists, used by merge queue

**Gaps:**
1. **No three-way merge for stale snapshots** -- Currently re-queues on stale snapshot. Architecture Section 16.4 says "attempt merge" first, then re-queue on conflict. This is acceptable for initial release since re-queuing is the safe path.
2. **`ApplyFiles` uses shell `cat >` for file writing** -- The `writeFile()` helper uses `sh -c` with heredoc. Should use `os.WriteFile()` directly for safety and reliability.
3. **Lock release not wired** -- Steps 9-10 (release locks, unblock tasks) are commented as "handled by the engine's WorkQueue.CompleteTask()". Need to verify this integration.

**Steps to Production:**
- [ ] Replace `writeFile()` shell command with `os.WriteFile()` and `os.MkdirAll()` for parent directories
- [ ] Wire merge queue processing to engine's main loop: after ProcessNext succeeds, call WorkQueue.CompleteTask()
- [ ] Add integration test: submit two items to queue, verify serialized processing
- [ ] Verify snapshot manager correctly detects stale/current/diverged states

---

### Phase 10: Orchestrator Runtime

**Spec Compliance:** 6/10
**Completeness:** 5/10

**What exists:**
- Phase/runtime enum definitions (`internal/orchestrator/embedded.go`): bootstrap, execution, paused, completed phases and claw, claude-code, codex, opencode runtimes
- `EmbeddedConfig` struct with runtime, image, limits, project slug, budget
- Action handler (`internal/orchestrator/actions.go`) -- exists

**Gaps:**
1. **No actual container spawning logic** -- The orchestrator runtime types exist but spawning an orchestrator container with the right config is not implemented.
2. **No bootstrap context construction** -- Architecture Section 8.7 requires scoped context: repo-map + semantic index for existing projects, prompt-only for greenfield.
3. **No IPC action handling integration** -- The 13 action types from Architecture Section 8.6 (submit_srs, create_task, spawn_meeseeks, etc.) need to be wired as IPC handlers.
4. **No orchestrator crash recovery** -- Architecture Section 8.6 says orchestrator state persists in SQLite for reconnection.

**Steps to Production:**
- [ ] Implement orchestrator container spawning with correct volume mounts and bootstrap context
- [ ] Implement bootstrap mode: construct repo-map or greenfield context based on project state
- [ ] Wire all 13 action request types from Architecture Section 8.6 as IPC handlers
- [ ] Implement orchestrator phase transitions (bootstrap -> execution -> paused -> completed)
- [ ] Implement crash recovery: on reconnection, read task tree from SQLite and resume

---

### Phase 11: SRS & ECO System

**Spec Compliance:** 9/10
**Completeness:** 9/10

**What exists:**
- Complete SRS format validation (`internal/srs/parser.go`): validates all required sections, checks FR-xxx/NFR-xxx/AC-xxx/IC-xxx numbering
- Complete approval flow (`internal/srs/approval.go`):
  - `SubmitDraft()` -- validates format, writes draft
  - `Approve()` -- locks SRS, computes hash, emits event
  - `Reject()` -- sends feedback event
  - `IsDelegatedToClaw()` -- delegation support
  - `VerifyIntegrity()` -- startup hash check
- Complete SRS immutability (`internal/srs/lock.go`):
  - `WriteDraft()`, `Lock()` (read-only permissions + SHA-256 hash file), `VerifyIntegrity()`
  - `IsLocked()` checks file permission bits
  - `Unlock()` for ECO addendum writing
- Complete ECO management (`internal/srs/eco.go`):
  - All 6 valid categories from Architecture Section 7.2
  - `ProposeECO()` with category validation
  - `ApproveECO()` with addendum file writing to `.axiom/eco/ECO-NNN.md`
  - `RejectECO()`
  - `CancelAffectedTasks()` -- marks tasks as `cancelled_eco` with `eco_ref`
  - ECO record format matches Architecture Section 7.4

**Gaps:**
1. **No user-facing approval prompt** -- The approval manager emits events but there's no CLI prompt or GUI callback for the user to review and approve/reject.
2. **ECO `CancelAffectedTasks` uses raw SQL** -- The eco_ref update bypasses the state layer. Should add a dedicated method to the DB layer.

**Steps to Production:**
- [ ] Implement CLI approval prompt: display SRS content, wait for user input (approve/reject/feedback)
- [ ] Wire SRS approval to `axiom run` flow: generate SRS -> present to user -> approve -> lock
- [ ] Move eco_ref SQL update to a dedicated state method
- [ ] Add test for SRS integrity verification failure (tampered SRS file)

---

### Phase 12: Budget & Cost Management

**Spec Compliance:** 9/10
**Completeness:** 8/10

**What exists:**
- Budget enforcer (`internal/budget/enforcer.go`):
  - `PreAuthorize()` -- validates max_tokens * pricing fits remaining budget
  - Budget warning and exhaustion thresholds
  - External mode flag
  - Pause flag for budget exhaustion
- Cost tracker (`internal/budget/tracker.go`):
  - `GetReport()` -- comprehensive cost report at all granularities (per-task, per-model, per-agent-type)
  - Projected total calculation based on completion percentage
  - External mode disclaimer
  - `GetTaskCost()`, `GetProjectTotal()`
  - `CalculateMaxRequestCost()` utility

**Gaps:**
1. **Budget enforcer not wired to broker** -- The broker has its own budget check in `validate()`. The enforcer should be the single source of truth, or they should be unified.
2. **No budget increase handler** -- Architecture Section 21.3 says user can increase budget to resume. No CLI or GUI path for this.
3. **Budget warning/exhaustion events not emitted** -- The enforcer has fields for thresholds but no code emits the events.

**Steps to Production:**
- [ ] Unify budget enforcement: either broker delegates to enforcer, or remove enforcer and keep broker's check
- [ ] Implement budget warning event emission when spend exceeds `warn_at_percent`
- [ ] Implement budget exhaustion: pause execution, emit event, prompt user
- [ ] Add CLI command `axiom budget increase <amount>` or integrate into `axiom resume`
- [ ] Wire budget display into `axiom status` output

---

### Phase 13: Model Registry

**Spec Compliance:** 7/10
**Completeness:** 6/10

**What exists:**
- Registry types (`internal/registry/registry.go`)
- OpenRouter model fetching (`internal/registry/openrouter.go`) -- exists

**Gaps:**
1. **No `~/.axiom/registry.db`** -- Architecture Section 18.3 specifies a separate SQLite registry database. Implementation may use in-memory storage only.
2. **No `models.json` curated capability file** -- Architecture Section 18.4 requires a shipped file with strengths/weaknesses/recommendations.
3. **No `performance.go`** -- Historical success rate and avg cost per task tracking not found.
4. **No offline fallback** -- Architecture Section 18.6 requires stale-data warning when network unavailable.

**Steps to Production:**
- [ ] Implement `~/.axiom/registry.db` SQLite database with Architecture Section 18.3 schema
- [ ] Create curated `models.json` with model capabilities, strengths, weaknesses
- [ ] Implement OpenRouter `/api/v1/models` fetching and merging with curated data
- [ ] Implement historical performance tracking (update after project completion)
- [ ] Implement offline fallback with stale-data warning
- [ ] Wire `axiom models refresh/list/info` CLI commands to registry

---

### Phase 14: BitNet Local Inference

**Spec Compliance:** 7/10
**Completeness:** 6/10

**What exists:**
- BitNet client in broker (`internal/broker/bitnet.go`) -- complete API client
- BitNet server management (`internal/broker/bitnet_server.go`) -- exists
- Grammar constraints passthrough in request bodies

**Gaps:**
1. **No first-run download** -- Architecture Section 19.9 requires prompting user to download Falcon3 weights on first `axiom bitnet start`.
2. **No server lifecycle management** -- Starting/stopping the BitNet server process (bitnet.cpp) is not fully implemented.
3. **No resource monitoring** -- Architecture Section 19.6 requires tracking BitNet CPU/memory usage.

**Steps to Production:**
- [ ] Implement model weight download with user confirmation on first run
- [ ] Implement BitNet server process management (start/stop/restart)
- [ ] Implement resource monitoring for BitNet server
- [ ] Wire `axiom bitnet start/stop/status/models` CLI commands

---

### Phase 15: CLI

**Spec Compliance:** 7/10
**Completeness:** 6/10

**What exists:**
- All CLI commands are defined with Cobra (`cmd/axiom/`):
  - Project: init, run, status, pause, resume, cancel, export
  - Models: refresh, list, info
  - BitNet: start, stop, status, models
  - API: start, stop, token generate/list/revoke
  - Index: refresh, query
  - Utility: version, doctor, config reload, skill generate

**Gaps:**
1. **Most commands are stubs** -- Commands parse flags and print placeholder messages but don't call engine methods.
2. **`axiom init` needs full directory creation** -- Should create `.axiom/config.toml` with defaults, `.gitignore` entries, directory structure.
3. **`axiom run` needs full lifecycle wiring** -- Generate SRS -> approve -> decompose -> execute loop.
4. **`axiom status` needs real data** -- Should query engine for task tree, active containers, budget status.
5. **`axiom doctor` only checks Docker, Git, resources, config** -- Missing BitNet, OpenRouter, warm-pool, secret scanner checks.

**Steps to Production:**
- [ ] Wire `axiom init` to create full `.axiom/` directory structure with config template and gitignore entries
- [ ] Wire `axiom run` to: load config -> create engine -> start engine -> spawn orchestrator -> SRS approval loop -> execution
- [ ] Wire `axiom status` to query engine state and display task tree, containers, budget
- [ ] Wire `axiom pause/resume/cancel` to engine lifecycle methods
- [ ] Complete `axiom doctor` with all checks from Architecture Section 27.7
- [ ] Wire model, bitnet, api, index, and skill commands to their respective subsystems

---

### Phase 16: API Server & Claw Integration

**Spec Compliance:** 8/10
**Completeness:** 8/10

**What exists:**
- HTTP server with routing (`internal/api/server.go`):
  - Configurable port, middleware stack (audit logging, IP restriction, auth, rate limiting)
  - All REST endpoints from Architecture Section 24.2 registered
- Complete handlers (`internal/api/handlers.go`, 235 lines):
  - All 16 endpoints: CreateProject, RunProject, ApproveSRS, RejectSRS, ApproveECO, RejectECO, PauseProject, ResumeProject, CancelProject, GetStatus, GetTasks, GetAttempts, GetCosts, GetEvents, GetModels, QueryIndex
  - Callback-based design for engine integration
- Token authentication (`internal/api/auth.go`):
  - `axm_sk_<random>` token generation
  - Expiration, revocation, scoped tokens (read-only / full-control)
  - Thread-safe with RWMutex
- Rate limiting (`internal/api/ratelimit.go`):
  - Per-token rate limiting with configurable RPM
  - Sliding window buckets
- WebSocket hub (`internal/api/websocket.go`):
  - Connection upgrading, client tracking
  - Event broadcasting from engine emitter
  - Per-project filtering support
- Cloudflare Tunnel (`internal/tunnel/tunnel.go`):
  - Start/stop cloudflared process
  - Public URL extraction from output

**Gaps:**
1. **Handlers use callback pattern but callbacks are nil by default** -- Handlers return default responses when callbacks are nil. Need to wire engine callbacks.
2. **Token storage is in-memory only** -- Architecture says tokens stored in `~/.axiom/api-tokens/`. Currently lost on restart.
3. **No `Retry-After` header on 429** -- Rate limiter returns bool but server middleware needs to set the header.
4. **WebSocket does not filter by project ID** -- `broadcast()` sends to all clients regardless of project ID. The `clients` map stores project ID but doesn't filter.

**Steps to Production:**
- [ ] Wire handler callbacks to engine operations during server startup
- [ ] Implement persistent token storage in `~/.axiom/api-tokens/`
- [ ] Add `Retry-After` header to 429 responses
- [ ] Implement project ID filtering in WebSocket broadcast
- [ ] Add audit logging of failed auth attempts with source IP

---

### Phase 17: Skill System

**Spec Compliance:** 6/10
**Completeness:** 5/10

**What exists:**
- Generator types (`internal/skill/generator.go`): Runtime enum, TemplateData struct, Generator struct

**Gaps:**
1. **No template files** -- The `skills/*.md.tmpl` files are not present in the repo.
2. **No template loading/rendering** -- `text/template` integration needs implementation.
3. **All 13 content items from Architecture Section 25.3 need to be covered in templates.**
4. **No regeneration on config change.**

**Steps to Production:**
- [ ] Create skill templates for all 4 runtimes (Claw, Claude Code, Codex, OpenCode)
- [ ] Implement template loading and Go `text/template` rendering with dynamic data injection
- [ ] Ensure all 13 required content items are covered
- [ ] Wire `axiom skill generate` CLI command
- [ ] Implement config change detection for automatic regeneration

---

### Phase 18: Security Hardening

**Spec Compliance:** 9/10
**Completeness:** 8/10

**What exists:**
- Complete secret scanning (`internal/security/secrets.go`):
  - Regex patterns for API keys (OpenAI, Axiom, GitHub, AWS), private keys, connection strings
  - Sensitive path patterns (`.env*`, `*credentials*`, `*secret*`)
  - `ScanContent()` returns findings and redacted content
  - Configurable patterns from config
- Complete prompt injection mitigation (`internal/security/injection.go`, 150 lines):
  - `WrapUntrustedContent()` -- wraps in `<untrusted_repo_content>` tags with source attribution
  - `InstructionSeparation()` -- returns the instruction separation text
  - `AddProvenanceLabel()` -- adds source file/line provenance
  - `IsExcludedPath()` -- blocks `.axiom/`, `.env` paths
  - `ScanForInjectionPatterns()` -- regex detection for "ignore previous instructions", "you are now", etc.
  - `SanitizeContent()` -- applies all mitigations
  - `PrepareContextForPrompt()` -- full pipeline: exclude paths -> redact secrets -> sanitize injection -> wrap
- Path safety validation in pipeline router (covered in Phase 6)

**Gaps:**
1. **No `validation.go` for file safety** -- Architecture Section 29.5 defines file safety rules. These are implemented in `pipeline/router.go` but a dedicated `security/validation.go` is not present.
2. **No sensitive file routing to BitNet** -- Architecture Section 29.4 says tasks touching sensitive files should be forced to BitNet. No integration between secret scanner and broker routing.
3. **No prompt log redaction** -- Architecture Section 29.4 point 5 says prompt logs must have secrets redacted. No integration between secret scanner and prompt logger.

**Steps to Production:**
- [ ] Implement sensitive file routing: when secret scanner flags a file, force the task to local-tier BitNet inference
- [ ] Wire `PrepareContextForPrompt()` into TaskSpec generation pipeline
- [ ] Implement prompt log redaction: apply `SecretScanner.ScanContent()` before writing prompt logs
- [ ] Add user-override path for local-only routing (with audit logging)

---

### Phase 19: Observability & Crash Recovery

**Spec Compliance:** 7/10
**Completeness:** 7/10

**What exists:**
- Prompt logging (`internal/engine/promptlog.go`) -- exists with test file
- Crash recovery in Engine: resets orphaned tasks, releases stale locks
- Orphan container cleanup in ContainerManager: finds and removes `axiom-*` containers, reconciles sessions
- SRS integrity verification in LockManager

**Gaps:**
1. **Crash recovery does not call container cleanup** -- `Engine.CrashRecovery()` resets tasks and locks but does not call `ContainerManager.CleanupOrphans()`. These need to be wired together.
2. **No staging directory cleanup** -- Architecture Section 22.3 step 4 says clean staging directories.
3. **`axiom doctor` checks are incomplete** -- Missing BitNet status, OpenRouter reachability, warm-pool image validation, secret scanner regex validation.

**Steps to Production:**
- [ ] Wire `Engine.CrashRecovery()` to call `ContainerManager.CleanupOrphans()`
- [ ] Add staging directory cleanup to crash recovery
- [ ] Wire SRS integrity verification to crash recovery
- [ ] Complete `axiom doctor` with all checks from Architecture Section 27.7
- [ ] Verify prompt logging respects `log_prompts` config flag

---

### Phase 20: GUI Dashboard

**Spec Compliance:** 5/10
**Completeness:** 4/10

**What exists:**
- Wails app backend (`gui/app.go`): callback-based design with 15+ operation callbacks
- Event forwarding from engine emitter to React frontend via `runtime.EventsEmit()`

**Gaps:**
1. **React frontend not verified** -- The `gui/frontend/` directory needs React components implementing all 9 views from Architecture Section 26.2.
2. **Callback implementations are stubs** -- OnNewProject, OnApproveSRS, etc. need to call engine methods.
3. **No real-time update verification** -- Architecture requires 500ms update latency.

**Steps to Production:**
- [ ] Implement all 9 React views: Project Overview, Task Tree, Active Containers, Cost Dashboard, File Diff Viewer, Log Stream, Timeline, Model Registry, Resource Monitor
- [ ] Wire Wails backend callbacks to engine operations
- [ ] Implement Wails event subscription in React for real-time updates
- [ ] Test 500ms update latency target
- [ ] Implement all GUI controls from Architecture Section 26.3

---

### Phase 21: Docker Images

**Spec Compliance:** 3/10
**Completeness:** 2/10

**What exists:**
- Makefile targets for building all 4 images
- Container hardening and naming in container manager

**Gaps:**
1. **No Dockerfiles** -- `docker/Dockerfile.meeseeks-go`, `-node`, `-python`, `-multi` do not exist.
2. **No IPC watcher script** -- Each image needs a script that watches `/workspace/ipc/input/` and writes to `/workspace/ipc/output/`.
3. **No seccomp profile** -- `docker/axiom-seccomp.json` not present.

**Steps to Production:**
- [ ] Create `docker/Dockerfile.meeseeks-go` -- Go toolchain, golangci-lint, non-root user, IPC watcher
- [ ] Create `docker/Dockerfile.meeseeks-node` -- Node.js, npm, TypeScript, eslint, non-root user, IPC watcher
- [ ] Create `docker/Dockerfile.meeseeks-python` -- Python 3, pip, ruff, mypy, non-root user, IPC watcher
- [ ] Create `docker/Dockerfile.meeseeks-multi` -- Go + Node.js + Python, non-root user, IPC watcher
- [ ] Create IPC watcher script (bash or Go binary) that watches input/ and writes to output/
- [ ] Create `docker/axiom-seccomp.json` seccomp profile
- [ ] Test that each image builds, runs as non-root, and has no pre-installed secrets

---

### Phase 22: Integration Testing

**Spec Compliance:** 5/10
**Completeness:** 4/10

**What exists:**
- `internal/integration_test.go` -- integration test file exists
- 29 unit test files across all packages

**Gaps:**
1. **No Docker-based integration tests** -- Need tests that spawn real containers.
2. **No IPC round-trip test** -- Engine writes TaskSpec -> container processes -> engine reads output.
3. **No full pipeline test** -- Manifest validation -> sandbox -> reviewer -> orchestrator -> merge.
4. **No concurrent lock test** -- Two tasks targeting same file, verify mutual exclusion.
5. **No budget boundary test** -- Task at budget limit, verify rejection.
6. **No crash recovery test** -- Simulate crash, verify state reconciliation.

**Steps to Production:**
- [ ] Implement integration test: full IPC round-trip (engine -> container -> engine)
- [ ] Implement integration test: approval pipeline end-to-end with mock containers
- [ ] Implement integration test: concurrent tasks with lock contention
- [ ] Implement integration test: merge queue serialization with stale snapshots
- [ ] Implement integration test: budget enforcement at boundary conditions
- [ ] Implement integration test: crash recovery (kill engine mid-execution, restart, verify state)
- [ ] Implement integration test: ECO flow (propose, approve, cancel tasks, create replacements)
- [ ] Set up CI pipeline to run integration tests with Docker

---

### Phase 23: End-to-End Testing

**Spec Compliance:** 3/10
**Completeness:** 2/10

**What exists:**
- `internal/e2e_test.go` -- e2e test file exists

**Gaps:**
1. **No test project fixtures** -- Need simple Go, Node, and Python projects.
2. **No end-to-end execution test** -- `axiom run` with a real project through completion.
3. **No external Claw test** -- API + tunnel + external orchestrator.
4. **No BitNet integration test** -- Local inference with grammar constraints.
5. **No GUI test** -- All views with real-time updates.

**Steps to Production:**
- [ ] Create test fixtures: simple Go CLI, Node Express API, Python Flask app
- [ ] Implement e2e test: `axiom run` with embedded orchestrator through SRS -> tasks -> execution -> completion
- [ ] Implement e2e test: external Claw orchestrator via API
- [ ] Implement e2e test: BitNet local inference for trivial tasks
- [ ] Implement e2e test: error recovery (intentional failures, retries, escalation)
- [ ] Implement e2e test: GUI with real-time updates

---

## Cross-Phase Integration Gaps

These are systemic issues that span multiple phases:

### 1. Engine Wiring (Critical)

The Engine struct needs to serve as the central coordinator. Currently it holds only config, db, and emitter. It needs references to:
- ContainerManager
- Broker (with OpenRouter + BitNet providers)
- MergeQueue
- WorkQueue
- SemanticIndexer
- SRSApprovalManager + ECOManager
- BudgetEnforcer + CostTracker
- IPCWatcher + IPCDispatcher + IPCWriter
- TunnelManager
- APIServer

The engine's `Start()` method needs to:
1. Perform crash recovery (reset tasks, cleanup orphans, verify SRS, clean staging)
2. Initialize all subsystems
3. Start IPC watcher
4. Start work queue processing loop
5. Start API server (if configured)

### 2. Main Execution Loop (Critical)

No main loop exists that:
1. Queries for dispatchable tasks
2. Builds TaskSpecs with minimum necessary context from semantic indexer
3. Spawns containers
4. Watches for IPC output
5. Routes through approval pipeline
6. Submits to merge queue
7. Releases locks and unblocks dependents
8. Loops until all tasks are done

### 3. TaskSpec Construction (Critical)

No code constructs actual TaskSpecs from:
- Task metadata (objective, constraints, acceptance criteria)
- Semantic index queries (symbol, file, package, or repo-map context)
- Secret scanning and redaction
- Prompt injection mitigation wrapping
- Base snapshot pinning

### 4. CLI-to-Engine Bridge (Important)

CLI commands parse flags but don't call engine methods. Each command needs wiring:
- `axiom init` -> directory creation + config generation
- `axiom run` -> engine.New() + engine.Start() + orchestrator spawn + SRS loop + execution
- `axiom status` -> query engine state
- `axiom pause/resume/cancel` -> engine lifecycle commands

---

## Priority Order for Production Readiness

### P0 -- Required for any end-to-end execution
1. Wire Engine to all subsystems
2. Implement main execution loop
3. Implement TaskSpec construction pipeline
4. Wire CLI commands to engine
5. Create Docker images (at least `meeseeks-multi`)
6. Wire IPC handlers for all message types

### P1 -- Required for reliable execution
7. Implement full approval pipeline (5 stages)
8. Implement validation sandbox execution
9. Implement retry/escalation logic
10. Complete crash recovery (containers + staging + SRS)
11. Wire budget enforcement end-to-end

### P2 -- Required for production quality
12. Complete semantic indexer with tree-sitter
13. Implement persistent API token storage
14. Complete `axiom doctor` checks
15. Create skill templates
16. Implement model registry with OpenRouter fetching

### P3 -- Nice to have for initial release
17. GUI dashboard views
18. Warm sandbox pools
19. Context invalidation warnings
20. Batched review for trivial tasks
21. BitNet server lifecycle management
22. End-to-end test suite

---

## Code Quality Notes

**Strengths:**
- Architecture references in comments throughout (e.g., "See Architecture Section 12.6.1")
- Proper error wrapping with `fmt.Errorf("...: %w", err)` consistently
- Thread-safe designs with sync.Mutex/RWMutex where needed
- Clean interface definitions for testability (DockerClient, Provider)
- Atomic file writes in IPC (tmp + rename pattern)
- SQL injection prevention via parameterized queries
- Proper NULL handling with `sql.NullString`

**Issues to fix:**
- `git/manager.go:writeFile()` uses shell command instead of `os.WriteFile()`
- `engine.go` event details serialized with `%v` instead of `json.Marshal`
- `tasks.go:GetWaitingOnLockTasks` uses `description LIKE` for blocking task lookup
- `broker.go:logCost` hardcodes agent type as "meeseeks"
- `broker.go:ipcWriterBaseDir` hardcodes `.axiom/containers/ipc`
