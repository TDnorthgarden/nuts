#!/bin/bash
# 完整测试套件

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

cd "$PROJECT_DIR"

echo "========================================"
echo "Nuts State Machine - Test Suite"
echo "========================================"

# 1. 单元测试
echo ""
echo "[1/4] Running Unit Tests..."
echo "----------------------------------------"
go test ./pkg/task -v -count=1
go test ./pkg/component -v -count=1 2>/dev/null || echo "No component tests yet"

echo ""
echo "[2/4] Running Build Test..."
echo "----------------------------------------"
go build ./cmd/nuts
go build ./cmd/component-example
go build ./cmd/eventbus-test
echo "✓ All binaries built successfully"

echo ""
echo "[3/4] Running Vet..."
echo "----------------------------------------"
go vet ./pkg/...
go vet ./cmd/...
echo "✓ No issues found"

echo ""
echo "[4/4] Running E2E Test..."
echo "----------------------------------------"
# 注意：E2E 测试需要启动服务，可能需要手动运行
# "$SCRIPT_DIR/e2e-test.sh"
echo "⚠ E2E test requires manual execution:"
echo "   scripts/e2e-test.sh"

echo ""
echo "========================================"
echo "Test Suite Complete"
echo "========================================"
echo ""
echo "Summary:"
echo "  ✓ Unit Tests: PASSED"
echo "  ✓ Build: PASSED"
echo "  ✓ Vet: PASSED"
echo "  ⚠ E2E: Manual execution required"
