# Makefile for Nuts Project

# Variables
BINARY_NAME_CLI=nuts-cli
BINARY_NAME_SERVICE=nuts-service
BINARY_NAME_COLLECTOR=nuts-collector
BINARY_NAME_TEST_NRI=test-nri
BUILD_DIR=build
GO=go
GOFLAGS=-v

# Directories
CMD_DIR=cmd
PKG_DIR=pkg
CONFIG_DIR=configs
DEPLOY_DIR=deployments

# Build targets
.PHONY: all build clean test lint fmt vet deps help

all: build

## build: Build all binaries
build: build-cli build-service build-collector

## build-cli: Build CLI binary
build-cli:
	@echo "Building CLI..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME_CLI) $(CMD_DIR)/cli/main.go

## build-service: Build Service binary
build-service:
	@echo "Building Service..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME_SERVICE) $(CMD_DIR)/service/main.go

## build-collector: Build Collector binary
build-collector:
	@echo "Building Collector..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME_COLLECTOR) $(CMD_DIR)/collector/main.go

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BUILD_DIR)

## test: Run all tests
test:
	@echo "Running tests..."
	$(GO) test -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	$(GO) test -v -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

## lint: Run linter
lint:
	@echo "Running linter..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed. Install it from https://golangci-lint.run/usage/install/"; \
	fi

## fmt: Format code
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

## vet: Run go vet
vet:
	@echo "Running go vet..."
	$(GO) vet ./...

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	$(GO) mod download
	$(GO) mod tidy

## run-cli: Run CLI
run-cli: build-cli
	@echo "Running CLI..."
	./$(BUILD_DIR)/$(BINARY_NAME_CLI) $(ARGS)

## run-service: Run Service
run-service: build-service
	@echo "Running Service..."
	./$(BUILD_DIR)/$(BINARY_NAME_SERVICE)

## run-collector: Run Collector
run-collector: build-collector
	@echo "Running Collector..."
	./$(BUILD_DIR)/$(BINARY_NAME_COLLECTOR)

## run-test-nri: Run NRI test program
run-test-nri: build-test-nri
	@echo "Running NRI test program..."
	@echo "Note: This program requires NRI plugin to be registered with containerd"
	@echo "Run this program in one terminal, then use crictl in another terminal to trigger events"
	./$(BUILD_DIR)/$(BINARY_NAME_TEST_NRI)

## docker-build: Build Docker images
docker-build:
	@echo "Building Docker images..."
	docker build -t nuts-cli:latest -f Dockerfile.cli .
	docker build -t nuts-service:latest -f Dockerfile.service .
	docker build -t nuts-collector:latest -f Dockerfile.collector .

## docker-push: Push Docker images
docker-push:
	@echo "Pushing Docker images..."
	docker push nuts-cli:latest
	docker push nuts-service:latest
	docker push nuts-collector:latest

## help: Show this help message
help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
