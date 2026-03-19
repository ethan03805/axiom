# Test 01: axiom init and Basic CLI Commands

**Date:** 2026-03-19
**Tester:** Claude (automated)
**Test Directory:** test_projects/test01_init, test_projects/test02_no_git

---

## Tests Performed

### 1. axiom version
**Result:** PASS
```
axiom version dev
  go:       go1.26.1
  os/arch:  darwin/arm64
```

### 2. axiom help
**Result:** PASS
All expected commands listed: init, run, status, pause, resume, cancel, export, models, bitnet, api, tunnel, skill, index, version, doctor, config.

### 3. axiom doctor (no project)
**Result:** PASS
```
  [PASS] Docker: Docker 29.2.1
  [PASS] Git: git version 2.50.1 (Apple Git-155)
  [PASS] System Resources: 10 CPUs available
  [PASS] Configuration: No project config (run 'axiom init' first)
All checks passed. Axiom is ready to run.
```

### 4. axiom init (fresh git directory)
**Result:** PASS
- Created .axiom/config.toml with all expected sections
- Created .axiom/.gitignore with proper entries
- Created subdirectories: containers/{specs,staging,ipc}, validation, eco, logs/prompts
- No old Axiom artifacts present

### 5. axiom init (idempotent - run twice)
**Result:** PASS
```
.axiom/config.toml already exists, skipping
Axiom project initialized. Edit .axiom/config.toml to configure.
```
Does not overwrite existing config.

### 6. axiom init (without git repo)
**Result:** PASS (but potentially a bug)
- `axiom init` succeeds without a git repo
- Per Architecture Section 28.2, the engine should refuse to start on a dirty working tree
- `axiom init` should at minimum warn that git is not initialized, since `axiom run` requires git

**Bug: axiom init does not warn when no git repo exists.**

### 7. axiom status (initialized project)
**Result:** PASS
```
Project:
Budget:  $10.00 (warn at 80%)
Max Meeseeks: 10
Runtime: claw
```
Note: Project name is empty because config.toml defaults have `name = ""`.

### 8. axiom export (initialized project)
**Result:** PASS
Outputs valid JSON with config, empty tasks, zero cost.

### 9. axiom doctor (initialized project)
**Result:** PASS
```
  [PASS] Configuration: Project config found
```

### 10. axiom models refresh
**Result:** PARTIAL PASS (bug found)
```
  OpenRouter API key not set (OPENROUTER_API_KEY). Skipping.
Scanning local BitNet models...
  Added local/falcon3-1b
Merging curated capability data...
Model registry updated. 1 models total.
```

**Bug: OpenRouter API key not read from config.** The code at `cmd/axiom/models.go:51` reads `os.Getenv("OPENROUTER_API_KEY")` but the API key is stored in `~/.axiom/config.toml` under `[openrouter] api_key`. The `Config` struct in `internal/engine/config.go` has no `OpenRouter` section at all. The architecture specifies in Section 19.5: "API keys stored in `~/.axiom/config.toml`."

**Fix:** Add an `OpenRouter` section to the Config struct:
```go
type OpenRouterConfig struct {
    APIKey string `toml:"api_key"`
}
```
And update `models.go` to load config and use the key from there, falling back to env var.

### 11. axiom models list / info
**Result:** PASS
Both commands work correctly after refresh.

### 12. axiom index refresh
**Result:** PASS
```
Re-indexing project...
Index refreshed: 0 symbols across 0 files.
```
Correct for an empty project.

### 13. axiom skill generate --runtime claw
**Result:** BUG FOUND
- Wrote skill file to `/Users/ethantriska/NewAxiom/axiom/axiom-skill.md` instead of the current test project directory
- `findProjectRoot()` in `cmd/axiom/skill.go` walks up the directory tree looking for a `skills/` subdirectory. It found `/Users/ethantriska/NewAxiom/axiom/skills/` (the Axiom source code directory) and used that as the "project root"
- This means skill generation writes to the WRONG location when the user's project is anywhere under the Axiom repo

