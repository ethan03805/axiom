# Installation Guide

---

## System Requirements

| Requirement | Minimum | Recommended |
|-------------|---------|-------------|
| **Operating System** | macOS, Linux | macOS (for BitNet cookie features) |
| **Go** | 1.22+ | 1.25+ |
| **Docker** | 20.10+ | Latest stable |
| **Git** | 2.30+ | Latest stable |
| **RAM** | 8 GB | 16 GB+ (for parallel workers + BitNet) |
| **Disk** | 5 GB free | 20 GB+ (Docker images + model weights) |
| **CPU** | 4 cores | 8+ cores (for parallel containers + BitNet) |

---

## Building from Source

### Clone and Build

```bash
git clone https://github.com/ethan03805/axiom.git
cd axiom
make build
```

The compiled binary is placed at `./bin/axiom`.

### Build Targets

| Target | Command | Description |
|--------|---------|-------------|
| Binary only | `make build` | Compile the `axiom` CLI binary |
| Tests | `make test` | Run all unit tests across 22 packages |
| Lint | `make lint` | Run golangci-lint (must be installed separately) |
| Docker images | `make docker-images` | Build all 4 Meeseeks container images |
| GUI | `make gui` | Build the Wails React frontend |
| Everything | `make all` | Build binary + Docker images + GUI |
| Clean | `make clean` | Remove build artifacts and Go cache |

### Installing the Binary

Move the binary to a directory in your PATH:

```bash
sudo cp ./bin/axiom /usr/local/bin/axiom
axiom version
```

Or add the `bin/` directory to your PATH:

```bash
export PATH="$PATH:$(pwd)/bin"
```

---

## Docker Images

Axiom uses Docker containers for all AI worker execution. Four language-specific images are provided:

| Image | Contents | Size |
|-------|----------|------|
| `axiom-meeseeks-go` | Go toolchain + golangci-lint | ~500 MB |
| `axiom-meeseeks-node` | Node.js + npm + TypeScript + eslint | ~400 MB |
| `axiom-meeseeks-python` | Python 3 + pip + ruff + mypy | ~350 MB |
| `axiom-meeseeks-multi` | Go + Node.js + Python (default) | ~900 MB |

Build all images:

```bash
make docker-images
```

Build a specific image:

```bash
docker build -t axiom-meeseeks-go:latest -f docker/Dockerfile.meeseeks-go .
```

### Custom Images

You can create custom Docker images with additional toolchains. Your image must:

1. Include a non-root user for execution
2. Include the IPC watcher script
3. Not contain any pre-installed API keys or secrets
4. Be configured in `.axiom/config.toml`:

```toml
[docker]
image = "my-custom-axiom-image:v1"
```

---

## Dependencies

### Go Dependencies (Managed by go.mod)

| Package | Purpose |
|---------|---------|
| `github.com/BurntSushi/toml` | TOML configuration parsing |
| `github.com/spf13/cobra` | CLI framework |
| `modernc.org/sqlite` | Pure-Go SQLite driver (no CGO) |
| `github.com/docker/docker` | Docker SDK for container management |
| `github.com/gorilla/websocket` | WebSocket support for API server |
| `github.com/fsnotify/fsnotify` | Filesystem watching for IPC |
| `github.com/google/uuid` | UUID generation for tokens and IDs |

All dependencies are managed through Go modules. Run `go mod download` to fetch them.

### External Dependencies

| Dependency | Required | Purpose |
|------------|----------|---------|
| Docker | Yes | Worker container isolation |
| Git | Yes | Branch management, commits |
| golangci-lint | Optional | Code linting (for `make lint`) |
| cloudflared | Optional | Cloudflare Tunnel for remote orchestrators |
| BitNet/bitnet.cpp | Optional | Local inference server |
| Node.js + npm | Optional | GUI frontend build |

---

## Global Configuration

Create the global Axiom configuration directory:

```bash
mkdir -p ~/.axiom
```

Create `~/.axiom/config.toml` with your defaults:

```toml
[openrouter]
api_key = "sk-or-v1-your-key-here"

[budget]
max_usd = 10.00
warn_at_percent = 80

[bitnet]
enabled = false
host = "localhost"
port = 3002
```

This file provides defaults for all Axiom projects on your machine. Project-specific `.axiom/config.toml` files override these values.

---

## BitNet Local Inference (Optional)

BitNet enables free, zero-latency local inference for trivial tasks. To set up:

### 1. Start the BitNet Server

```bash
axiom bitnet start
```

On first run, if no model weights are present, Axiom prompts you to download the Falcon3 1-bit model weights. Confirm the download and wait for completion.

Model weights are stored in `~/.axiom/bitnet/models/`.

### 2. Verify

```bash
axiom bitnet status
axiom bitnet models
```

### 3. Configure

```toml
# ~/.axiom/config.toml or .axiom/config.toml
[bitnet]
enabled = true
host = "localhost"
port = 3002
max_concurrent_requests = 4
cpu_threads = 4
```

---

## GUI Dashboard (Optional)

The GUI requires Node.js and npm for the frontend build:

```bash
# Install frontend dependencies and build
make gui

# Or manually:
cd gui/frontend
npm install
npm run build
```

The GUI is a Wails v2 desktop application (Go backend + React frontend). Launch it after building.

---

## Verifying the Installation

Run the full diagnostic check:

```bash
axiom doctor
```

This validates:

- Docker daemon is running and accessible
- Docker version is compatible
- BitNet server status (if configured)
- Network connectivity to OpenRouter
- System resource availability (CPU, memory, disk)
- Docker image availability for configured Meeseeks image
- Secret scanner regex patterns are valid

A passing `axiom doctor` means you're ready to use Axiom.

---

## Upgrading

To upgrade Axiom to the latest version:

```bash
cd /path/to/axiom
git pull origin main
make build
make docker-images  # Rebuild Docker images if they changed
```

If you installed the binary system-wide:

```bash
sudo cp ./bin/axiom /usr/local/bin/axiom
```

---

## Uninstalling

```bash
# Remove the binary
sudo rm /usr/local/bin/axiom

# Remove Docker images
docker rmi axiom-meeseeks-go:latest
docker rmi axiom-meeseeks-node:latest
docker rmi axiom-meeseeks-python:latest
docker rmi axiom-meeseeks-multi:latest

# Remove global configuration and cached data
rm -rf ~/.axiom

# Remove project-specific data (per project)
rm -rf .axiom/
```
