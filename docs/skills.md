# Skill System

The skill system teaches orchestrator runtimes how to use Axiom. Each supported runtime has a different mechanism for receiving instructions, so Axiom generates runtime-specific skill files.

---

## Purpose

When an orchestrator (Claw, Claude Code, Codex, or OpenCode) connects to Axiom, it needs to know:
- How the Axiom workflow operates
- What API endpoints or IPC messages are available
- How to construct TaskSpecs
- How to manage budgets
- The rules for task decomposition, review, and scope
- Error handling and escalation procedures

The skill system encodes all of this into the appropriate format for each runtime.

---

## Supported Runtimes

| Runtime | Skill Mechanism | Generated File |
|---------|----------------|---------------|
| Claw | Markdown skill file | `axiom-skill.md` |
| Claude Code | CLAUDE.md injection + hooks | `.claude/CLAUDE.md` + hook config |
| Codex | System prompt / instructions | `codex-instructions.md` |
| OpenCode | System prompt / instructions | `opencode-instructions.md` |

---

## Generating Skills

```bash
axiom skill generate --runtime claw
axiom skill generate --runtime claude-code
axiom skill generate --runtime codex
axiom skill generate --runtime opencode
```

Skills are regenerated when project configuration changes.

---

## Skill Content

All generated skills include these 13 topics:

| # | Topic | Description |
|---|-------|-------------|
| 1 | Workflow overview | Prompt, SRS, approval, execution flow |
| 2 | Trust boundary | Trusted Engine vs Untrusted Agent Plane |
| 3 | API/IPC reference | Available endpoints or IPC request types |
| 4 | TaskSpec format | How to construct TaskSpecs with context tiers |
| 5 | ReviewSpec format | How reviewer evaluations work |
| 6 | Context tier system | Symbol, file, package, repo-map, indexed query |
| 7 | Model registry | Available models, tiers, pricing |
| 8 | Budget management | Rules for staying within budget |
| 9 | Decomposition principles | Task sizing, independence, traceability |
| 10 | Communication model | Hierarchical, no direct agent-to-agent |
| 11 | ECO process | Valid categories, approval flow |
| 12 | Error handling | Retry, escalation, blocking procedures |
| 13 | Test separation | Different model family for tests vs implementation |

---

## Template System

**Directory:** `skills/`

Skill templates use Go `text/template` for dynamic content injection:

```
skills/
  claw.md.tmpl           # Claw skill template
  claude-code.md.tmpl    # Claude Code skill template
  codex.md.tmpl          # Codex skill template
  opencode.md.tmpl       # OpenCode skill template
```

**File:** `internal/skill/generator.go`

The generator:
1. Reads the appropriate template
2. Injects current project config, model registry data, API endpoint info
3. Writes the rendered file to the appropriate location
4. Supports regeneration on config changes

---

## Dynamic Content

Templates receive these data sources at render time:

| Source | Content |
|--------|---------|
| Project config | Budget, concurrency, orchestrator settings |
| Model registry | Available models with tiers, pricing, capabilities |
| API endpoints | Server port, available endpoints |
| Project state | Current project name, slug, branch |

---

## Claw vs Embedded Orchestrators

**Claw orchestrators** use the skill file as a reference document. The Claw reads the skill and uses Axiom's REST API to manage the project.

**Embedded orchestrators** (Claude Code, Codex, OpenCode) receive the skill as part of their system prompt or CLAUDE.md. They interact with Axiom via IPC messages from within their Docker container.

The key difference: Claw skills reference REST API endpoints; embedded skills reference IPC message types. The content is otherwise identical.
