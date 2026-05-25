.PHONY: all build build-server build-cli build-component-example run run-server run-cli run-component-example test test-race test-coverage bench bench-cpu bench-mem clean tidy fmt lint vet tools install docker-build docker-run proto help

# 构建变量
BINARY_NAME=nuts
CLI_NAME=nuts-cli
COMPONENT_EXAMPLE_NAME=component-example
BUILD_DIR=./build
CMD_DIR=./cmd/nuts
CLI_DIR=./cmd/nuts-cli
COMPONENT_EXAMPLE_DIR=./cmd/component-example

# 版本信息
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0")
BUILD_TIME=$(shell date +%Y-%m-%dT%H:%M:%S%z)
GIT_COMMIT=$(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH=$(shell git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")

# Go参数
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME) -X main.GitCommit=$(GIT_COMMIT) -X main.GitBranch=$(GIT_BRANCH)"
CLI_LDFLAGS=-ldflags "-X github.com/sig-cloudnative/nuts/pkg/cli.Version=$(VERSION) -X github.com/sig-cloudnative/nuts/pkg/cli.BuildTime=$(BUILD_TIME) -X github.com/sig-cloudnative/nuts/pkg/cli.GitCommit=$(GIT_COMMIT)"

all: tidy build

# 下载依赖
tidy:
	go mod tidy
	go mod verify

# 构建所有二进制文件
build: build-server build-cli build-component-example

# 构建服务端
build-server:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(BINARY_NAME)"

# 构建CLI客户端
build-cli:
	@echo "Building $(CLI_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build $(CLI_LDFLAGS) -o $(BUILD_DIR)/$(CLI_NAME) $(CLI_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(CLI_NAME)"

# 构建组件示例
build-component-example:
	@echo "Building $(COMPONENT_EXAMPLE_NAME)..."
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(COMPONENT_EXAMPLE_NAME) $(COMPONENT_EXAMPLE_DIR)
	@echo "Build complete: $(BUILD_DIR)/$(COMPONENT_EXAMPLE_NAME)"

# 运行服务端
run-server: build-server
	$(BUILD_DIR)/$(BINARY_NAME)

# 运行CLI客户端
run-cli: build-cli
	$(BUILD_DIR)/$(CLI_NAME)

# 运行组件示例
run-component-example: build-component-example
	$(BUILD_DIR)/$(COMPONENT_EXAMPLE_NAME)

# 运行程序（默认运行服务端）
run: run-server

# 开发模式运行（热重载）
dev:
	go run $(CMD_DIR)

# 运行测试
test:
	go test -v -timeout 30s ./...

# 运行race检测测试
test-race:
	go test -race -v -timeout 30s ./...

# 运行测试并生成覆盖率报告
test-coverage:
	@echo "Running tests with coverage..."
	@mkdir -p $(BUILD_DIR)
	go test -v -coverprofile=$(BUILD_DIR)/coverage.out -covermode=atomic ./...
	go tool cover -html=$(BUILD_DIR)/coverage.out -o $(BUILD_DIR)/coverage.html
	@echo "Coverage report generated: $(BUILD_DIR)/coverage.html"

# 代码格式化
fmt:
	go fmt ./...
	goimports -w .

# 代码检查
lint:
	golangci-lint run ./...

# 静态分析
vet:
	go vet ./...

# 清理构建产物
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR)
	go clean -cache -testcache -modcache

# 安装依赖工具
tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/tools/cmd/goimports@latest

