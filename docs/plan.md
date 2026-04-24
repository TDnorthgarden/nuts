# NUTS 通用框架代码开发计划

## 概述

本文档基于 `framework.md` 设计文档，制定通用框架的代码开发计划。遵循**接口先行、分层开发、工厂模式、配置驱动**的原则。

---

## 开发阶段

### 第一阶段：基础层 (Week 1)

**目标**：建立所有模块共享的基础数据结构和服务

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/common/` | `event.go` | `Event` 统一事件结构 | P0 |
| `pkg/common/` | `id.go` | `IDGenerator` 接口 + Snowflake实现 | P0 |
| `pkg/errors/` | `interface.go` | `Error` 统一错误接口 | P0 |
| `pkg/errors/` | `errors.go` | 错误实现 + 构造函数 | P0 |
| `pkg/logger/` | `interface.go` | `Logger` 接口定义 | P1 |
| `pkg/logger/` | `zap.go` | Zap 实现 | P1 |

**关键设计点**：
- `Event` 结构作为所有模块间通信的统一格式
- `IDGenerator` 使用 Snowflake 算法，支持分布式ID生成
- `Error` 接口包含错误码、类型、详情，便于错误处理

---

### 第二阶段：核心通信层 (Week 2)

**目标**：实现数据源和事件总线，建立模块间通信机制

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/datasource/` | `interface.go` | `DataSource` 接口 | P0 |
| `pkg/datasource/` | `factory.go` | `DataSourceFactory` 工厂 | P0 |
| `pkg/datasource/` | `manager.go` | `DataSourceManager` 管理器 | P0 |
| `pkg/datasource/` | `config.go` | `DataSourceConfig` 配置 | P0 |
| `pkg/eventbus/` | `interface.go` | `EventBus` 接口 | P0 |
| `pkg/eventbus/` | `factory.go` | `EventBusFactory` 工厂 | P0 |
| `pkg/eventbus/` | `local.go` | 内存实现（单机/测试） | P0 |
| `pkg/eventbus/` | `grpc/interface.go` | gRPC 接口定义 | P1 |
| `pkg/eventbus/` | `grpc/server.go` | gRPC 服务端 | P1 |
| `pkg/eventbus/` | `grpc/client.go` | gRPC 客户端 | P1 |

**关键设计点**：
- 数据源和策略引擎在同一进程内，通过 Channel 直接通信
- 跨进程模块通过 EventBus 通信，支持 gRPC/Redis/Kafka
- EventBus 由 TaskScheduler 管理作为服务端

---

### 第三阶段：策略引擎层 (Week 3)

**目标**：实现策略管理和 DSL 引擎抽象

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/policy/` | `interface.go` | `Policy`, `PolicyEngine`, `PolicyReceiver`, `PolicyMatcher` | P0 |
| `pkg/policy/` | `engine.go` | `PolicyEngineImpl` 实现 | P0 |
| `pkg/policy/` | `config.go` | `PolicyEngineConfig` 配置 | P0 |
| `pkg/dsl/` | `interface.go` | `DSLEngine` 接口 | P0 |
| `pkg/dsl/` | `factory.go` | `DSLEngineFactory` 工厂 | P0 |
| `pkg/dsl/` | `libdslgo/engine.go` | libdslgo 适配实现 | P0 |
| `pkg/dsl/` | `result.go` | `EvaluationResult` 评估结果 | P0 |

**关键设计点**：
- `Policy` 结构通用化，使用 `TaskConfig` 传递配置
- 策略引擎只负责 DSL 解析和匹配，不负责业务通知
- 匹配成功后通过 EventBus 发布 `PolicyMatched` 事件
- DSL 引擎通过工厂模式支持 libdslgo/CEL/Rego

---

### 第四阶段：任务调度层 (Week 4)

**目标**：实现任务调度器和状态机

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/scheduler/` | `interface.go` | `TaskScheduler`, `Task`, `TaskExecutor` | P0 |
| `pkg/scheduler/` | `scheduler.go` | `TaskSchedulerImpl` 实现 | P0 |
| `pkg/scheduler/` | `store.go` | `TaskStore` 接口 | P0 |
| `pkg/scheduler/` | `store_memory.go` | 内存存储实现 | P0 |
| `pkg/scheduler/` | `config.go` | `SchedulerConfig` 配置 | P0 |
| `pkg/scheduler/` | `filter.go` | `TaskFilter` 查询过滤器 | P1 |
| `pkg/statemachine/` | `interface.go` | `StateMachine`, `State`, `StateHandler` | P0 |
| `pkg/statemachine/` | `statemachine.go` | `StateMachineImpl` 实现 | P0 |
| `pkg/statemachine/` | `factory.go` | `StateMachineFactory` 工厂 | P0 |
| `pkg/statemachine/` | `context.go` | `StateContext` 状态上下文 | P0 |
| `pkg/statemachine/` | `config.go` | `StateMachineConfig` 配置 | P0 |

