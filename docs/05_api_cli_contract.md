# API / CLI 合同（触发诊断与获取结果）

> 目标：把“手动触发端到端跑通”所需的接口/字段一次性定清，避免后续采集与输出联调返工。

> **更新**: 新增 NRI V3 高性能 API 端点（参见第 4 节）

## 1. 诊断触发（Trigger）

### 1.1 API（规划）
`POST /v1/diagnostics:trigger`

请求体（建议）：
- `trigger_type`: `"manual"`（或 `"condition"`/`"event"` 在后续扩展）
- `target`: object
  - `pod_uid`: string（可选）
  - `namespace`: string（可选）
  - `pod_name`: string（可选）
  - `cgroup_id`: string（可选）
  - `node`: string（可选）
  - `all`: boolean（可选：是否全局）
  - `network_target`: object（可选：用于 network evidence 的探测目标；默认 `protocol=tcp`）
    - `target_id`: string（可选）
    - `dst_ip`: string（可选）
    - `dst_port`: number（可选）
    - `protocol`: string（可选；默认 `tcp`）
    - `endpoint`: string（可选）
- `time_window`: object
  - `start_time_ms`: number
  - `end_time_ms`: number
- `collection_options`: object（可选）
  - `sample_rate`: number（可选）
  - `max_traces`: number（可选）

## 4. NRI V3 高性能 API（新增）

NRI V3 提供基于 DashMap 的高性能 API 端点，支持并发读写和批量事件处理。

### 4.1 端点列表

| 方法 | 路径 | 功能 | 性能特性 |
|:-----|:-----|:-----|:---------|
| GET | `/api/v3/nri/status` | 获取 V3 状态统计 | O(1) 原子计数 |
| GET | `/api/v3/nri/pod` | 查询 Pod 详情 | DashMap 无锁查询 |
| POST | `/api/v3/nri/batch` | 批量提交事件 | 多线程批量处理 |

### 4.2 请求/响应示例

**GET /api/v3/nri/status**

响应：
```json
{
  "version": "3.0.0-optimized",
  "features": ["dashmap-concurrent", "batch-processing", "version-control"],
  "pod_count": 150,
  "container_count": 420,
  "cgroup_count": 380,
  "pid_count": 1250
}
```

**POST /api/v3/nri/batch**

请求：
```json
{
  "events": [{
    "pod_uid": "pod-abc-123",
    "pod_name": "nginx-pod",
    "namespace": "default",
    "containers": [{
      "container_id": "container-xyz",
      "cgroup_ids": ["/sys/fs/cgroup/..."],
      "pids": [12345, 12346]
    }]
  }]
}
```

响应：
```json
{
  "submitted": 1,
  "failed": 0,
  "errors": []
}
```

### 4.3 V1 vs V3 对比

| 特性 | V1 | V3 | 适用场景 |
|:-----|:---|:---|:---------|
| 并发性能 | RwLock 串行 | DashMap 并行 | 高并发场景 |
| 批量处理 | 单事件 | 多线程批量 | 高频事件 |
| 版本控制 | 无 | CAS 原子操作 | 防竞争 |
| 持久化 | 内存 | sled 可选 | 生产环境 |
| API 路径 | `/api/v1/*` | `/api/v3/*` | 逐步迁移 |
  - `requested_evidence_types`: string[]（可选：用户指定需要采集的 evidence 类型，如 `network`/`block_io`）
  - `requested_metrics_by_type`: object（可选：按 evidence_type 指定需要采集的 metric 列表）
    - key: `evidence_type`，value: string[]（例如 `{"network":["loss_rate","latency_p99_ms"],"block_io":["io_latency_p99_ms"]}`）
  - `requested_events_by_type`: object（可选：按 evidence_type 指定需要采集的事件类型列表）
    - key: `evidence_type`，value: string[]
- `idempotency_key`: string（用于幂等，建议必填）

响应体（建议）：
- `task_id`: string
- `status`: string（例如：`"queued"|"running"|"failed"|"done"`)
- `accepted_at_ms`: number

### 1.2 CLI（规划）
- `nutsctl diagnostics trigger --manual ...`

CLI 入参建议与 API 字段保持一致（target/time_window/idempotency_key）。

## 2. 条件触发与异常事件联动（扩展位）
> 先保留接口设计位，等第 2~3 周再落地规则引擎与事件源。
- 条件触发：`rule_id` 或 `condition_expression`
- 异常事件：`event_type`（如 `OOM`）+ 事件归属 scope（pod/cgroup）

## 3. 结果获取（Result Query）
`GET /v1/diagnostics/{task_id}`

建议返回：
- `task_id`
- `status`
- `diagnosis_result`: object（遵循 `docs/02_schemas.md` 的 Diagnosis Schema v0.2）
- `evidence_refs`: array（可选）

## 4. 告警平台推送 payload（规划接口）
为对接告警平台，建议在 Result Publisher 中实现：
- 输入：Diagnosis Result（schema）
- 输出：payload（payload 字段映射在此规划）

