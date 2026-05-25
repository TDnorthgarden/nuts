#!/bin/bash
# E2E 测试脚本

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

echo "=== Starting E2E Test ==="
echo "Project directory: $PROJECT_DIR"

# 构建二进制文件
echo "Building binaries..."
go build -o /tmp/nuts ./cmd/nuts
go build -o /tmp/component-example ./cmd/component-example
go build -o /tmp/eventbus-test ./cmd/eventbus-test

# 启动 nuts 服务
echo "Starting nuts server..."
/tmp/nuts &
NUTS_PID=$!
echo "NUTS PID: $NUTS_PID"

# 等待服务启动
sleep 5

# 检查服务是否启动
if ! curl -s http://localhost:8080/api/v1/status > /dev/null 2>&1; then
    echo "ERROR: Nuts server failed to start"
    kill $NUTS_PID 2>/dev/null || true
    exit 1
fi
echo "Nuts server is running"

# 启动示例组件
echo "Starting component example..."
/tmp/component-example &
COMPONENT_PID=$!
echo "Component PID: $COMPONENT_PID"

sleep 3

# 发送测试事件
echo "Sending test events..."
# 这里可以添加发送测试事件的逻辑
# /tmp/eventbus-test

echo "Waiting for processing..."
sleep 5

# 检查结果
echo "Checking results..."
echo "Tasks:"
curl -s http://localhost:8080/api/v1/tasks | head -100

echo ""
echo "StateMachine Config:"
curl -s http://localhost:8080/api/v1/statemachine/config | head -50

# 清理
echo ""
echo "Cleaning up..."
kill $COMPONENT_PID 2>/dev/null || true
kill $NUTS_PID 2>/dev/null || true

# 清理临时文件
rm -f /tmp/nuts /tmp/component-example /tmp/eventbus-test

echo ""
echo "=== E2E Test Complete ==="
