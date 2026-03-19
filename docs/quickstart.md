# Quick Start Guide

Get a project running with Axiom in under 5 minutes.

---

## Prerequisites

Before starting, ensure you have:

- **Go 1.22+** installed (`go version`)
- **Docker** installed and running (`docker info`)
- **Git** configured with a user name and email
- An **OpenRouter API key** for cloud inference (or local hardware for BitNet-only mode)

---

## Step 1: Build from Source

```bash
git clone https://github.com/ethan03805/axiom.git
cd axiom
make build
```

This produces a single binary at `./bin/axiom`.

To also build Docker images for worker containers:

```bash
make docker-images
```

To build everything (binary, Docker images, GUI):

```bash
make all
```

---

## Step 2: Verify System Requirements

```bash
./bin/axiom doctor
```

The doctor command checks:
- Docker daemon availability and version
- BitNet server status (if configured)
- Network connectivity to OpenRouter
- System resources (CPU, memory, disk)
- Warm-pool image availability
- Secret scanner regex validity

Fix any issues reported before proceeding.

---

## Step 3: Configure Your API Key

Create the global configuration file:

```bash
mkdir -p ~/.axiom
cat > ~/.axiom/config.toml << 'EOF'
[openrouter]
api_key = "sk-or-v1-your-key-here"
EOF
```

Replace the API key with your actual OpenRouter key. This file is never committed to git and is used across all Axiom projects.

---

## Step 4: Initialize a Project

Navigate to your project directory (or create a new one):

```bash
mkdir my-project && cd my-project
git init
axiom init
```

`axiom init` creates the `.axiom/` directory structure with a default configuration file.

---

## Step 5: Configure the Project

Edit `.axiom/config.toml`:

```toml
[project]
name = "my-project"
slug = "my-project"

[budget]
max_usd = 10.00          # Maximum you're willing to spend
warn_at_percent = 80      # Warning threshold

[concurrency]
max_meeseeks = 10         # Parallel AI workers

[orchestrator]
runtime = "claw"          # claw | claude-code | codex | opencode
```

See [Configuration Reference](configuration.md) for all available options.

---

## Step 6: Run Your First Project

```bash
axiom run --budget 10.00 "Build a REST API for user management with JWT authentication, user registration, login, and profile endpoints using Go and the Gin framework"
```

Axiom will:

1. **Generate an SRS** -- The orchestrator analyzes your prompt and produces a structured Software Requirements Specification.
2. **Present for approval** -- You review the SRS. Approve to proceed, reject with feedback to iterate.
3. **Decompose into tasks** -- The approved SRS is broken into a hierarchical task tree.
4. **Execute tasks** -- Meeseeks (disposable AI workers) execute tasks in parallel within Docker containers.
5. **Validate and review** -- Each task's output is compiled, linted, tested, and reviewed by a separate AI model.
6. **Merge** -- Approved output is committed to a dedicated `axiom/my-project` git branch.
7. **Report completion** -- A cost summary and status report are produced.

---

## Step 7: Monitor Progress

While Axiom runs, use these commands:

```bash
# Show task tree, active workers, budget, resources
axiom status

# Pause execution (active workers finish, no new ones spawn)
axiom pause

# Resume a paused project
axiom resume

# Cancel everything
axiom cancel
```

For a visual dashboard, build and launch the GUI:

```bash
make gui
# Then launch the Wails app
```

---

## Step 8: Review and Merge

All Axiom-generated code is committed to a dedicated branch:

```bash
git log axiom/my-project
git diff main..axiom/my-project
```

When satisfied, merge the branch yourself:

```bash
git checkout main
git merge axiom/my-project
```

Axiom never automatically pushes to remote repositories.

---

## Example: Simple CLI Tool

```bash
mkdir hello-cli && cd hello-cli
git init
axiom init
axiom run --budget 2.00 "Build a Go CLI tool that converts CSV files to JSON. Support stdin/stdout, file input/output flags, and pretty-printing."
```

This is a small, well-defined task. Axiom will likely:
- Generate a focused SRS with 3-5 requirements
- Create 2-4 implementation tasks plus test generation tasks
- Complete in a few minutes
- Cost under $1.00

---

## Example: Web Application

```bash
mkdir my-blog && cd my-blog
git init
axiom init
axiom run --budget 15.00 "Build a blog platform with a Go backend using Gin and SQLite. Include user registration, authentication with JWT, creating/editing/deleting posts, markdown rendering, and a simple REST API. Include comprehensive tests."
```

This is a larger project. Axiom will:
- Generate a detailed SRS covering architecture, data model, API design
- Create a deeper task tree with 10-20+ tasks
- Use standard/premium models for complex tasks, cheap/local for simple ones
- Run multiple workers in parallel
- Take longer but stay within budget

---

## Next Steps

- [Configuration Reference](configuration.md) -- All configuration options explained
- [CLI Reference](cli.md) -- Complete command documentation
- [Architecture Overview](architecture-overview.md) -- How Axiom works under the hood
- [Budget & Cost Management](budget.md) -- Understanding and controlling costs
- [Security Model](security.md) -- How Axiom keeps your code safe
- [Troubleshooting](troubleshooting.md) -- Common issues and solutions
