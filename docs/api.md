# API Server

Axiom exposes a REST + WebSocket API for external orchestrators (Claws) and programmatic access to project management.

---

## Starting the Server

```bash
axiom api start    # Start on configured port (default 3000)
axiom api stop     # Stop the server
```

---

## Authentication

All API requests require a Bearer token:

```
Authorization: Bearer axm_sk_<32-hex-chars>
```

### Token Management

```bash
# Generate a full-control token (default)
axiom api token generate

# Generate a read-only token with 8-hour expiry
axiom api token generate --scope read-only --expires 8h

# List active tokens
axiom api token list

# Revoke a token
axiom api token revoke <token-id>
```

### Token Properties

| Property | Description |
|----------|-------------|
| Format | `axm_sk_<32-hex-chars>` |
| ID | Short identifier (8 hex chars) for listing/revocation |
| Scope | `read-only` (GET only) or `full-control` (all methods) |
| Expiration | Default 24 hours, configurable |
| Storage | `~/.axiom/api-tokens/*.json` |

### Security Features

- **Rate limiting**: Per-token, configurable (default 120 RPM). Returns `429 Too Many Requests` with `Retry-After` header.
- **IP restrictions**: Optional allowlist with CIDR support.
- **Audit logging**: All requests logged including failed auth attempts with source IP.

---

## REST Endpoints

### Project Lifecycle

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/projects` | Create a new project |
| `POST` | `/api/v1/projects/:id/run` | Submit prompt and start execution |
| `POST` | `/api/v1/projects/:id/srs/approve` | Approve the generated SRS |
| `POST` | `/api/v1/projects/:id/srs/reject` | Reject SRS with feedback |
| `POST` | `/api/v1/projects/:id/eco/approve` | Approve an ECO |
| `POST` | `/api/v1/projects/:id/eco/reject` | Reject an ECO |
| `POST` | `/api/v1/projects/:id/pause` | Pause execution |
| `POST` | `/api/v1/projects/:id/resume` | Resume execution |
| `POST` | `/api/v1/projects/:id/cancel` | Cancel execution |

### Data Queries

| Method | Endpoint | Description |
|--------|----------|-------------|
| `GET` | `/api/v1/projects/:id/status` | Project status, budget, task tree, resources |
| `GET` | `/api/v1/projects/:id/tasks` | Task tree with statuses |
| `GET` | `/api/v1/projects/:id/tasks/:tid/attempts` | Attempt history for a specific task |
| `GET` | `/api/v1/projects/:id/costs` | Cost breakdown (per-task, per-model, per-agent-type) |
| `GET` | `/api/v1/projects/:id/events` | Event log |
| `GET` | `/api/v1/models` | Model registry |

### Semantic Index

| Method | Endpoint | Description |
|--------|----------|-------------|
| `POST` | `/api/v1/index/query` | Query the semantic index (structured JSON body) |

**Request body:**
```json
{
    "query_type": "reverse_dependencies",
    "params": {
        "symbol_name": "HandleAuth"
    }
}
```

---

## WebSocket

### Event Stream

```
ws://localhost:3000/ws/projects/:id
```

Connect to receive real-time project events filtered by project ID. Events include task completions, reviews, errors, budget warnings, ECO proposals, and all other engine events.

Events are JSON objects matching the `Event` structure from the events table.

---

## Endpoint Details

### POST /api/v1/projects/:id/run

Start project execution with a prompt.

**Request:**
```json
{
    "prompt": "Build a REST API for user management",
    "budget": 10.00
}
```

### POST /api/v1/projects/:id/srs/reject

Reject SRS with feedback for revision.

**Request:**
```json
{
    "feedback": "Please add rate limiting to the API requirements"
}
```

### POST /api/v1/projects/:id/eco/approve

Approve a specific ECO.

**Request:**
```json
{
    "eco_id": 1
}
```

### GET /api/v1/projects/:id/status

**Response:**
```json
{
    "phase": "executing",
    "progress_percent": 45.0,
    "elapsed_seconds": 120,
    "active_meeseeks": 3,
    "budget": {
        "max_usd": 10.00,
        "spent_usd": 4.50,
        "remaining_usd": 5.50,
        "projected_total_usd": 9.00
    },
    "tasks": {
        "total": 12,
        "done": 5,
        "in_progress": 3,
        "queued": 4
    }
}
```

### GET /api/v1/projects/:id/costs

**Response:**
```json
{
    "project_total_usd": 4.50,
    "budget_used_percent": 45.0,
    "remaining_usd": 5.50,
    "projected_total_usd": 9.00,
    "by_task": [...],
    "by_model": [...],
    "by_agent_type": [...],
    "external_mode": false
}
```

---

## Middleware Stack

Requests pass through middleware in this order:

1. **Audit** -- Logs method, path, status, token_id, client_ip, latency
2. **IP Restriction** -- Checks against configured allowlist (if any)
3. **Authentication** -- Validates Bearer token (existence, expiration, revocation, scope)
4. **Rate Limiting** -- Enforces per-token RPM limit

---

## Cloudflare Tunnel

For remote Claw connections (NanoClaw in Docker, remote systems):

```bash
axiom tunnel start
# Outputs: https://<random>.trycloudflare.com

axiom tunnel stop
```

Requires `cloudflared` to be installed. The tunnel exposes the local API server to the internet.

**Security note:** Remote orchestration via tunnel exposes project metadata (task trees, status, semantic index queries) to the remote Claw. Understand this before enabling.

For local Claw connections (OpenClaw on same machine), connect directly to `http://localhost:3000`.

---

## Configuration

```toml
[api]
port = 3000              # Listen port
rate_limit_rpm = 120     # Requests per minute per token
allowed_ips = []         # IP allowlist (empty = allow all)
```
