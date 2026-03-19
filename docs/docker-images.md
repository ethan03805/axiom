# Docker Images

Axiom provides four language-specific Docker images for Meeseeks workers and validation sandboxes.

---

## Available Images

| Image | Contents | Dockerfile |
|-------|----------|-----------|
| `axiom-meeseeks-go` | Go toolchain + golangci-lint | `docker/Dockerfile.meeseeks-go` |
| `axiom-meeseeks-node` | Node.js + npm + TypeScript + eslint | `docker/Dockerfile.meeseeks-node` |
| `axiom-meeseeks-python` | Python 3 + pip + ruff + mypy | `docker/Dockerfile.meeseeks-python` |
| `axiom-meeseeks-multi` | Go + Node.js + Python (default) | `docker/Dockerfile.meeseeks-multi` |

---

## Building Images

Build all images:

```bash
make docker-images
```

Build a specific image:

```bash
docker build -t axiom-meeseeks-go:latest -f docker/Dockerfile.meeseeks-go .
docker build -t axiom-meeseeks-node:latest -f docker/Dockerfile.meeseeks-node .
docker build -t axiom-meeseeks-python:latest -f docker/Dockerfile.meeseeks-python .
docker build -t axiom-meeseeks-multi:latest -f docker/Dockerfile.meeseeks-multi .
```

---

## Image Requirements

Every Axiom Docker image must include:

1. **Non-root user** -- Containers always run as unprivileged user
2. **IPC watcher script** -- Reads from `/workspace/ipc/input/`, writes to `/workspace/ipc/output/`
3. **Minimal shell** -- For the LLM agent to execute within
4. **No API keys or secrets** -- Clean image with no pre-installed credentials
5. **Language toolchain** -- Compiler, package manager, linter for the target language

---

## Image Contents by Language

### Go Image

| Tool | Version | Purpose |
|------|---------|---------|
| Go compiler | 1.22+ | Compilation |
| golangci-lint | Latest | Linting |
| git | Latest | Version control operations within container |

### Node Image

| Tool | Version | Purpose |
|------|---------|---------|
| Node.js | LTS | JavaScript runtime |
| npm | Latest | Package management |
| TypeScript | Latest | TypeScript compilation |
| eslint | Latest | Linting |

### Python Image

| Tool | Version | Purpose |
|------|---------|---------|
| Python 3 | 3.11+ | Runtime |
| pip | Latest | Package management |
| ruff | Latest | Fast linting |
| mypy | Latest | Type checking |

### Multi Image (Default)

Combines all three language toolchains in a single image. Larger (~900 MB) but handles any language without image switching.

---

## Configuring the Image

In `.axiom/config.toml`:

```toml
[docker]
image = "axiom-meeseeks-multi:latest"   # Default
# image = "axiom-meeseeks-go:latest"    # Go-only projects
# image = "my-custom-image:v1"          # Custom image
```

The validation sandbox uses the **same image** as the Meeseeks to ensure toolchain version parity. This prevents false validation failures from version mismatches.

---

## Custom Images

You can create custom Docker images for projects needing additional toolchains (Rust, Java, etc.):

```dockerfile
FROM axiom-meeseeks-multi:latest

# Add Rust toolchain
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/root/.cargo/bin:${PATH}"

# Add your project-specific tools
RUN go install github.com/my/tool@latest
```

Build and configure:

```bash
docker build -t my-axiom-image:v1 -f Dockerfile.custom .
```

```toml
[docker]
image = "my-axiom-image:v1"
```

---

## Seccomp Profile

An optional seccomp profile restricts syscall access for additional security:

**File:** `docker/axiom-seccomp.json`

Apply via Docker:

```
--security-opt seccomp=docker/axiom-seccomp.json
```

This restricts containers to a minimal set of syscalls needed for code execution, blocking potentially dangerous operations.

---

## Container Runtime Configuration

All containers are spawned with these flags (applied by the engine, not in the Dockerfile):

```
--read-only
--cap-drop=ALL
--security-opt=no-new-privileges
--pids-limit=256
--tmpfs /tmp:rw,noexec,size=256m
--network=none
--user <uid>:<gid>
--cpus <from config>
--memory <from config>
```

---

## Volume Mount Layout

Inside every container:

```
/workspace/
  spec/           # Read-only: TaskSpec or ReviewSpec
  staging/        # Read-write: Meeseeks output (code + manifest)
  ipc/
    input/        # Read-write: Messages from engine
    output/       # Read-write: Messages to engine
/tmp              # tmpfs scratch space (noexec, 256MB)
```
