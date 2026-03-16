.PHONY: build test lint docker-images gui all clean

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
	docker build -t axiom-meeseeks-go:latest -f docker/Dockerfile.meeseeks-go .
	docker build -t axiom-meeseeks-node:latest -f docker/Dockerfile.meeseeks-node .
	docker build -t axiom-meeseeks-python:latest -f docker/Dockerfile.meeseeks-python .
	docker build -t axiom-meeseeks-multi:latest -f docker/Dockerfile.meeseeks-multi .

gui:
	cd gui/frontend && npm install && npm run build

all: build docker-images gui

clean:
	rm -rf $(BUILD_DIR)
	go clean -cache
