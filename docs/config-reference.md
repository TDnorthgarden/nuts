# NUTS 配置文件参考手册

配置文件路径：`configs/nuts.toml`（TOML 格式）

---

## 目录

- [1. global — 全局设置](#1-global--全局设置)
- [2. id — ID 生成器](#2-id--id-生成器)
- [3. log — 日志](#3-log--日志)
- [4. server — HTTP 服务器](#4-server--http-服务器)
- [5. datasource — 数据源](#5-datasource--数据源)
- [6. policy — 策略引擎](#6-policy--策略引擎)
- [7. scheduler — 调度器](#7-scheduler--调度器)
- [8. statemachine — 状态机](#8-statemachine--状态机)
- [9. task — 任务管理](#9-task--任务管理)
- [10. eventbus — 事件总线](#10-eventbus--事件总线)
- [11. metrics — 指标与告警](#11-metrics--指标与告警)
- [12. shutdown — 优雅关闭](#12-shutdown--优雅关闭)
- [13. tracing — 分布式追踪](#13-tracing--分布式追踪)

---

## 1. global — 全局设置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `log_level` | string | `"info"` | 全局日志级别。可选值：`debug`、`info`、`warn`、`error`、`fatal` |

**逻辑实现**：`pkg/core/core.go:reconfigureLogger()` 读取 `global.log_level`，传给 `log.NewZapLoggerWithConfig()` 创建 Zap logger。若为空则使用 `"info"`。

---

## 2. id — ID 生成器

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `[id].type` | string | `"uuid"` | ID 生成器类型。可选值：`uuid`、`snowflake` |
| `[id.snowflake].machine_id` | int | `0` | 雪花算法机器 ID（0-1023），分布式环境下需保证每节点唯一。仅 `type = "snowflake"` 时生效 |

**逻辑实现**：`pkg/core/core.go:initIDGenerator()` 读取 `[id]` 配置 map，通过 `common.IDGeneratorFactory.Create()` 工厂方法创建对应生成器。UUID 生成器无需额外参数；Snowflake 生成器读取 `machine_id` 参数。创建后通过 `common.SetDefaultGenerator()` 设为全局默认，后续所有 `common.GenerateUUID()` 调用均使用该生成器。

---

## 3. log — 日志

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `output_path` | string | `""` | 日志文件路径。为空时输出到 stdout |
| `encoding` | string | `"console"` | 日志编码格式。可选值：`console`（人类可读）、`json`（机器解析） |
| `sampling_enabled` | bool | `true` | 是否启用日志采样 |
| `sampling_initial` | int | `3` | 采样窗口内前 N 条日志全量输出 |
| `sampling_thereafter` | int | `1000` | 之后每 N 条采样输出 1 条 |
| `sampling_tick_millis` | int | `1000` | 采样计数器重置周期（毫秒） |

**逻辑实现**：`pkg/core/core.go:reconfigureLogger()` 读取上述字段，构造 `log.ZapConfig` 结构体，调用 `log.NewZapLoggerWithConfig()` 创建 Zap logger 并设为全局默认。采样机制由 Zap 框架内置实现：每个 tick 周期内，前 `sampling_initial` 条全量输出，之后按 `1/sampling_thereafter` 比例采样。

---

## 4. server — HTTP 服务器

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `address` | string | `"tcp://0.0.0.0:8080"` | 监听地址，支持 `tcp://host:port` 或 `unix:///path/to/socket` 格式 |
| `event_rate_limit` | int | `10` | 数据源事件处理速率限制（每秒请求数） |
| `event_burst` | int | `10` | 速率限制突发容量 |

**逻辑实现**：
- `address`：`pkg/core/core.go:startHTTPServer()` 读取 `server.address`，通过 `common.NewListener()` 创建对应协议的 listener（TCP 或 Unix Socket）。
- `event_rate_limit` / `event_burst`：`pkg/core/core.go:initEventLimiter()` 读取后创建 `golang.org/x/time/rate.Limiter`，在 `processEvents()` 中通过 `eventLimiter.Wait()` 对数据源事件进行速率限制。

---

## 5. datasource — 数据源

### 5.1 通用配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `[datasource].type` | string | `"mock"` | 数据源类型。可选值：`mock`、`containerd`、`nri` |
| `[datasource].event_channel_buffer_size` | int | `1000` | 数据源事件通道缓冲区大小 |

**逻辑实现**：`pkg/core/core.go:initDataSources()` 读取 `datasource.event_channel_buffer_size` 创建带缓冲的 event channel，传给 `DataSourceManager`。`DataSourceManager.Init()` 根据 `datasource.type` 选择对应数据源工厂进行初始化。

### 5.2 mock — Mock 数据源

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `[datasource.mock].event_interval_ms` | int | `5000` | 模拟事件生成间隔（毫秒） |
| `[datasource.mock].event_types` | string[] | `["ContainerStart","ContainerStop","ContainerUpdate"]` | 模拟生成的事件类型列表 |

**逻辑实现**：`pkg/datasource/mock.go` 读取配置后，按 `event_interval_ms` 定时从 `event_types` 中随机选取事件类型生成模拟事件，注入 event channel。主要用于开发和测试。

### 5.3 nri — NRI 数据源

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `socket_path` | string | `"/var/run/nri/nri.sock"` | NRI 插件 socket 路径 |
| `plugin_name` | string | `"00-nuts"` | NRI 插件名称 |
| `plugin_index` | int | `0` | NRI 插件索引，决定插件调用顺序 |
| `events` | string[] | `["RunPodSandbox","StopPodSandbox","StartContainer","StopContainer","RemoveContainer"]` | 订阅的 NRI 事件类型 |
| `buffer_size` | int | `1000` | 事件缓冲区大小 |
| `stop_timeout` | int | `5` | Stop 等待超时（秒），超时后强制退出 |
| `health_check_interval` | duration | `30s` | 健康检查间隔 |
| `reconnect_interval` | duration | `5s` | 断连后重连间隔 |

**逻辑实现**：`pkg/datasource/nri_config.go:ParseNRIConfig()` 解析配置 map，`Validate()` 补全默认值。`pkg/datasource/nri.go` 通过 containerd NRI SDK 的 `stub.Stub` 注册为 NRI 插件，订阅指定事件。`Stop()` 使用 `stop_timeout` 控制优雅关闭超时。`HealthCheckInterval` 和 `ReconnectInterval` 在 Validate 中设置默认值（30s / 5s）。

### 5.4 containerd — Containerd 数据源

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `socket_path` | string | `"/run/containerd/containerd.sock"` | Containerd gRPC socket 路径 |
| `namespace` | string | `"k8s.io"` | Containerd namespace |
| `events` | string[] | `["TaskCreate","TaskStart","TaskExit","TaskDelete"]` | 订阅的 Containerd 事件类型 |
| `buffer_size` | int | `1000` | 事件缓冲区大小 |
| `stop_timeout` | int | `5` | Stop 等待超时（秒），超时后强制退出 |
| `reconnect_initial_backoff` | int | `1` | 重连初始退避时间（秒） |
| `reconnect_max_backoff` | int | `30` | 重连最大退避时间（秒） |
| `health_check_interval` | duration | `30s` | 健康检查间隔 |
| `reconnect_interval` | duration | `5s` | 断连后重连间隔 |

**逻辑实现**：`pkg/datasource/containerd_config.go:ParseContainerdConfig()` 解析配置 map，`Validate()` 补全默认值。`pkg/datasource/containerd.go` 通过 containerd Go SDK 的 `client.Subscribe()` 订阅事件。`runEventLoop()` 实现带指数退避的自动重连：初始退避 `reconnect_initial_backoff` 秒，每次翻倍，上限 `reconnect_max_backoff` 秒。`Stop()` 使用 `stop_timeout` 控制优雅关闭超时。

---

## 6. policy — 策略引擎

### 6.1 通用配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | — (必填) | DSL 引擎类型。可选值：`cel`（Google CEL 表达式） |
| `max_concurrent_evaluations` | int | `50` | 最大并发策略评估数，超出时阻塞等待 |
| `evaluation_timeout` | string | `"5s"` | 单次 CEL 表达式评估超时（Go duration 格式） |

**逻辑实现**：`pkg/policy/engine.go:Init()` 读取 `policy.type` 通过 `policy.Factory` 创建 DSL 引擎实例，读取 `policy.max_concurrent_evaluations` 设置并发信号量大小。`evaluation_timeout` 通过接口断言调用 `SetEvaluationTimeout()` 设置到 CEL 引擎。`pkg/policy/cel.go:EvaluateWithCtx()` 使用该超时作为表达式执行的 deadline。

### 6.2 policy.db — 策略存储

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `"memory"` | 存储类型。可选值：`memory`（内存）、`sqlite`（SQLite 持久化） |
| `path` | string | `"data/policy.db"` | SQLite 数据库文件路径，仅 `type = "sqlite"` 时生效 |

**逻辑实现**：`pkg/core/core.go:initPolicyEngine()` 读取 `policy.db.type` 和 `policy.db.path`，通过 `db.DefaultFactory.Create()` 创建数据库实例，再包装为 `policy.NewPolicyStore()`。若配置缺失或创建失败，回退到 `policy.NewMemoryPolicyStore()` 内存存储。

---

## 7. scheduler — 调度器

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `policy_matched_buffer_size` | int | `100` | 策略匹配事件（`policy.matched`）直接 channel 缓冲区大小 |

**逻辑实现**：`pkg/core/core.go:New()` 读取 `scheduler.policy_matched_buffer_size`，创建 `policyMatchedCh` channel。策略匹配成功的事件通过该 channel 直接传递给 `startTaskEventHandler()` 创建任务，不经过 EventBus。

---

## 8. statemachine — 状态机

### 8.1 全局配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `name` | string | `"task_lifecycle"` | 状态机名称 |
| `initial_state` | string | `"pending"` | 任务创建后的初始状态 |
| `payload_builder` | string | `"default"` | Payload 构建器类型 |
| `terminal_states` | string[] | `["completed","failed","abandoned"]` | 终态列表。进入终态后任务不再流转 |

**逻辑实现**：`pkg/task/sm_factory.go:LoadStateMachineConfig()` 从 `cfg.GetMap("statemachine")` 读取配置，逐字段解析构造 `StateMachineConfig` 结构体。`IsTerminalState()` 方法遍历 `terminal_states` 判断是否为终态。

### 8.2 statemachine.states — 状态定义

格式：`[statemachine.states.<state_name>]`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `description` | string | `""` | 状态描述 |
| `auto_retry` | bool | `false` | 超时后是否自动重试 |
| `max_retries` | int | `0` | 最大重试次数，仅 `auto_retry = true` 时生效 |
| `retry_to_state` | string | `""` | 重试目标状态。为空时回退到 `initial_state` |

**逻辑实现**：`LoadStateMachineConfig()` 解析 `states` map 为 `map[string]StateConfig`。超时检查器 `pkg/task/timeout_checker.go` 发现任务超时后回调 `pkg/core/core.go:handleTimeoutEvent()`，根据 `auto_retry` 和 `max_retries` 决定重试或归档。重试时通过 `handleTaskRetry()` 转换到 `retry_to_state`，递增 `RetryCount` 并计算指数退避延迟。

### 8.3 statemachine.transitions — 转换规则

格式：`[[statemachine.transitions]]`

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `from` | string | — (必填) | 源状态 |
| `to` | string | — (必填) | 目标状态 |
| `allowed` | bool | `true` | 是否允许此转换 |

**逻辑实现**：`LoadStateMachineConfig()` 解析 `transitions` 数组为 `[]TransitionConfig`。`IsTransitionAllowed(from, to)` 遍历转换列表校验是否允许。`task/state_machine_engine.go:HandleTransitionCommand()` 在执行状态转换前调用此方法校验合法性。

---

## 9. task — 任务管理

### 9.1 通用配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `max_concurrent` | int | `100` | 最大并发任务数，超出时新任务阻塞等待（反压至数据源） |
| `default_timeout` | string | `"30m"` | 默认任务超时（Go duration 格式），当 rule 未设置 timeout 时使用 |

**逻辑实现**：
- `max_concurrent`：`pkg/core/core.go:initTaskScheduler()` 创建带缓冲信号量 `taskSem`。`handlePolicyMatchedEvent()` 通过 `taskSem <- struct{}{}` 获取槽位，达到上限时阻塞。终态转换后通过 `<-taskSem` 释放。
- `default_timeout`：`computeEffectiveTimeout()` 取 `rule_timeout` 和 `default_timeout` 中较小值作为生效超时。若两者均无效则使用 30 分钟兜底。

### 9.2 task.retry — 重试配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `max_backoff_seconds` | int | `60` | 任务重试指数退避上限（秒） |

**逻辑实现**：`pkg/core/core.go:backoff()` 方法计算指数退避延迟：`2^retryCount` 秒，上限为 `max_backoff_seconds`。在 `handleTaskRetry()` 中，若 `StateUpdatedAt + backoff` 尚未到达则跳过当前 tick。

### 9.3 task.db — 任务存储

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `"memory"` | 存储类型。可选值：`memory`（内存）、`sqlite`（SQLite 持久化） |
| `path` | string | `"data/tasks.db"` | SQLite 数据库文件路径，仅 `type = "sqlite"` 时生效 |

**逻辑实现**：`pkg/core/core.go:initTaskScheduler()` 读取 `task.db.type` 和 `task.db.path`，通过 `db.DefaultFactory.Create()` 创建数据库实例，包装为 `task.NewTaskStore()`。

### 9.4 task.archive — 归档清理

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `retention_days` | int | `30` | 历史任务保留天数，`0` 表示不清理 |
| `cleanup_interval` | string | `"1h"` | 清理检查间隔（Go duration 格式） |

**逻辑实现**：`pkg/core/core.go:initArchiveCleaner()` 读取配置创建 `task.ArchiveCleaner`。`retention_days > 0` 时启用。`pkg/task/archive.go:cleanup()` 扫描所有归档任务（`ArchivedAt` 不为空），删除超过保留期的任务。

### 9.5 task.scheduler — 任务调度器

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `timeout_check_interval` | string | `"3s"` | 超时检查最大间隔（Go duration 格式）。实际间隔动态调整：有活跃任务时按最早超时时间唤醒 |
| `timeout_rebuild_interval` | int | `5` | 超时堆重建间隔（tick 数），每 N 次 check 后从数据库全量重建一次最小堆 |

**逻辑实现**：`pkg/core/core.go:initTaskScheduler()` 读取后创建 `task.TimeoutChecker`。`pkg/task/timeout_checker.go` 使用最小堆（`container/heap`）按 `TimeoutAt` 排序，每次 check 弹出已超时任务并触发回调。`timeout_rebuild_interval` 控制堆的重建频率，防止堆数据因并发更新而失真。`timeout_check_interval` 作为无活跃任务时的兜底唤醒间隔。

---

## 10. eventbus — 事件总线

### 10.1 通用配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | — | 事件总线类型。可选值：`grpc`（gRPC 远程通信）、`noop`（空操作，独立模式）。不配置时默认使用 `noop` |

**逻辑实现**：`pkg/core/core.go:initEventBus()` 读取 `eventbus.type`。若为空则创建 `eventbus.NewNoopEventBus()`（独立模式，不启动 gRPC 服务）。若为 `grpc` 则读取 `[eventbus.grpc]` 配置，通过 `eventbus.Factory.CreateWithMap()` 创建 gRPC EventBus 服务端。

### 10.2 eventbus.grpc — gRPC 配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `address` | string | `"tcp://localhost:50051"` | gRPC 监听/连接地址，支持 `tcp://host:port` 或 `unix:///path` 格式 |
| `keepalive_max_idle` | string | `"5m"` | 连接最大空闲时间，超时后关闭空闲连接 |
| `keepalive_time` | string | `"2h"` | 客户端 keepalive ping 间隔 |
| `keepalive_timeout` | string | `"20s"` | keepalive ping 响应超时 |
| `publish_timeout` | string | `"5s"` | gRPC Publish 调用超时 |
| `subscriber_buffer_size` | int | `100` | 订阅者 channel 缓冲区大小 |
| `subscriber_send_timeout` | string | `"100ms"` | 向订阅者发送事件的超时，超时后丢弃事件并计数 |

**逻辑实现**：
- **服务端**：`pkg/eventbus/grpc.go:NewGRPCEventBusServerWithConfig()` 读取配置，通过 `parseDurationOrDefault()` 解析 duration 字段（失败时使用默认值）。keepalive 参数传给 `grpc.KeepaliveParams()`。`publish_timeout` 用于 `publishViaGRPC()` 的 context 超时。`subscriber_buffer_size` 用于创建订阅者 channel。`subscriber_send_timeout` 用于 `publishLocal()` 中的 `time.After()` 超时，超时后递增 `dropCount` 并记录告警日志。
- **客户端**：`pkg/eventbus/grpc.go:NewGRPCEventBusClient()` 创建客户端连接，默认使用与服务端相同的超时值。客户端模式下 `Subscribe()` 通过 gRPC 流订阅，`Publish()` 通过 gRPC 调用服务端发布。`subscribeViaGRPC()` 实现带指数退避（1s-60s）的自动重连。

---

## 11. metrics — 指标与告警

### 11.1 通用配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `type` | string | `"noop"` | 指标收集类型。可选值：`noop`（空实现）、`prometheus`（暴露 `/metrics` 端点） |
| `namespace` | string | `"nuts"` | Prometheus 指标前缀 |

**逻辑实现**：`pkg/core/core.go:initMetrics()` 读取 `metrics.type`，为 `prometheus` 时创建 `metrics.NewPrometheusMetrics(namespace)` 并注册 `/metrics` HTTP 端点。其他值使用 `common.NoopMetricsRecorder{}`。

### 11.2 metrics.alert — 告警配置

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用指标告警 |
| `timeout_rate_threshold` | float | `10.0` | 超时率阈值（次/秒），超过时输出 ERROR 级别告警日志 |
| `error_rate_threshold` | float | `20.0` | 错误率阈值（次/秒），超过时输出 ERROR 级别告警日志 |
| `window_size_seconds` | int | `60` | 滑动窗口大小（秒） |
| `check_interval_seconds` | int | `10` | 告警检查间隔（秒） |

**逻辑实现**：`pkg/core/core.go:initMetrics()` 读取告警配置，创建 `metrics.AlertConfig`，通过 `metrics.NewAlertMetrics()` 包装底层 MetricsRecorder。`pkg/metrics/alert.go:AlertMetrics` 使用滑动窗口（`slidingWindow`）统计超时/错误事件速率，每次 `TaskTimeout()` / `TaskError()` 回调时检查是否超过阈值，超过则输出告警日志。

---

## 12. shutdown — 优雅关闭

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `grace_period` | int | `15` | 优雅关闭总等待时间（秒），超时后强制退出 |
| `tracer_timeout` | int | `5` | Tracer 关闭超时（秒），用于刷新追踪数据缓冲区 |
| `http_timeout` | int | `5` | HTTP 服务器关闭超时（秒） |

**逻辑实现**：`pkg/core/core.go:Stop()` 按以下顺序关闭：
1. 取消 context，通知所有 goroutine
2. 停止 EventBus（关闭所有订阅 channel）
3. 停止 Tracer（`tracer_timeout` 秒内刷新缓冲区）
4. 停止 HTTP 服务器（`http_timeout` 秒内完成活跃请求）
5. 等待所有 goroutine 退出（`grace_period` 秒超时后强制继续）
6. 停止数据源、策略引擎、归档清理器

---

## 13. tracing — 分布式追踪

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `false` | 是否启用 OpenTelemetry 追踪 |
| `endpoint` | string | `"localhost:4317"` | OTLP gRPC collector 地址 |
| `service_name` | string | `"nuts"` | 服务名称，用于追踪数据标识 |
| `sample_rate` | float | `1.0` | 采样率（0.0 ~ 1.0），1.0 表示全量采样 |

**逻辑实现**：`pkg/core/core.go:initTracer()` 读取配置，调用 `trace.InitTracer()` 创建 OpenTelemetry `TracerProvider`。使用 OTLP gRPC exporter 连接 collector，采样策略为 `ParentBased(TraceIDRatioBased(sample_rate))`。创建后通过 `trace.GetTracer("nuts-core")` 获取 tracer 实例，用于在 `processEvents()` 等关键路径创建 span。HTTP 服务器通过 `otelhttp.NewHandler()` 自动注入追踪。