**关键设计点**：
- TaskScheduler 包含 StateMachine、EventBus、IDGenerator
- 状态机通过配置文件定义状态和转换规则
- StateHandler 负责状态的进入/退出/执行逻辑
- 状态转换通过 EventBus 事件驱动

---

### 第五阶段：数据库抽象层 (Week 5)

**目标**：实现数据库抽象，支持多种存储后端

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/storage/` | `interface.go` | `DB`, `Tx`, `Result`, `Row`, `Rows` | P0 |
| `pkg/storage/` | `factory.go` | `DBFactory` 工厂 | P0 |
| `pkg/storage/sqlite/` | `sqlite.go` | SQLite 实现 | P0 |
| `pkg/storage/event/` | `interface.go` | `TimeSeriesDB` 时序数据库接口 | P1 |
| `pkg/storage/kv/` | `interface.go` | `KV` 键值存储接口 | P1 |
| `pkg/storage/kv/leveldb/` | `leveldb.go` | LevelDB 实现 | P2 |

**关键设计点**：
- 关系型数据库抽象支持事务操作
- 时序数据库用于存储事件数据
- KV 存储用于轻量级数据缓存
- 通过工厂模式支持 SQLite/MySQL/PostgreSQL/InfluxDB

---

### 第六阶段：配置管理层 (Week 6)

**目标**：实现统一的配置管理系统

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/config/` | `interface.go` | `Config`, `ConfigParser` | P0 |
| `pkg/config/` | `registry.go` | `ConfigRegistry` 解析器注册表 | P0 |
| `pkg/config/` | `file.go` | `FileConfig` YAML配置加载 | P0 |
| `pkg/config/` | `viper.go` | Viper 集成（热加载） | P2 |

**各模块配置解析器**：

| 包路径 | 文件 | 配置解析器 | 优先级 |
|--------|------|-----------|--------|
| `pkg/datasource/` | `config.go` | `DataSourceConfigParser` | P0 |
| `pkg/policy/` | `config.go` | `PolicyConfigParser` | P0 |
| `pkg/scheduler/` | `config.go` | `SchedulerConfigParser` | P0 |
| `pkg/statemachine/` | `config.go` | `StateMachineConfigParser` | P0 |
| `pkg/eventbus/` | `config.go` | `EventBusConfigParser` | P0 |

**关键设计点**：
- 各模块注册自己的配置解析器
- 配置文件采用分层结构（global + 模块配置）
- 支持运行时重新加载配置

---

### 第七阶段：支撑层 (Week 7)