# 安装到系统路径
install: build
	@echo "Installing binaries to /usr/local/bin..."
	@sudo cp $(BUILD_DIR)/$(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
	@sudo cp $(BUILD_DIR)/$(CLI_NAME) /usr/local/bin/$(CLI_NAME)
	@echo "Installation complete"

# 跨平台构建
build-all: build-linux build-darwin build-windows

build-linux:
	@echo "Building for Linux..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 $(CMD_DIR)
	GOOS=linux GOARCH=amd64 go build $(CLI_LDFLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-linux-amd64 $(CLI_DIR)

build-darwin:
	@echo "Building for macOS..."
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 $(CMD_DIR)
	GOOS=darwin GOARCH=amd64 go build $(CLI_LDFLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-darwin-amd64 $(CLI_DIR)

build-windows:
	@echo "Building for Windows..."
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe $(CMD_DIR)
	GOOS=windows GOARCH=amd64 go build $(CLI_LDFLAGS) -o $(BUILD_DIR)/$(CLI_NAME)-windows-amd64.exe $(CLI_DIR)

# Docker构建
docker-build:
	@echo "Building Docker image..."
	docker build -t nuts:$(VERSION) .

# Docker运行
docker-run:
	@echo "Running Docker container..."
	docker run --rm nuts:$(VERSION)

# 打印帮助信息
help:
	@echo "NUTS Build System"
	@echo "=================="
	@echo ""
	@echo "Available targets:"
	@echo "  make all                    - Run tidy and build"
	@echo "  make build                  - Build all binaries (server, cli, component-example)"
	@echo "  make build-server           - Build server binary"
	@echo "  make build-cli              - Build CLI binary"
	@echo "  make build-component-example- Build component example binary"
	@echo "  make run                    - Build and run server"
	@echo "  make run-server             - Build and run server"
	@echo "  make run-cli                - Build and run CLI"
	@echo "  make run-component-example  - Build and run component example"
	@echo "  make dev                    - Run in development mode (go run)"
	@echo "  make test                   - Run all tests"
	@echo "  make test-race              - Run tests with race detector"
	@echo "  make test-coverage          - Run tests with coverage report"
	@echo "  make fmt                    - Format Go code"
	@echo "  make lint                   - Run linter"
	@echo "  make vet                    - Run go vet"
	@echo "  make tidy                   - Download and tidy dependencies"
	@echo "  make clean                  - Clean build artifacts"
	@echo "  make tools                  - Install development tools"
	@echo "  make install                - Install binaries to /usr/local/bin"
	@echo "  make build-all              - Build for all platforms (linux, darwin, windows)"
	@echo "  make build-linux            - Build for Linux"
	@echo "  make build-darwin           - Build for macOS"
	@echo "  make build-windows          - Build for Windows"
	@echo "  make docker-build           - Build Docker image"
	@echo "  make docker-run             - Run Docker container"
	@echo "  make bench                  - Run all benchmark tests"
	@echo "  make bench-cpu              - Run CPU profiling"
	@echo "  make bench-mem              - Run memory profiling"
	@echo "  make proto                  - Generate gRPC code from api/eventbus.proto"
	@echo "  make help                   - Show this help message"
	@echo ""
	@echo "Build variables:"
	@echo "  VERSION=$(VERSION)"
	@echo "  BUILD_TIME=$(BUILD_TIME)"
	@echo "  GIT_COMMIT=$(GIT_COMMIT)"
	@echo "  GIT_BRANCH=$(GIT_BRANCH)"

# 性能测试
bench:
	go test -bench=. -benchmem ./...

# CPU 性能分析
bench-cpu:
	go test -bench=. -cpuprofile=cpu.prof ./...
	go tool pprof cpu.prof

# 内存性能分析
bench-mem:
	go test -bench=. -memprofile=mem.prof ./...
	go tool pprof mem.prof

# 从 proto 生成 gRPC 代码
proto:
	@echo "Generating gRPC code from api/eventbus.proto..."
	protoc --proto_path=. --go_out=. --go_opt=paths=source_relative --go-grpc_out=. --go-grpc_opt=paths=source_relative api/eventbus.proto
	@echo "Generating protobuf code from api/event.proto..."
	protoc --proto_path=. --proto_path=/tmp/protobuf-include --go_out=. --go_opt=paths=source_relative api/event.proto
	@echo "gRPC code generated successfully"
