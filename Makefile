.PHONY: build test lint docker-images clean

BINARY_NAME=axiom
BUILD_DIR=bin

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/axiom

test:
	go test ./...

lint:
	@command -v golangci-lint >/dev/null 2>&1 || { echo "golangci-lint not installed"; exit 1; }
	golangci-lint run ./...

docker-images:
	docker build -t axiom-worker:latest -f Dockerfile.worker .
	docker build -t axiom-validator:latest -f Dockerfile.validator .

clean:
	rm -rf $(BUILD_DIR)
	go clean -cache
