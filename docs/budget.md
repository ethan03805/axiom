# Budget & Cost Management

Axiom enforces a hard budget ceiling on every project. You are never charged more than you authorize.

---

## How Budget Enforcement Works

Before every inference request, the engine:

1. Calculates the maximum possible cost: `max_tokens x model_price_per_token`
2. Checks remaining budget: `max_usd - total_spent`
3. Rejects the request if max possible cost exceeds remaining budget

This is dynamic per-request pre-authorization, not a fixed percentage reservation.

---

## Configuration

```toml
# .axiom/config.toml
[budget]
max_usd = 10.00          # Hard ceiling for this project
warn_at_percent = 80      # Emit budget_warning event at this threshold
```

Or via CLI:

```bash
axiom run --budget 15.00 "Build me a blog platform"
```

---

## Cost Tracking Granularity

| Level | Description |
|-------|-------------|
| **Per-request** | Model ID, input/output tokens, cost per inference call |
| **Per-attempt** | Total cost for a single Meeseeks attempt |
| **Per-task** | Total cost across all attempts (retries + escalations) |
| **Per-agent-type** | Aggregate for Meeseeks vs reviewers vs sub-orchestrators |
| **Per-model** | Aggregate per model used |
| **Per-project** | Cumulative total cost |

All cost data is stored in the `cost_log` table in SQLite.

---

## Budget Events

| Event | Trigger | Behavior |
|-------|---------|----------|
| `budget_warning` | Spend reaches `warn_at_percent` | Orchestrator adjusts strategy (cheaper models, fewer retries) |
| `budget_exhausted` | Spend reaches 100% of `max_usd` | Active containers finish, no new spawns, user prompted |

---

## Budget-Aware Orchestration

The orchestrator uses budget information to plan:

1. **Model selection** -- When budget is limited, prefer cheaper models and BitNet for more tasks
2. **Concurrency** -- Reduce parallel Meeseeks to slow spend rate if budget is tight
3. **Retry limits** -- Reduce max retries when budget is constrained
4. **Task sizing** -- More aggressive decomposition to use cheaper models

---

## Budget Exhaustion Flow

1. Active containers are allowed to finish their current work
2. No new Meeseeks containers are spawned
3. User is notified with a cost summary and progress report
4. User can:
   - Increase the budget to resume execution
   - Cancel the project

---

## External Client Mode

When using a Claw orchestrator (external client mode), budget tracking covers engine-managed costs only:

- Meeseeks inference: **tracked**
- Reviewer inference: **tracked**
- Sub-orchestrator inference: **tracked**
- Orchestrator's own inference: **NOT tracked** (uses its own provider)

Cost displays include the note: "Orchestrator inference cost not tracked (external mode)."

In embedded mode (Claude Code, Codex, OpenCode), ALL inference is tracked including the orchestrator's.

---

## Viewing Costs

### CLI

```bash
# Current spend, budget remaining, projected total
axiom status

# Detailed cost breakdown
axiom export  # includes cost data in JSON
```

### GUI Dashboard

The Cost Dashboard view shows:
- Total spent and budget remaining
- Budget gauge with color warnings (green < 80%, yellow 80-95%, red > 95%)
- Breakdown by task, by model, by agent type
- Projected total cost based on completion percentage
- Budget adjustment controls

### Completion Report

After project completion, a final cost summary is generated showing cost by category, model utilization, and comparison to budget.

---

## Cost Estimation

Model pricing is fetched from the OpenRouter API and stored in the model registry. Cost per request is calculated as:

```
cost = (input_tokens / 1_000_000) * prompt_price_per_million
     + (output_tokens / 1_000_000) * completion_price_per_million
```

BitNet (local) inference costs $0.00.

---

## Typical Costs

These are rough estimates. Actual costs depend on task complexity, model selection, retry rates, and prompt sizes.

| Project Size | Budget Range | Description |
|-------------|-------------|-------------|
| Trivial | $0.50 - $2.00 | Single-file CLI tool, simple script |
| Small | $2.00 - $5.00 | Small API with 3-5 endpoints |
| Medium | $5.00 - $15.00 | Web application with auth, CRUD, tests |
| Large | $15.00 - $50.00 | Multi-module application with complex logic |

Using BitNet for trivial tasks significantly reduces costs by handling variable renames, imports, config changes, and boilerplate locally at zero cost.
