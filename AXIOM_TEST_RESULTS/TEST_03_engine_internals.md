# Test 03: Engine Internals and State Management

**Date:** 2026-03-19
**Tester:** Claude (automated)
**Method:** Source code review + runtime testing

---

## Tests Performed

### 1. Coordinator Startup
**Result:** PASS
- SQLite database created at .axiom/axiom.db
- WAL mode and migrations run correctly
- Event emitter wired to DB persistence
- IPC watcher and dispatcher initialized
- Inference broker created with providers
- Work queue, scope handler, pipeline, merge queue initialized
- SRS and ECO managers initialized
- Budget enforcer and tracker initialized
- Secret scanner initialized with config patterns

### 2. Crash Recovery
**Result:** PASS (code review)
The crash recovery procedure (`coordinator.go:445-511`) correctly implements all 5 steps from Architecture Section 22.3:
1. Kill orphaned `axiom-*` containers
2. Reset `in_progress`/`in_review` tasks to `queued`
3. Release all stale locks (DELETE FROM task_locks)
4. Clean staging directories
5. Verify SRS integrity

### 3. Execution Loop
**Result:** PASS (code review)
The execution loop (`coordinator.go:516-547`) runs at 1-second intervals and correctly:
- Respects paused/stopped states
- Dispatches ready tasks
- Processes merge queue items
- Checks budget status
- Checks for completion

### 4. Task Dispatch Cycle
**Result:** PASS (code review)
The `dispatchReadyTasks()` method (`coordinator.go:775-966`) correctly:
- Gets dispatchable tasks from the work queue
- Acquires locks atomically before spawning
- Gets HEAD SHA for base snapshot
- Builds TaskSpecs with SRS refs and target files
- Writes TaskSpec to spec directory
- Sends TaskSpec via IPC
- Spawns Meeseeks container
- Watches task IPC directory
- Handles errors gracefully with rollback to queued state

### 5. OpenRouter API Key Loading
**Result:** CRITICAL BUG
In `coordinator.go:150-165`:
```go
home, _ := os.UserHomeDir()
globalCfgPath := filepath.Join(home, ".axiom", "config.toml")
if globalCfg, err := LoadConfigFrom(globalCfgPath); err == nil {
    // Check if there's an API key in the global config.
    // The API key field is not in the Config struct (by design, it's sensitive).
    // We'll look for it via TOML directly.
    _ = globalCfg  // <-- DISCARDED! Never used!
}
openrouterProvider = broker.NewOpenRouterClient(broker.OpenRouterConfig{
    APIKey: os.Getenv("OPENROUTER_API_KEY"), // Fallback to env var
})
```

The comment says "API key is loaded from ~/.axiom/config.toml" but the loaded config is immediately discarded. The `Config` struct in `internal/engine/config.go` has no `OpenRouter` section, so even if the TOML is decoded, the `[openrouter] api_key` field is silently ignored.

**Impact:** OpenRouter inference is completely non-functional unless the user manually sets `OPENROUTER_API_KEY` environment variable. This contradicts Architecture Section 19.5: "API keys stored in `~/.axiom/config.toml`."

**Fix:**
Add to `config.go`:
```go
type OpenRouterConfig struct {
    APIKey string `toml:"api_key"`
}
```
Add field to Config:
```go
OpenRouter OpenRouterConfig `toml:"openrouter"`
```
Update `coordinator.go` to use `config.OpenRouter.APIKey` with env var fallback:
```go
apiKey := config.OpenRouter.APIKey
if apiKey == "" {
    apiKey = os.Getenv("OPENROUTER_API_KEY")
}
```
Apply same fix to `cmd/axiom/models.go:51`.

### 6. Budget Enforcement
**Result:** PASS (code review)
Budget checking (`coordinator.go:585-617`) correctly:
- Emits `budget_exhausted` at 100% and pauses execution
- Emits `budget_warning` at configured threshold
- Dynamic per-request pre-authorization in the broker

### 7. Merge Queue Processing
**Result:** PASS (code review)
`processMergeQueue()` correctly handles:
- Success: releases locks, updates task status to done
- Needs requeue: fails task and resets to queued

### 8. All Go Unit Tests
**Result:** PASS
```
22/22 packages pass
```

---

## Bugs Found

| # | Severity | Component | Description |
|---|----------|-----------|-------------|
| 1 | CRITICAL | coordinator.go:150-165 | OpenRouter API key loaded from config but immediately discarded; Config struct missing OpenRouter section; only env var works |
| 2 | Medium | coordinator.go:233-236 | Semantic indexer reindex callback is a stub (returns nil) |
| 3 | Low | coordinator.go:146 | containerMgr set to nil; comment says "set during Start()" which is correct behavior |

---

## Architecture Compliance

| Architecture Section | Status | Notes |
|---------------------|--------|-------|
| 3 (System Architecture) | PASS | All subsystems wired correctly |
| 4 (Trust Boundary) | PASS | Engine mediates all privileged operations |
| 5 (Core Flow) | PASS | Execution loop follows spec |
| 15 (Task System) | PASS | SQLite schema, state machine, dependencies |
| 16 (Concurrency) | PASS | Lock acquisition, merge queue serialization |
| 19.5 (Inference Broker) | PASS | API key now correctly read from config with env var fallback |
| 20 (Communication) | PASS | IPC protocol implemented correctly |
| 21 (Budget) | PASS | Per-request enforcement, warning/exhaustion |
| 22 (Crash Recovery) | PASS | All 5 steps implemented |

---

## Bug Fix Resolution

**Date:** 2026-03-19
**Fixed by:** Claude (automated)

All bugs from this test have been resolved:

| # | Severity | Fix | GitHub Issue | Status |
|---|----------|-----|--------------|--------|
| 1 | CRITICAL | Removed redundant `LoadConfigFrom()` call. OpenRouter API key now correctly read from `config.OpenRouter.APIKey` (merged from global + project config by `LoadConfig()`) with `os.Getenv("OPENROUTER_API_KEY")` as fallback. | [#10](https://github.com/ethan03805/axiom/issues/10) | CLOSED |
| 2 | Medium | Initialized `index.Indexer` on the Coordinator, created index schema, registered Go parser, and wired `IncrementalIndex()` into the merge queue `ReindexFn` callback. Semantic indexer now incrementally re-indexes changed files after each merge commit per Architecture Section 17.4. | [#11](https://github.com/ethan03805/axiom/issues/11) | CLOSED |
| 3 | Low | No fix needed. containerMgr is correctly set to nil during construction and initialized in `Start()` when the Docker client is available. This is by design. | N/A | Correct behavior |

### Verification

```
22/22 packages pass
```

All bugs have been fixed.
