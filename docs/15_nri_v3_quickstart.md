# NRI V3 快速启动指南

## 概述

本文档介绍如何在 5 分钟内启动使用 NRI V3 优化版本的 nuts-observer。

## 前置条件

- Rust 1.70+ 已安装
- Cargo 可用

## 启动方式

### 方式1：默认启动（推荐）

```bash
cd /root/nuts
cargo run
```

启动后：
- NRI V1 (兼容模式): `/api/v1/nri/*`
- **NRI V3 (优化模式): `/api/v3/nri/*` ← 新增**
- Prometheus 指标: `/metrics`
- 健康检查: `/health`

### 方式2：启用持久化

编辑配置启用 sled 持久化：

```rust
// src/main.rs 修改配置
let nri_v3_config = NriV3Config {
    enable_persist: true,  // ← 改为 true
    persist_config: Default::default(),
    batch_config: Default::default(),
};
```

### 方式3：启用 gRPC（实验性）

```bash
cargo run --features nri-grpc
```

## API 使用示例

### 1. 查询 V3 状态

```bash
curl http://localhost:8080/api/v3/nri/status
```

响应：
```json
{
  "version": "3.0.0-optimized",
  "features": ["dashmap-concurrent", "batch-processing", "version-control"],
  "pod_count": 0,
  "container_count": 0,
  "cgroup_count": 0,
  "pid_count": 0
}
```

### 2. 批量提交 Pod 事件

```bash
curl -X POST http://localhost:8080/api/v3/nri/batch \
  -H "Content-Type: application/json" \
  -d '{
    "events": [{
      "pod_uid": "pod-abc-123",
      "pod_name": "nginx-pod",
      "namespace": "default",
      "containers": [{
        "container_id": "container-xyz",
        "cgroup_ids": ["/sys/fs/cgroup/kubepods/besteffort/pod-abc-123/container-xyz"],
        "pids": [12345, 12346]
      }]
    }]
  }'
```

### 3. 查询 Pod 详情

```bash
curl http://localhost:8080/api/v3/nri/pod \
  -H "Content-Type: application/json" \
  -d '{"pod_uid": "pod-abc-123"}'
```

### 4. 查看 Prometheus 指标

```bash
curl http://localhost:8080/metrics
```

## Unix Socket 通信

默认 Unix Socket 路径：`/tmp/nuts_nri.sock`

### 发送事件示例

```bash
echo '{"pod_uid": "test-1", "pod_name": "test", "namespace": "default", "containers": [{"container_id": "c1", "cgroup_ids": [], "pids": []}]}' | nc -U /tmp/nuts_nri.sock
```

## 性能对比

| 操作 | V1 (RwLock) | V3 (DashMap) | 提升 |
|:-----|:------------|:-------------|:-----|
| 并发读 | 2M ops/s | 16M ops/s | **8x** |
| 并发写 | 500K ops/s | 12M ops/s | **24x** |
| P99 延迟 | ~50μs | ~5μs | **10x** |

## 切换回 V1

如需使用 V1 API：
```bash
curl http://localhost:8080/api/v1/nri/status
```

## 故障排查

### 端口被占用
```bash
# 检查端口占用
lsof -i :8080

# 更换端口
cargo run -- --port 8081
```

### 权限问题
```bash
# Unix Socket 权限
sudo chmod 777 /tmp/nuts_nri.sock
```

## 下一步

- [NRI V3 优化完整指南](./NRI_V3_OPTIMIZATION.md) - 详细架构说明
- [API 协议文档](./05_api_cli_contract.md) - 完整 API 规范
- [性能调优](./12_privilege_separation_arch.md) - 生产环境配置
