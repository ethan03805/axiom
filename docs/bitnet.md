# BitNet Local Inference

BitNet provides free, zero-latency local inference for trivial tasks using 1-bit quantized models running on your hardware.

---

## Why Local Inference?

Many tasks in a software project are trivial: variable renames, import additions, config changes, boilerplate generation. Sending these to a cloud API wastes money and adds latency. BitNet handles them locally at zero cost with sub-second response times.

By routing trivial tasks to BitNet and reserving cloud models for complex work, Axiom significantly reduces total project cost.

---

## Architecture

```
Meeseeks (in Docker container)
    |
    | inference_request (via IPC)
    v
Engine Inference Broker
    |
    | (routes based on task tier)
    v
BitNet Server (localhost:3002)
    |
    | OpenAI-compatible API
    v
Falcon3 1.58-bit model
    |
    v
Response back through Broker -> IPC -> Meeseeks
```

Meeseeks never know whether they're talking to BitNet or OpenRouter. The Inference Broker handles routing transparently.

---

## Setup

### Starting the Server

```bash
axiom bitnet start
```

On first run, if model weights are absent, Axiom prompts for download confirmation and fetches Falcon3 1-bit weights to `~/.axiom/bitnet/models/`.

### Checking Status

```bash
axiom bitnet status    # Server status, memory usage, active requests
axiom bitnet models    # List available local models
```

### Stopping

```bash
axiom bitnet stop
```

---

## Configuration

```toml
# .axiom/config.toml or ~/.axiom/config.toml
[bitnet]
enabled = true                    # Enable/disable local inference
host = "localhost"                # Server hostname
port = 3002                       # Server port
max_concurrent_requests = 4       # Parallel requests
cpu_threads = 4                   # CPU threads allocated
```

---

## Supported Models

| Model | Quantization | Memory | Use Cases |
|-------|-------------|--------|-----------|
| Falcon3-1B | 1.58-bit | ~500 MB | Variable renames, imports, config, boilerplate, formatting |

The Falcon3 series is selected for:
- Small memory footprint (runnable on consumer hardware)
- Adequate capability for trivial code tasks
- Zero API cost

---

## Grammar-Constrained Decoding

For structured output (JSON, specific code patterns), BitNet supports GBNF (Generalized Backus-Naur Form) grammar constraints:

```json
{
    "type": "inference_request",
    "model_id": "local/falcon3-1b",
    "messages": [...],
    "max_tokens": 1024,
    "grammar_constraints": "root ::= \"{\" ws ... \"}\""
}
```

Grammar constraints physically prevent the model from generating tokens outside the specified grammar. This is critical for 1-bit model reliability -- without constraints, models may produce malformed output that fails validation.

---

## Task Routing

| Task Tier | Routing |
|-----------|---------|
| Local | Always to BitNet (when enabled) |
| Cheap | Cloud (OpenRouter) by default |
| Standard | Cloud (OpenRouter) |
| Premium | Cloud (OpenRouter) |

### Fallback Behavior

When OpenRouter is unavailable:
- Local-tier tasks: auto-route to BitNet
- Other tiers: queue until connectivity restores

When BitNet is unavailable:
- Local-tier tasks are queued until BitNet is back
- Other tiers are unaffected

---

## Sensitive File Routing

When `force_local_for_sensitive = true` (default), tasks touching sensitive files (`.env*`, `*credentials*`, etc.) are forced to BitNet regardless of tier. This ensures secrets never leave your machine.

```toml
[security]
force_local_for_sensitive = true
```

---

## Resource Management

BitNet uses local CPU and RAM. The engine tracks resource usage separately from container concurrency:

- CPU threads are configurable
- Memory usage is monitored
- Warnings fire if combined load (containers + BitNet) exceeds system capacity

BitNet tasks do **not** count against the `max_meeseeks` container concurrency limit. They use a separate local resource pool.

---

## Cost Savings

BitNet inference costs $0.00 per request. For a typical project:

| Without BitNet | With BitNet |
|---------------|------------|
| All tasks use cloud models | Trivial tasks use local (free) |
| $10.00 total | $6.00 total (40% savings) |

The savings depend on the ratio of trivial to complex tasks. Projects with many small, mechanical changes benefit most.