字段映射建议：
- payload 中至少包含：
  - `payload_version`: string（例如 `"alert_payload.v0.1"`）
  - `task_id`
  - `trigger_time_ms`
  - `trigger`（建议透传 `Diagnosis.trigger` 的关键字段）
  - `status`（建议透传 `Diagnosis.status`）
  - `conclusions`（摘要；建议包含 `conclusion_id/title/confidence/evidence_strength`）
  - `top_evidence_types`（证据类型列表，来自本次 Evidence 集合）
  - `traceability`（至少给出证据引用 ID，建议形如 `{evidence_ids: string[], conclusion_ids: string[]}`）
  - `dedup_key`（建议：用于告警去重，例如由 `task_id + trigger_time_ms + top_conclusion_ids` 计算）

告警平台的具体格式以你们的对接文档为准；第 1 周可以用 mock payload 验证链路打通。

## 4.1 AI Adapter 合同（规划接口）
> 目的：把证据链结构化地提供给 AI，并定义 AI 失败/超时的降级策略，避免影响核心诊断结果输出。

### 4.1.1 AI 输入（建议）
AI 入参建议由 `Diagnosis Result` 和 `Evidence` 的最小摘要构成：
- `task_id`
- `trigger`（manual/condition/event + trigger_time_ms）
- `conclusions`（conclusion_id + title + confidence + evidence_strength）
- `evidence_refs`（至少包含 `evidence_id`、`evidence_type`、`scope_key`）
- `evidence_summaries`（可选；对每个 evidence_type 提供用户选择的 metric_summary/events_topology 摘要）
- `traceability`（给出结论与证据的关联，便于 AI 回答可追溯）

### 4.1.2 AI 输出（建议）
AI 输出建议回填到 `Diagnosis.ai` 字段：
- `ai.enabled`
- `ai.status`: `ok|timeout|unavailable|failed`
- `ai.summary`: string（解释与建议摘要）

### 4.1.3 降级策略（硬要求）
- 当 AI 不可用或超时时，系统必须仍输出可解析的 `Diagnosis Result`（至少 conclusions/recommendations 可用）。
- 告警 payload 以 `Diagnosis Result` 为准，AI 失败不应影响 payload 的结构可解析性。

## 5. 错误码（建议最小集合）
- `NRI_UNAVAILABLE`
- `MAPPING_MISSING`
- `BPFTRACE_SCRIPT_LOAD_FAILED`
- `COLLECTION_TIMEOUT`
- `OUTPUT_SCHEMA_BUILD_FAILED`
- `PUBLISH_ALERT_FAILED`（告警推送失败不应影响诊断结果落地）

## 6. 元数据查询（用于“用户选择采集哪些”）
> 目标：用户可以先查询插件支持的 evidence 类型与指标，然后在触发时只选择需要的内容，以节省采集资源。

### 6.1 查询支持的 evidence 类型
`GET /v1/metadata/evidence-types`

响应（建议）：
- `evidence_types`: string[]

建议在 `v0.2` 初版至少包含：
- `network`
- `block_io`
- `fs_stall`
- `syscall_latency`
- `cgroup_compete`
- `oom`

### 6.2 查询某 evidence_type 支持的 metrics / events
`GET /v1/metadata/evidence-types/{evidence_type}`

响应（建议）：
- `evidence_type`: string
- `supported_metrics`: string[]
- `supported_events`: string[]

建议响应示例：

1) `evidence_type=network`
```json
{
  "evidence_type": "network",
  "supported_metrics": [
    "connectivity_success_rate",
    "loss_rate",
    "latency_p50_ms",
    "latency_p90_ms",
    "latency_p99_ms",
    "latency_avg_ms",
    "jitter_ms"
  ],
  "supported_events": [
    "connectivity_failure_burst",
    "packet_loss_burst",
    "latency_spike"
  ]
}
```

2) `evidence_type=block_io`
```json
{
  "evidence_type": "block_io",
  "supported_metrics": [
    "io_latency_p50_ms",
    "io_latency_p90_ms",
    "io_latency_p99_ms",
    "throughput_bytes_per_s",
    "io_ops_per_s",
    "queue_depth",
    "timeout_count"
  ],
  "supported_events": [
    "io_latency_spike",
    "io_queue_depth_spike",
    "io_timeout",
    "throughput_drop"
  ]
}
```

3) `evidence_type=oom`
```json
{
  "evidence_type": "oom",
  "supported_metrics": [],
  "supported_events": [
    "oom_kill"
  ]
}
```

4) `evidence_type=fs_stall`
```json
{
  "evidence_type": "fs_stall",
  "supported_metrics": [
    "fs_stall_p50_ms",
    "fs_stall_p90_ms",
    "fs_stall_p99_ms",
    "fs_ops_per_s"
  ],
  "supported_events": [
    "fs_stall_spike"
  ]
}
```

5) `evidence_type=syscall_latency`
```json
{
  "evidence_type": "syscall_latency",
  "supported_metrics": [
    "syscall_latency_p50_ms",
    "syscall_latency_p90_ms",
    "syscall_latency_p99_ms",
    "syscall_ops_per_s"
  ],
  "supported_events": [
    "syscall_latency_spike"
  ]
}
```

6) `evidence_type=cgroup_compete`
```json
{
  "evidence_type": "cgroup_compete",
  "supported_metrics": [
    "cpu_throttle_ratio",
    "io_throttle_ratio",
    "memory_pressure_index",
    "contention_score"
  ],
  "supported_events": [
    "cgroup_throttle_burst"
  ]
}
```