**Bug: skill generate uses wrong project root.** The `findProjectRoot()` function should look for `.axiom/` (an initialized Axiom project) not `skills/` (the source template directory). The template directory should be determined separately (e.g., from the binary's location or an embedded filesystem).

**Fix:** Change `findProjectRoot()` to look for `.axiom/config.toml` or `.axiom/` directory. Load templates from the binary's embedded filesystem or a known install path, not by walking up the tree.

### 14. axiom skill generate --runtime claude-code
**Result:** BUG FOUND (same as #13)
- Wrote to `/Users/ethantriska/NewAxiom/axiom/.claude/CLAUDE.md` instead of test project
- Also: this OVERWRITES the existing CLAUDE.md for the Axiom project itself, which is destructive

### 15. axiom bitnet status
**Result:** PASS
```
BitNet Server Status
--------------------
Status:          stopped
Enabled:         true
Host:            localhost
Port:            3002
CPU Threads:     4 / 10
CPU Usage:       40.0%
Active Requests: 0
Model Weights:   not found
```

### 16. axiom api token generate
**Result:** PASS
Successfully generates `axm_sk_*` token with ID, scope, and expiration.

---

## Bugs Found

| # | Severity | Component | Description |
|---|----------|-----------|-------------|
| 1 | Medium | cmd/axiom/models.go | OpenRouter API key only read from env var, not from ~/.axiom/config.toml |
| 2 | High | cmd/axiom/skill.go | findProjectRoot() walks up looking for skills/ dir, writes to wrong location |
| 3 | Low | cmd/axiom/project.go | axiom init doesn't warn when no git repo exists |
| 4 | Medium | internal/engine/config.go | Config struct missing OpenRouter section for API key storage |
| 5 | Medium | cmd/axiom/models.go:85 | models.json path is relative to cwd, should be relative to binary/install |

---

## Bug Fixes Applied

**Date:** 2026-03-19
**Fixed By:** Claude (automated)

All 5 bugs have been fixed and verified by re-running the same tests outlined above. GitHub issues were created for each bug, fixes were implemented, tests re-run to confirm, and issues closed.

### Fix Summary

| # | Issue | Fix Description | Files Changed |
|---|-------|----------------|---------------|
| 1 | [#1](https://github.com/ethan03805/axiom/issues/1) | Added `OpenRouterConfig` struct; `models.go` now reads API key from `~/.axiom/config.toml` first, falls back to `OPENROUTER_API_KEY` env var | `internal/engine/config.go`, `cmd/axiom/models.go` |
| 2 | [#2](https://github.com/ethan03805/axiom/issues/2) | Changed `findProjectRoot()` to look for `.axiom/` instead of `skills/`; templates now embedded in binary via Go embed (`skills/embed.go`); skill files written to user's project directory | `cmd/axiom/skill.go`, `internal/skill/generator.go`, `skills/embed.go` (new) |
| 3 | [#3](https://github.com/ethan03805/axiom/issues/3) | Added git repo check in `axiom init` that prints a warning if `.git/` is absent | `cmd/axiom/project.go` |
| 4 | [#4](https://github.com/ethan03805/axiom/issues/4) | Added `OpenRouterConfig` struct with `APIKey` field to the `Config` struct; `[openrouter]` TOML section is now parsed | `internal/engine/config.go` |
| 5 | [#5](https://github.com/ethan03805/axiom/issues/5) | Added `findCuratedModelsJSON()` that searches next to binary, `~/.axiom/models.json`, and cwd as fallback | `cmd/axiom/models.go` |

### Verification Results

All 16 original tests re-run and passing:

- Test 1 (version): PASS
- Test 2 (help): PASS
- Test 3 (doctor no project): PASS
- Test 4 (init fresh): PASS
- Test 5 (init idempotent): PASS
- Test 6 (init no git): PASS -- now prints warning
- Test 7 (status): PASS
- Test 8 (export): PASS
- Test 9 (doctor initialized): PASS
- Test 10 (models refresh): PASS -- now reads API key from config, fetches 350 models
- Test 11 (models list/info): PASS
- Test 12 (index refresh): PASS
- Test 13 (skill generate claw): PASS -- writes to correct project directory
- Test 14 (skill generate claude-code): PASS -- writes to correct project directory, no longer overwrites source repo
- Test 15 (bitnet status): PASS
- Test 16 (api token generate): PASS

All unit tests (`go test ./...`): PASS (0 failures)
