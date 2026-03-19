# Inference Broker

The Inference Broker mediates ALL model API calls in Axiom. No container ever calls a model API directly. This ensures budget enforcement, model policy compliance, credential security, and complete audit logging.

---

## How It Works

```
Meeseeks (in container)
    |
    | inference_request (via IPC)
    v
Engine IPC Handler
    |
    v
Inference Broker
    |
    +-- Validate model allowlist (is model allowed for this task tier?)
    +-- Validate token budget (max_tokens x pricing <= remaining budget?)
    +-- Validate rate limit (under 50 requests per task?)
    |
    +-- Route to provider:
    |       |
    |       +-- OpenRouter (cloud inference)
    |       +-- BitNet (local inference)
    |
    v
Provider executes inference
    |
    v
Broker logs to cost_log table
    |
    v
inference_response (via IPC back to container)
```

---

## Providers

### OpenRouter (`internal/broker/openrouter.go`)

HTTP client for the OpenRouter `/api/v1/chat/completions` endpoint.

- Supports streaming (SSE) and non-streaming modes
- Exponential backoff retry: 1s, 2s, 4s (max 3 retries)
- Respects `Retry-After` header on 429 responses
- Retries on 429 (rate limit) and 5xx (server errors)
- API key loaded from `~/.axiom/config.toml`, never injected into containers

### BitNet (`internal/broker/bitnet.go`)

HTTP client for the local BitNet server at `localhost:3002`.

- OpenAI-compatible API format
- Supports grammar-constrained decoding (GBNF) for structured output
- Handles server unavailability gracefully
- Zero cost, zero latency for trivial tasks

---

## Validation

Before executing any inference request, the broker validates:

### 1. Model Allowlist

The requested model must be permitted for the task's tier:

| Task Tier | Allowed Model Tiers |
|-----------|-------------------|
| local | local only |
| cheap | local, cheap |
| standard | local, cheap, standard |
| premium | local, cheap, standard, premium |

A local-tier task cannot request a premium model.

### 2. Token Budget

```
max_possible_cost = max_tokens * model_price_per_token
remaining_budget = project_budget - total_spent

if max_possible_cost > remaining_budget:
    reject request
```

This is dynamic per-request pre-authorization, not a fixed percentage reservation.

### 3. Rate Limiting

Per-task rate limits prevent runaway inference loops. Default: 50 requests per task. Configurable.

---

## Fallback Routing

- If OpenRouter is unavailable and the task is BitNet-eligible (local tier): auto-route to BitNet
- If OpenRouter is unavailable and the task is NOT BitNet-eligible: queue until connectivity restores
- The engine emits a `provider_unavailable` event so the GUI and orchestrator are aware

---

## Streaming

Streaming responses from OpenRouter are relayed as chunked IPC files:

1. Broker receives SSE chunks from OpenRouter
2. Writes each chunk to the container's IPC input directory as `response-001.json`, `response-002.json`, etc.
3. Container uses inotify to process chunks incrementally
4. Token counts are tracked from streaming responses

---

## Credential Management

- API keys stored in `~/.axiom/config.toml` (owner-only permissions)
- Keys are loaded by the engine at startup
- Keys are **never** injected into container environments or IPC messages
- Keys can be rotated without engine restart via `axiom config reload`
- Container inference requests specify the model; the broker adds credentials at call time

---

## Audit Logging

Every inference request is logged in the `cost_log` table:

| Field | Description |
|-------|-------------|
| `task_id` | Which task made the request |
| `attempt_id` | Which attempt within the task |
| `agent_type` | meeseeks, reviewer, sub_orchestrator, orchestrator |
| `model_id` | Which model was used |
| `input_tokens` | Tokens in the prompt |
| `output_tokens` | Tokens in the response |
| `cost_usd` | Calculated cost |
| `timestamp` | When the request was made |

---

## Grammar-Constrained Decoding

For BitNet requests requiring structured output (JSON, specific code patterns), the broker includes GBNF grammar constraints:

```json
{
    "type": "inference_request",
    "model_id": "local/falcon3-1b",
    "messages": [...],
    "grammar_constraints": "root ::= \"{\" ... \"}\""
}
```

Grammar constraints physically prevent the model from generating tokens outside the specified grammar. This is critical for local-tier reliability with 1-bit quantized models.
