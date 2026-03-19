.PHONY: build test lint docker-images gui all clean bitnet bitnet-model

BINARY_NAME=axiom
BUILD_DIR=bin
BITNET_DIR=third_party/BitNet
BITNET_VENV=$(BITNET_DIR)/.venv
BITNET_SERVER=$(BITNET_DIR)/build/bin/llama-server
BITNET_MODEL=$(BITNET_DIR)/models/Falcon3-1B-Instruct-1.58bit/ggml-model-i2_s.gguf

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/axiom

test:
	go test ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

# Build the vendored BitNet inference engine (llama-server with 1.58-bit kernel support).
# Prerequisites: python3, cmake, clang
bitnet: $(BITNET_SERVER)

$(BITNET_SERVER): $(BITNET_DIR)/setup_env.py
	@echo "Building BitNet inference engine..."
	@test -d $(BITNET_DIR) || { echo "Error: third_party/BitNet not found. Run: git clone --recursive https://github.com/microsoft/BitNet.git third_party/BitNet"; exit 1; }
	@command -v cmake >/dev/null 2>&1 || { echo "Error: cmake not found. Install with: brew install cmake"; exit 1; }
	@command -v python3 >/dev/null 2>&1 || { echo "Error: python3 not found"; exit 1; }
	@test -d $(BITNET_VENV) || python3 -m venv $(BITNET_VENV)
	$(BITNET_VENV)/bin/pip install -q -r $(BITNET_DIR)/requirements.txt
	cd $(BITNET_DIR) && .venv/bin/python setup_env.py --hf-repo tiiuae/Falcon3-1B-Instruct-1.58bit -q i2_s
	@echo "BitNet build complete: $(BITNET_SERVER)"

# Download and convert the Falcon3 1.58-bit model (runs as part of bitnet target above).
bitnet-model: $(BITNET_MODEL)

$(BITNET_MODEL): $(BITNET_SERVER)
	@echo "Model already built by setup_env.py"

docker-images:
	docker build -t axiom-meeseeks-go:latest -f docker/Dockerfile.meeseeks-go .
	docker build -t axiom-meeseeks-node:latest -f docker/Dockerfile.meeseeks-node .
	docker build -t axiom-meeseeks-python:latest -f docker/Dockerfile.meeseeks-python .
	docker build -t axiom-meeseeks-multi:latest -f docker/Dockerfile.meeseeks-multi .

gui:
	cd gui/frontend && npm install && npm run build

all: bitnet build docker-images gui

clean:
	rm -rf $(BUILD_DIR)
	go clean -cache
