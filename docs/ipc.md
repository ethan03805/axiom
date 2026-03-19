# IPC Protocol

Axiom uses filesystem-based inter-process communication (IPC) between the engine and Docker containers. All messages are JSON files written to shared directories.

---

## How It Works

```
Engine                              Container
------                              ---------

Write JSON to:                      Watch /workspace/ipc/input/
.axiom/containers/ipc/<id>/input/   (inotify or polling)
        |                                   |
        +---------> filesystem ------------>+
                                            |
                                    Process message
                                    Write response to:
                                    /workspace/ipc/output/
        +<---------- filesystem <-----------+
        |
Watch .axiom/containers/ipc/<id>/output/
(fsnotify)
```

### Direction

| Direction | Host Path | Container Path |
|-----------|-----------|---------------|
| Engine to Container | `.axiom/containers/ipc/<task-id>/input/` | `/workspace/ipc/input/` |
| Container to Engine | `.axiom/containers/ipc/<task-id>/output/` | `/workspace/ipc/output/` |

### File Naming

Messages are named: `<message-type>-<sequence-number>.json`

Example: `inference_request-001.json`, `task_output-001.json`

### Detection

- **Engine side**: Uses `fsnotify` to watch all active container output directories. Sub-100ms detection.
- **Container side**: Uses Linux `inotify` to watch input directory. Falls back to 1-second polling if inotify is unavailable.

---

## Message Types

| Type | Direction | Purpose |
|------|-----------|---------|
| `task_spec` | Engine -> Meeseeks | Deliver TaskSpec for execution |
| `review_spec` | Engine -> Reviewer | Deliver ReviewSpec for evaluation |
| `revision_request` | Engine -> Meeseeks | Return feedback for revision |
| `task_output` | Meeseeks -> Engine | Submit completed work + manifest |
| `review_result` | Reviewer -> Engine | Submit review verdict |
| `inference_request` | Any Agent -> Engine | Request model inference |
| `inference_response` | Engine -> Any Agent | Return inference result |
| `lateral_message` | Engine <-> Meeseeks | Brokered lateral communication |
| `action_request` | Agent -> Engine | Request privileged action (spawn, query) |
| `action_response` | Engine -> Agent | Return action result |
| `request_scope_expansion` | Meeseeks -> Engine | Request additional files |
| `scope_expansion_response` | Engine -> Meeseeks | Approval or denial |
| `context_invalidation_warning` | Engine -> Meeseeks | Referenced symbols changed |
| `shutdown` | Engine -> Container | Request graceful shutdown |

---

## Inference Request/Response

### Request Format

```json
{
    "type": "inference_request",
    "task_id": "task-042",
    "model_id": "anthropic/claude-sonnet-4",
    "messages": [
        {"role": "system", "content": "..."},
        {"role": "user", "content": "..."}
    ],
    "max_tokens": 8192,
    "temperature": 0.2,
    "grammar_constraints": null
}
```

### Response Format

```json
{
    "type": "inference_response",
    "task_id": "task-042",
    "model_id": "anthropic/claude-sonnet-4",
    "content": "...",
    "input_tokens": 1234,
    "output_tokens": 567,
    "cost_usd": 0.0089
}
```

### Streaming

Streaming responses use sequential chunk files:

```
response-001.json  (first chunk)
response-002.json  (second chunk)
response-003.json  (third chunk, final=true)
```

The container watches for new chunk files via inotify and processes them incrementally.

---

## Scope Expansion Messages

### Request

```json
{
    "type": "request_scope_expansion",
    "task_id": "task-042",
    "additional_files": [
        "src/routes/api.go",
        "src/middleware/cors.go"
    ],
    "reason": "Need to update API route registration to match new handler signature"
}
```

### Response (Approved)

```json
{
    "type": "scope_expansion_response",
    "task_id": "task-042",
    "status": "approved",
    "expanded_files": ["src/routes/api.go", "src/middleware/cors.go"],
    "locks_acquired": true
}
```

### Response (Lock Conflict)

```json
{
    "type": "scope_expansion_response",
    "task_id": "task-042",
    "status": "waiting_on_lock",
    "blocked_by": "task-038",
    "message": "Container will be destroyed and task re-queued when locks are available"
}
```

---

## Action Request/Response

### Orchestrator Actions

| Action | Description |
|--------|-------------|
| `submit_srs` | Submit generated SRS for approval |
| `submit_eco` | Propose an ECO |
| `create_task` | Add a task to the tree |
| `create_task_batch` | Add multiple tasks atomically |
| `spawn_meeseeks` | Request Meeseeks for a task |
| `spawn_reviewer` | Request reviewer for a task |
| `spawn_sub_orchestrator` | Request sub-orchestrator |
| `approve_output` | Approve Meeseeks output |
| `reject_output` | Reject with feedback |
| `query_index` | Query semantic indexer |
| `query_status` | Get task tree state |
| `query_budget` | Get budget status |
| `request_inference` | Submit inference request |

### Request Format

```json
{
    "type": "action_request",
    "action": "create_task",
    "task_id": "orchestrator-001",
    "payload": {
        "title": "Implement user registration handler",
        "parent_id": "task-010",
        "tier": "standard",
        "task_type": "implementation",
        "target_files": ["src/handlers/register.go"],
        "srs_refs": ["FR-001", "AC-001"],
        "dependencies": ["task-005"]
    }
}
```

---

## Handler Dispatch

**File:** `internal/ipc/handler.go`

Incoming messages are dispatched by type to the appropriate engine subsystem:

| Message Type | Routed To |
|-------------|-----------|
| `inference_request` | Inference Broker |
| `task_output` | File Router / Approval Pipeline |
| `review_result` | Pipeline stage advancement |
| `action_request` | Action dispatcher (spawn, query, etc.) |
| `request_scope_expansion` | Scope expansion handler |

Responses are written back to the container's input directory.