**目标**：实现监控、追踪、安全等支撑功能

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/metrics/` | `interface.go` | `Metrics`, `Counter`, `Gauge`, `Histogram` | P1 |
| `pkg/metrics/` | `prometheus.go` | Prometheus 实现 | P1 |
| `pkg/tracer/` | `interface.go` | `Tracer`, `Span` | P2 |
| `pkg/tracer/` | `otel.go` | OpenTelemetry 实现 | P2 |
| `pkg/auth/` | `interface.go` | `Authenticator`, `Credentials`, `Token` | P2 |
| `pkg/authz/` | `interface.go` | `Authorizer`, `Policy` | P2 |
| `pkg/crypto/` | `interface.go` | `Encryptor`, `Hasher` | P2 |

---

### 第八阶段：分布式组件 (Week 8)

**目标**：实现分布式环境下的支撑组件

| 包路径 | 文件 | 接口/功能 | 优先级 |
|--------|------|-----------|--------|
| `pkg/ratelimit/` | `interface.go` | `RateLimiter`, `Rate` | P2 |
| `pkg/circuitbreaker/` | `interface.go` | `CircuitBreaker`, `State` | P2 |
| `pkg/discovery/` | `interface.go` | `ServiceDiscovery`, `Service` | P2 |
| `pkg/cache/` | `interface.go` | `Cache` | P2 |

---

## 集成测试阶段

### Service 主程序集成

| 文件 | 内容 | 优先级 |
|------|------|--------|
| `cmd/service/main.go` | 初始化流程，模块组装 | P0 |
| `internal/service/service.go` | Service 核心实现 | P0 |
| `internal/api/handler.go` | HTTP API 处理器 | P0 |

**初始化顺序**：
1. 加载配置文件
2. 初始化 Logger
3. 初始化 EventBus（作为服务端）
4. 初始化 Storage（数据库）
5. 初始化 DataSourceManager
6. 初始化 PolicyEngine（订阅 DataSource 事件）
7. 初始化 TaskScheduler（订阅 PolicyMatched 事件）
8. 启动 HTTP API 服务

---

## 目录结构

```
nuts/
├── pkg/
│   ├── common/              # 通用数据结构
│   │   ├── event.go
│   │   └── id.go
│   ├── errors/              # 错误处理
│   │   ├── interface.go
│   │   └── errors.go
│   ├── logger/              # 日志抽象
│   │   ├── interface.go
│   │   └── zap.go
│   ├── config/              # 配置管理
│   │   ├── interface.go
│   │   ├── registry.go
│   │   └── file.go
│   ├── datasource/           # 数据源抽象
│   │   ├── interface.go
│   │   ├── factory.go
│   │   ├── manager.go
│   │   └── config.go
│   ├── eventbus/             # 事件总线
│   │   ├── interface.go
│   │   ├── factory.go
│   │   ├── local.go
│   │   ├── config.go
│   │   └── grpc/
│   │       ├── interface.go
│   │       ├── server.go
│   │       └── client.go
│   ├── policy/               # 策略引擎
│   │   ├── interface.go
│   │   ├── engine.go
│   │   └── config.go
│   ├── dsl/                  # DSL引擎
│   │   ├── interface.go
│   │   ├── factory.go
│   │   ├── result.go
│   │   └── libdslgo/
│   │       └── engine.go
│   ├── scheduler/            # 任务调度
│   │   ├── interface.go
│   │   ├── scheduler.go
│   │   ├── store.go
│   │   ├── store_memory.go
│   │   ├── filter.go
│   │   └── config.go
│   ├── statemachine/         # 状态机
│   │   ├── interface.go
│   │   ├── statemachine.go
│   │   ├── factory.go
│   │   ├── context.go
│   │   └── config.go
│   ├── storage/              # 数据库抽象
│   │   ├── interface.go
│   │   ├── factory.go
│   │   ├── sqlite/
│   │   ├── event/
│   │   │   └── interface.go
│   │   └── kv/
│   │       ├── interface.go
│   │       └── leveldb/
│   ├── metrics/              # 指标
│   │   ├── interface.go
│   │   └── prometheus.go
│   ├── tracer/               # 链路追踪
│   │   ├── interface.go
│   │   └── otel.go
│   ├── auth/                 # 认证
│   │   └── interface.go
│   ├── authz/                # 授权
│   │   └── interface.go
│   ├── crypto/               # 加密
│   │   └── interface.go
│   ├── ratelimit/            # 限流
│   │   └── interface.go
│   ├── circuitbreaker/       # 熔断
│   │   └── interface.go
│   ├── discovery/            # 服务发现
│   │   └── interface.go
│   └── cache/                # 缓存
│       └── interface.go
├── internal/
│   ├── service/              # Service内部实现
│   │   └── service.go
│   └── api/                  # HTTP API
│       └── handler.go
├── cmd/
│   └── service/
│       └── main.go
└── docs/
    ├── framework.md          # 框架设计文档
    ├── plugin.md             # 插件实例文档
    ├── flow.md               # 数据流转文档
    └── plan.md               # 本开发计划
```

---

## 优先级说明

- **P0 (Critical)**：核心功能，必须实现，阻塞后续开发
- **P1 (High)**：重要功能，影响框架完整性
- **P2 (Medium)**：增强功能，可后续迭代

---

## 关键依赖关系

```
common (event, id)
    ↓
errors, logger
    ↓
eventbus, storage
    ↓
datasource → policy → scheduler → statemachine
    ↑              ↑         ↑
    └──────────────┴─────────┘
              dsl
    ↑              ↑         ↑
    └──────────────┴─────────┘
            config (各模块注册解析器)
```

---

## 验收标准

### 接口定义完成标准
- 所有接口定义完整，包含方法签名和文档注释
- 接口之间依赖关系清晰
- 通过 `go build` 编译检查

### 实现完成标准
- 接口实现通过单元测试（覆盖率 > 80%）
- 工厂模式注册机制可用
- 配置文件解析正确
- 模块间集成测试通过

### 框架层完成标准
- Service 主程序能正常启动
- DataSource → PolicyEngine → TaskScheduler 数据流完整
- EventBus 跨进程通信正常
- 状态机状态转换正确

---

## 后续工作

框架层完成后，进入 **plugin.md** 实例开发阶段：
1. NRI/Docker 数据源具体实现
2. Collector 采集器引擎
3. Aggregation 聚合引擎
4. Diagnostic 诊断引擎
5. 完整的故障分析工作流

---

**文档版本**: v1.0  
**创建日期**: 2026-04-24  
**基于文档**: framework.md v1.0
