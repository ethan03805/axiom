# Troubleshooting

Common issues and solutions when using Axiom.

---

## Diagnostic Tool

Always start with:

```bash
axiom doctor
```

This validates Docker, BitNet, network connectivity, system resources, images, and configuration.

---

## Installation & Setup

### "Docker daemon not running"

**Symptom:** `axiom doctor` reports Docker is unavailable.

**Solution:**
```bash
# macOS
open -a Docker

# Linux
sudo systemctl start docker
```

Verify: `docker info`

### "Go version too old"

**Symptom:** Build fails with syntax errors or module issues.

**Solution:** Axiom requires Go 1.22+. Check with `go version` and upgrade if needed.

### "Docker images not found"

**Symptom:** `axiom run` fails to spawn Meeseeks containers.

**Solution:**
```bash
make docker-images
```

Verify: `docker images | grep axiom-meeseeks`

---

## Project Initialization

### "Dirty working tree"

**Symptom:** `axiom run` refuses to start.

**Cause:** The engine refuses to start on a dirty git working tree because uncommitted changes risk merge conflicts.

**Solution:**
```bash
git status          # See what's changed
git stash           # Stash changes temporarily
# OR
git add . && git commit -m "WIP"
```

### "Project already initialized"

**Symptom:** `axiom init` refuses to run.

**Cause:** `.axiom/` directory already exists.

**Solution:** Delete `.axiom/` if you want to start fresh, or use the existing project.

---

## Runtime Issues

### Tasks stuck in "queued"

**Possible causes:**
1. **Dependency not met** -- Check if dependent tasks are blocked or failed
2. **Lock conflict** -- Another task holds the write-set lock on needed files
3. **Concurrency limit** -- All Meeseeks slots are occupied
4. **Budget exhausted** -- No budget remaining for inference

**Diagnosis:**
```bash
axiom status    # Shows task tree with states and active containers
```

### Tasks repeatedly failing

**Symptom:** Task fails, retries, fails again, eventually blocked.

**Possible causes:**
1. **TaskSpec too vague** -- Orchestrator needs to provide better context
2. **Interface mismatch** -- Task's interface contract doesn't match actual code
3. **Missing dependencies** -- Code references packages not available in container
4. **Model too weak** -- Task tier too low for the complexity

**What happens:** Axiom automatically retries (max 3) then escalates to a higher model tier (max 2 escalations). If still failing after escalation, the task is marked `blocked` and the orchestrator is notified.

### "Budget exhausted"

**Symptom:** Execution pauses, user prompted.

**Solution:**
- Check current spend: `axiom status`
- Increase budget and resume
- Or cancel if the project is too expensive

### Container timeout

**Symptom:** Container killed after 30 minutes (default).

**Possible causes:**
1. Task is too large -- needs decomposition into smaller subtasks
2. Model is too slow for the task
3. Infinite loop in generated code during validation

**Solution:** The engine automatically retries with feedback. If persistent, consider increasing `timeout_minutes` in config or having the orchestrator decompose the task differently.

---

## BitNet Issues

### "BitNet server not responding"

**Symptom:** Local-tier tasks queue indefinitely.

**Solution:**
```bash
axiom bitnet status    # Check if server is running
axiom bitnet start     # Start if not running
```

### "Model weights not found"

**Symptom:** `axiom bitnet start` fails on first run.

**Solution:** Allow the download when prompted, or manually download Falcon3 1-bit weights to `~/.axiom/bitnet/models/`.

### High CPU usage from BitNet

**Symptom:** System sluggish during Axiom execution.

**Solution:** Reduce BitNet resource allocation:
```toml
[bitnet]
max_concurrent_requests = 2    # Reduce from default 4
cpu_threads = 2                 # Reduce from default 4
```

---

## API & Tunnel Issues

### "401 Unauthorized"

**Possible causes:**
1. Token expired -- Generate a new one: `axiom api token generate`
2. Token revoked -- Check: `axiom api token list`
3. Wrong scope -- Read-only tokens can't call non-GET endpoints

### "429 Too Many Requests"

**Cause:** Rate limit exceeded (default 120 RPM per token).

**Solution:** Wait for the `Retry-After` period, or increase the limit:
```toml
[api]
rate_limit_rpm = 240
```

### Tunnel connection refused

**Symptom:** Remote Claw can't connect to tunnel URL.

**Solution:**
```bash
axiom tunnel stop
axiom tunnel start    # Get a fresh tunnel URL
```

Ensure `cloudflared` is installed and has network access.

---

## Git Issues

### Merge conflicts in the Axiom branch

**Symptom:** Tasks fail in the merge queue due to conflicts.

**Cause:** Multiple parallel tasks modified overlapping code in incompatible ways.

**What happens:** The merge queue automatically re-queues conflicting tasks with updated context. If the conflict persists after retries, the orchestrator restructures affected tasks.

### Branch already exists

**Symptom:** `axiom run` fails because `axiom/<slug>` branch exists.

**Solution:** Delete the old branch if the previous project is complete:
```bash
git branch -D axiom/my-project
```

---

## Crash Recovery

### Engine crashed mid-execution

**What happens on restart:** The engine automatically:
1. Kills orphaned `axiom-*` containers
2. Resets stuck tasks (in_progress with no container) to queued
3. Releases stale write-set locks
4. Cleans staged files
5. Verifies SRS integrity
6. Resumes from SQLite state

If the orchestrator was embedded, it reconnects and reads the task tree from SQLite. No manual intervention needed.

### SRS integrity check failed

**Symptom:** Engine refuses to start, reports hash mismatch.

**Cause:** The `.axiom/srs.md` file was modified after approval.

**Solution:** Restore the original SRS from git history, or start a new project.

---

## Performance

### Slow task execution

**Possible causes:**
1. **Model latency** -- OpenRouter response times vary by model and load
2. **Too many parallel workers** -- System resource contention
3. **Large context** -- TaskSpecs with excessive context slow inference

**Solutions:**
- Reduce `max_meeseeks` if system is overloaded
- Enable BitNet for trivial tasks to reduce cloud API pressure
- Trust the orchestrator's context tier selection

### High memory usage

**Cause:** Multiple Docker containers + BitNet server + SQLite.

**Solution:**
```toml
[concurrency]
max_meeseeks = 5          # Reduce from 10

[docker]
mem_limit = "1g"           # Reduce from 2g

[bitnet]
cpu_threads = 2            # Reduce from 4
```

---

## Getting Help

If none of the above resolves your issue:

1. Check the event log: `axiom status` or the GUI Log Stream view
2. Enable prompt logging for deeper diagnosis:
   ```toml
   [observability]
   log_prompts = true
   ```
3. Check `.axiom/logs/` for runtime logs
4. Export full project state: `axiom export`
