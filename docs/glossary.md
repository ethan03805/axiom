# Glossary

Definitions of terms used throughout the Axiom documentation.

---

| Term | Definition |
|------|-----------|
| **Axiom** | AI agent orchestration platform for autonomous software development |
| **Base Snapshot** | Git SHA that a TaskSpec was generated against. Used by the merge queue to detect stale context. |
| **BitNet** | Local 1-bit quantized inference engine (Falcon3) for trivial tasks. Zero cost, zero latency. |
| **Bootstrap Mode** | Orchestrator phase during SRS generation with scoped context: repo-map for existing projects, prompt-only for greenfield. |
| **Claw** | AI assistant platform (OpenClaw/NanoClaw) used as the recommended orchestrator runtime. |
| **Context Invalidation Warning** | Optional IPC message from engine to Meeseeks when committed changes affect symbols in the Meeseeks' current TaskSpec context. |
| **Context Tier** | Level of project context in a TaskSpec: symbol, file, package, repo-map, or indexed query. Lower tiers preferred. |
| **ECO (Engineering Change Order)** | Controlled process for adapting to environmental changes (dead APIs, broken dependencies) without modifying project scope. Six categories: ECO-DEP, ECO-API, ECO-SEC, ECO-PLT, ECO-LIC, ECO-PRV. |
| **Embedded Mode** | Orchestrator deployment mode where the orchestrator runs inside a Docker container with all inference brokered through the engine. Full budget tracking. Used by Claude Code, Codex, OpenCode. |
| **External Client Mode** | Orchestrator deployment mode where the orchestrator connects via REST API with its own inference provider. Partial budget tracking (engine-side only). Used by Claw orchestrators. |
| **File Router** | Engine component that brokers files from container staging to the project filesystem through the five-stage approval pipeline. |
| **Grammar Constraints** | GBNF rules restricting BitNet model output to valid syntax. Prevents malformed output from 1-bit models. |
| **Inference Broker** | Engine component mediating ALL model API calls. Validates model allowlists, enforces budget, rate limits, and logs every request. |
| **IPC (Inter-Process Communication)** | Filesystem-based communication between the engine and containers using JSON files in shared directories. |
| **Meeseeks** | Disposable, single-task AI worker agent. Named after Mr. Meeseeks from Rick and Morty. Born for one task, destroyed after completion. Never persists between tasks. |
| **Merge Queue** | Serialized commit pipeline that processes approved outputs one at a time, validating against current HEAD before each commit. |
| **Model Registry** | Catalog of available AI models with capabilities, pricing, and historical performance data. |
| **Output Manifest** | JSON declaration (`manifest.json`) of all file operations a Meeseeks performed (adds, modifications, deletions, renames). |
| **Reviewer** | Disposable AI agent that evaluates Meeseeks output against the original TaskSpec. Uses a different model family than the Meeseeks for standard+ tiers. |
| **ReviewSpec** | Document combining the original TaskSpec, Meeseeks output, and validation results, given to the reviewer for evaluation. |
| **Scope Expansion** | Runtime mechanism allowing a Meeseeks to request modification of files outside its originally declared scope. Requires engine validation and orchestrator approval. |
| **Semantic Indexer** | Code analysis system that maintains a queryable index of symbols, exports, interfaces, and dependencies. Enables precise TaskSpec context construction. |
| **SRS (Software Requirements Specification)** | The immutable project definition document generated from the user's prompt and approved before execution begins. |
| **Sub-Orchestrator** | Delegated LLM agent managing a subtree of the task tree. Created by the main orchestrator for complex subsystems. |
| **TaskSpec** | Self-contained task description delivered to a Meeseeks. Includes objective, context, interface contract, constraints, and acceptance criteria. |
| **Trusted Engine** | The Go control plane running on the host. Performs all privileged operations: filesystem writes, git commits, container spawning, inference brokering, budget enforcement. |
| **Untrusted Agent Plane** | All LLM agents running inside Docker containers. They propose actions; the engine executes them. |
| **Untrusted Artifact Execution Plane** | Validation sandboxes that run untrusted generated code. Distinct from the Agent Plane. No network, no secrets. |
| **Validation Sandbox** | Isolated Docker container for running compilation, linting, and tests against untrusted generated code. |
| **Warm Sandbox Pool** | Pre-warmed validation containers synced to HEAD for reduced validation latency. Experimental, behind feature flag. |
| **Write-Set Lock** | File-level lock preventing concurrent modification of the same resources by parallel Meeseeks. |
