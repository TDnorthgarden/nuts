# 故障分析插件开发计划

## 项目概述

故障分析插件是一个基于Go语言开发的容器性能监控和诊断系统，通过containerd的NRI机制获取容器生命周期事件，使用BPF技术采集进程、文件、网络、IO等事件数据，并通过策略引擎、聚合引擎和诊断引擎进行性能瓶颈分析。

## 技术栈

- **开发语言**: Go（主要）、C/bpftrace（BPF部分）、Python（AI部分）
- **容器运行时**: containerd 1.6+
- **NRI版本**: containerd NRI v0.8.0
- **Web框架**: Gin（HTTP RESTful API）
- **RPC框架**: gRPC
- **BPF工具**: bpftrace、bcc
- **数据库**（支持多种，通过接口抽象）:
  - 策略和配置数据：SQLite、MySQL、PostgreSQL、LevelDB
  - 事件数据（时序）：InfluxDB、TimescaleDB、ClickHouse、LevelDB
  - 审计和诊断数据：SQLite、MySQL、PostgreSQL
- **定时任务**: robfig/cron
- **AI框架**: OpenAI或本地大模型（第三阶段）

## 系统架构

### 项目结构

项目分为三个二进制程序：

1. **cli** - 命令行工具
   - 功能：推送策略给service
   - 交互方式：HTTP/gRPC

2. **service** - 核心服务
   - 功能：NRI事件接收、策略引擎、聚合引擎、诊断引擎
   - 部署方式：Deployment（普通容器，可多副本）
   - 通过gRPC调用collector服务

3. **collector** - 独立采集器
   - 功能：基于bpftrace的数据采集
   - 采集类型：进程、文件、网络、IO、perf
   - 部署方式：DaemonSet（特权容器，每个节点运行）
   - 通过gRPC提供服务接口

### 模块划分

#### 1. DataSource（数据源模块）
- 通过containerd NRI接口获取容器生命周期事件
- 填充cgroup信息
- 推送事件给策略引擎

#### 2. PolicyEngine（策略引擎）
- PolicyReceiver：接收策略（CLI/Sidecar/AI Agent）
- PolicyManager：管理策略生命周期（存储、查询）
- PolicyMatcher：匹配pod/容器是否符合策略（DataSource调用）
- PolicyNotifier：通知采集器启动/停止，通知聚合引擎开启/关闭定时任务（PolicyMatcher调用）
- PolicyTaskManager：管理策略任务状态机（新增）

**逻辑流程**：
1. DataSource接收NRI事件（容器创建/启动/停止等）
2. DataSource调用PolicyEngine的PolicyMatcher进行匹配
3. 如果匹配成功：
   - PolicyMatcher调用PolicyTaskManager创建或更新任务
   - 根据任务状态，PolicyTaskManager调用PolicyNotifier
   - PolicyNotifier通过gRPC客户端调用Collector服务启动采集
   - PolicyNotifier通知AggregationEngine开启聚合任务（通过内部RPC）
4. 如果容器停止或策略过期：
   - PolicyTaskManager更新任务状态
   - PolicyNotifier通过gRPC客户端调用Collector服务停止采集
   - PolicyNotifier通知AggregationEngine停止/完成聚合任务

#### 3. Collector（采集器）
- 核心库设计：pkg/collector/作为可复用库
- 具体采集器：
  - 进程采集器（ProcessCollector）
  - 文件采集器（FileCollector）
  - 网络采集器（NetworkCollector）
  - IO采集器（IOCollector）
  - Perf采集器（PerfCollector）
- 脚本管理器（ScriptManager）
- gRPC服务端：提供采集控制接口
- 作为独立进程运行，通过gRPC接收service的采集请求

#### 4. AggregationEngine（聚合引擎）
- 任务调度器（TaskScheduler）
- 事件聚合器（EventAggregator）
- 聚合算法抽象（AggregationAlgorithm）
  - 简单去重聚合算法（SimpleAggregationAlgorithm）
  - 时间窗口聚合算法（TimeWindowAggregationAlgorithm）
  - 统计聚合算法（StatisticalAggregationAlgorithm）
  - 频率聚合算法（FrequencyAggregationAlgorithm）
  - 自定义聚合算法（CustomAggregationAlgorithm）
- 审计生成器（AuditGenerator）
- 诊断通知器（DiagnosticNotifier）

#### 5. DiagnosticEngine（诊断引擎）
- 审计分析器（AuditAnalyzer）
- 诊断策略抽象（DiagnosticStrategy）
  - 内置诊断策略（BuiltInDiagnosticStrategy）：基于规则引擎
  - AI诊断策略（AIDiagnosticStrategy）：基于AI模型
- 瓶颈检测器（BottleneckDetector）
- 报告生成器（ReportGenerator）
- AI Agent接口（AIAgentInterface，第三阶段）

**诊断流程**：
1. 接收聚合引擎的通知（审计数据）
2. 根据配置选择诊断策略：
   - AI未开启：使用内置诊断策略（规则引擎）
   - AI开启：使用AI诊断策略
3. 执行诊断分析，检测性能瓶颈
4. 生成分析报告

## 接口抽象设计

为了支持后续实现不同的引擎，需要对以下模块进行接口抽象：

### 1. 策略引擎接口

```go
// PolicyMatcher 策略匹配器接口
type PolicyMatcher interface {
    Match(event *Event) (*MatchResult, error)
}

// PolicyReceiver 策略接收器接口
type PolicyReceiver interface {
    Receive(policy *Policy) error
    Update(policy *Policy) error
    Delete(id string) error
    Get(id string) (*Policy, error)
    List() ([]*Policy, error)
}

// PolicyNotifier 策略通知器接口
type PolicyNotifier interface {
    NotifyCollectorStart(cgroupID string, policyID string, metrics []string) error
    NotifyCollectorStop(cgroupID string, policyID string) error
    NotifyAggregationStart(cgroupID string, policyID string, duration time.Duration) error
    NotifyAggregationStop(cgroupID string, policyID string) error
}

// PolicyTaskManager 策略任务管理器接口（状态机）
type PolicyTaskManager interface {
    CreateTask(policy *Policy, event *Event) (*Task, error)
    UpdateTaskState(taskID string, state TaskState, reason string) error
    GetTask(taskID string) (*Task, error)
    GetTasksByPolicy(policyID string) ([]*Task, error)
    GetTasksByCgroup(cgroupID string) ([]*Task, error)
    DeleteTask(taskID string) error
}

// TaskState 任务状态
type TaskState int

const (
    TaskStateIdle      TaskState = iota // 空闲：策略已创建，等待匹配
    TaskStatePending                    // 等待：匹配成功，等待启动
    TaskStateRunning                    // 运行中：采集中
    TaskStateCompleted                  // 已完成：采集时长到期
    TaskStateStopped                    // 已停止：pod/容器停止
    TaskStateFailed                     // 失败：采集器或聚合引擎失败
)

// Task 任务
type Task struct {
    ID              string        // 任务ID
    PolicyID        string        // 策略ID
    CgroupID        string        // cgroup ID
    State           TaskState     // 任务状态
    Metrics         []string      // 采集指标
    Duration        time.Duration // 采集时长
    StartTime       time.Time     // 开始时间
    EndTime         time.Time     // 结束时间
    CreatedAt       time.Time     // 创建时间
    UpdatedAt       time.Time     // 更新时间
    FailureReason   string        // 失败原因
}
```

### 2. 采集器接口

```go
// Collector 采集器统一接口
type Collector interface {
    Start(cgroupID string, policyID string, metrics []string) error
    Stop(cgroupID string, policyID string) error
    IsRunning(cgroupID string, policyID string) bool
}

// CollectorImpl 采集器实现（独立进程模式）
type CollectorImpl struct {
    processCollector  *ProcessCollector
    fileCollector     *FileCollector
    networkCollector  *NetworkCollector
    ioCollector       *IOCollector
    perfCollector     *PerfCollector
    scriptManager     *ScriptManager
}

// CollectorClient Collector gRPC客户端（service端使用）
type CollectorClient interface {
    StartCollection(req *StartCollectionRequest) error
    StopCollection(req *StopCollectionRequest) error
    IsCollecting(cgroupID string, policyID string) (bool, error)
}

// ScriptManager 脚本管理器接口
type ScriptManager interface {
    LoadScript(scriptType string, scriptPath string) error
    UnloadScript(scriptType string, scriptPath string) error
    ExecuteScript(scriptType string, args []string) ([]byte, error)
}
```

### 3. 聚合引擎接口

```go
// EventAggregator 事件聚合器接口
type EventAggregator interface {
    Aggregate(events []*Event) (*AggregatedEvent, error)
    SetAlgorithm(algorithm AggregationAlgorithm) error
}

// AggregationAlgorithm 聚合算法接口
type AggregationAlgorithm interface {
    Name() string
    Aggregate(events []*Event) (*AggregatedEvent, error)
    Validate(events []*Event) error
}

// 聚合算法实现
type SimpleAggregationAlgorithm struct {
    // 简单去重聚合
}

type TimeWindowAggregationAlgorithm struct {
    window time.Duration
    // 时间窗口聚合
}

type StatisticalAggregationAlgorithm struct {
    // 统计聚合（均值、中位数、分位数等）
}

type FrequencyAggregationAlgorithm struct {
    // 频率聚合
}

type CustomAggregationAlgorithm struct {
    // 自定义聚合算法
}

// TaskScheduler 任务调度器接口
type TaskScheduler interface {
    Schedule(task *Task) error
    Cancel(taskID string) error
}
```

### 4. 数据库接口

```go
// PolicyStore 策略存储接口
type PolicyStore interface {
    Create(policy *Policy) error
    Update(policy *Policy) error
    Delete(id string) error
    Get(id string) (*Policy, error)
    List() ([]*Policy, error)
    Query(query *PolicyQuery) ([]*Policy, error)
}

// EventStore 事件存储接口（时序数据）
type EventStore interface {
    Write(event *Event) error
    WriteBatch(events []*Event) error
    Query(query *EventQuery) ([]*Event, error)
    QueryByTimeRange(start, end time.Time, filters map[string]string) ([]*Event, error)
    Delete(cgroupID string, policyID string) error
}

// AuditStore 审计存储接口
type AuditStore interface {
    Create(audit *Audit) error
    Get(id string) (*Audit, error)
    ListByPolicy(policyID string) ([]*Audit, error)
    ListByCgroup(cgroupID string) ([]*Audit, error)
    Update(audit *Audit) error
}

// DiagnosisStore 诊断结果存储接口
type DiagnosisStore interface {
    Create(diagnosis *Diagnosis) error
    Get(id string) (*Diagnosis, error)
    ListByAudit(auditID string) ([]*Diagnosis, error)
    Update(diagnosis *Diagnosis) error
}

// 数据库实现
type SQLitePolicyStore struct {
    db *sql.DB
}

type MySQLPolicyStore struct {
    db *sql.DB
}

type LevelDBEventStore struct {
    db *leveldb.DB
}

type ClickHouseEventStore struct {
    client *clickhouse.Client
}

type InfluxDBEventStore struct {
    client *influxdb2.Client
}
```

### 5. 诊断引擎接口

```go
// DiagnosticEngine 诊断引擎接口
type DiagnosticEngine interface {
    Analyze(audit *Audit) (*DiagnosisResult, error)
    GenerateReport(diagnosis *DiagnosisResult) (*Report, error)
}

// DiagnosticStrategy 诊断策略接口
type DiagnosticStrategy interface {
    Name() string
    Analyze(audit *Audit) (*DiagnosisResult, error)
}

// BuiltInDiagnosticStrategy 内置诊断策略（规则引擎）
type BuiltInDiagnosticStrategy struct {
    // 基于预定义规则的诊断
}

// AIDiagnosticStrategy AI诊断策略
type AIDiagnosticStrategy struct {
    // 基于AI模型的诊断
    aiClient AIClient
}

// BottleneckDetector 瓶颈检测器接口
type BottleneckDetector interface {
    Detect(audit *Audit) ([]*Bottleneck, error)
}
```

## 开发阶段

### 第一阶段：CLI策略推送（预计2个月）

**目标**：实现基本的策略管理功能，支持CLI命令行工具推送策略给service

#### 任务分解

**Week 1-2: 项目初始化**
- [ ] 创建项目目录结构
- [ ] 初始化Go模块
- [ ] 配置依赖管理（go.mod）
- [ ] 设计数据库表结构
- [ ] 搭建开发环境

**Week 3-4: Service基础框架**
- [ ] 实现service主程序框架
- [ ] 集成Gin框架
- [ ] 实现HTTP RESTful API基础路由
- [ ] 实现策略数据模型
- [ ] 实现策略存储（PostgreSQL/MySQL）

**Week 5-6: 策略引擎实现**
- [ ] 实现PolicyReceiver接口
- [ ] 实现PolicyMatcher接口
- [ ] 实现PolicyManager
- [ ] 实现策略CRUD接口
- [ ] 实现策略验证逻辑

**Week 7-8: CLI工具开发**
- [ ] 实现cli主程序框架
- [ ] 实现命令行参数解析
- [ ] 实现策略推送功能
- [ ] 实现策略查询功能
- [ ] 实现策略删除功能

**Week 9-10: 测试和优化**
- [ ] 编写单元测试
- [ ] 编写集成测试
- [ ] 性能优化
- [ ] 文档编写

### 第二阶段：Sidecar进程（预计1.5个月）

**目标**：实现NRI事件接收、采集器和聚合引擎

#### 任务分解

**Week 11-12: NRI集成**
- [ ] 集成containerd NRI v0.8.0
- [ ] 实现DataSource模块
- [ ] 实现NRI事件监听
- [ ] 实现cgroup信息填充
- [ ] 实现事件推送给策略引擎

**Week 13-14: 采集器实现**
- [ ] 实现Collector接口
- [ ] 实现ScriptManager接口
- [ ] 实现ProcessCollector
- [ ] 实现FileCollector
- [ ] 实现NetworkCollector
- [ ] 实现IOCollector
- [ ] 实现bpftrace脚本模板

**Week 15-16: 聚合引擎实现**
- [ ] 实现EventAggregator接口
- [ ] 实现TaskScheduler接口
- [ ] 集成robfig/cron
- [ ] 实现审计生成器
- [ ] 实现诊断通知器
- [ ] 集成InfluxDB时序数据库

**Week 17-18: 集成测试**
- [ ] 端到端测试
- [ ] 性能测试
- [ ] 错误处理测试
- [ ] 文档更新

### 第三阶段：AI Agent集成（预计2个月）

**目标**：集成AI诊断引擎，实现智能故障分析

#### 任务分解

**Week 19-20: 诊断引擎基础**
- [ ] 实现DiagnosticEngine接口
- [ ] 实现AuditAnalyzer
- [ ] 实现BottleneckDetector接口
- [ ] 实现规则引擎
- [ ] 实现ReportGenerator

**Week 21-22: AI集成准备**
- [ ] 设计AI Agent接口
- [ ] 实现MCP协议支持
- [ ] 编写SKILL定义
- [ ] 准备训练数据

**Week 23-24: AI模型集成**
- [ ] 集成OpenAI API或本地大模型
- [ ] 实现AI诊断逻辑
- [ ] 实现降级策略（AI失败时使用规则引擎）
- [ ] 性能优化

**Week 25-26: 测试和优化**
- [ ] AI模型准确度测试
- [ ] 端到端测试
- [ ] 文档完善
- [ ] 部署准备

## 目录结构设计

```
nuts/
├── cmd/                          # 主程序入口
│   ├── cli/                      # CLI工具
│   │   └── main.go
│   ├── service/                  # Service主程序
│   │   └── main.go
│   └── collector/                # 独立Collector二进制
│       └── main.go
├── pkg/                          # 可复用库
│   ├── collector/                # Collector核心库
│   │   ├── interface.go          # Collector接口定义
│   │   ├── impl.go               # CollectorImpl实现
│   │   ├── process.go            # 进程采集器
│   │   ├── file.go               # 文件采集器
│   │   ├── network.go            # 网络采集器
│   │   ├── io.go                 # IO采集器
│   │   ├── perf.go               # Perf采集器
│   │   ├── script.go             # 脚本管理器
│   │   └── server.go             # gRPC服务端
│   ├── policy/                   # 策略引擎库
│   │   ├── interface.go          # 策略接口定义
│   │   ├── matcher.go            # PolicyMatcher实现
│   │   ├── receiver.go           # PolicyReceiver实现
│   │   ├── notifier.go           # PolicyNotifier实现
│   │   ├── manager.go            # PolicyManager实现
│   │   └── task/                 # 任务管理（状态机）
│   │       ├── interface.go      # TaskManager接口定义
│   │       ├── manager.go        # TaskManager实现
│   │       ├── state.go          # 状态机逻辑
│   │       └── store.go          # TaskStore实现
│   ├── aggregation/              # 聚合引擎库
│   │   ├── interface.go          # 聚合接口定义
│   │   ├── aggregator.go         # EventAggregator实现
│   │   ├── scheduler.go          # TaskScheduler实现
│   │   ├── audit.go               # 审计生成器
│   │   └── algorithm/            # 聚合算法实现
│   │       ├── interface.go      # 聚合算法接口定义
│   │       ├── simple.go         # 简单去重聚合算法
│   │       ├── timewindow.go     # 时间窗口聚合算法
│   │       ├── statistical.go    # 统计聚合算法
│   │       ├── frequency.go      # 频率聚合算法
│   │       └── custom.go         # 自定义聚合算法
│   ├── diagnostic/               # 诊断引擎库
│   │   ├── interface.go          # 诊断接口定义
│   │   ├── analyzer.go           # AuditAnalyzer实现
│   │   ├── detector.go           # BottleneckDetector实现
│   │   ├── report.go             # ReportGenerator实现
│   │   └── strategy/             # 诊断策略实现
│   │       ├── interface.go      # DiagnosticStrategy接口定义
│   │       ├── builtin.go        # 内置诊断策略（规则引擎）
│   │       └── ai.go             # AI诊断策略
│   ├── datasource/               # 数据源库
│   │   ├── nri.go                # NRI事件监听
│   │   └── event.go              # 事件处理
│   ├── storage/                  # 数据库抽象层
│   │   ├── interface.go          # 数据库接口定义
│   │   ├── policy/               # 策略存储实现
│   │   │   ├── sqlite.go         # SQLite实现
│   │   │   ├── mysql.go          # MySQL实现
│   │   │   ├── postgres.go       # PostgreSQL实现
│   │   │   └── leveldb.go        # LevelDB实现
│   │   ├── event/                # 事件存储实现
│   │   │   ├── influxdb.go       # InfluxDB实现
│   │   │   ├── clickhouse.go     # ClickHouse实现
│   │   │   ├── timescaledb.go    # TimescaleDB实现
│   │   │   └── leveldb.go        # LevelDB实现
│   │   ├── audit/                # 审计存储实现
│   │   │   ├── sqlite.go         # SQLite实现
│   │   │   ├── mysql.go          # MySQL实现
│   │   │   └── postgres.go       # PostgreSQL实现
│   │   └── diagnosis/            # 诊断存储实现
│   │       ├── sqlite.go         # SQLite实现
│   │       ├── mysql.go          # MySQL实现
│   │       └── postgres.go       # PostgreSQL实现
│   └── client/                   # 客户端库
│       └── collector.go          # Collector gRPC客户端
├── internal/                     # 内部实现
│   ├── service/                  # Service内部实现
│   │   ├── server.go             # HTTP服务器
│   │   ├── handler.go            # API处理器
│   │   └── config.go             # 配置管理
│   └── config/                   # 配置
│       └── config.go             # 配置结构
├── api/                          # API定义
│   ├── proto/                    # gRPC proto文件
│   │   ├── collector.proto       # Collector服务定义
│   │   └── policy.proto          # 策略服务定义
│   └── openapi/                  # OpenAPI文档
├── scripts/                      # BPF脚本
│   ├── process.bt                # 进程采集脚本
│   ├── file.bt                   # 文件采集脚本
│   ├── network.bt                # 网络采集脚本
│   ├── io.bt                     # IO采集脚本
│   └── perf.bt                   # Perf采集脚本
├── configs/                      # 配置文件
│   ├── service.yaml              # Service配置
│   └── collector.yaml            # Collector配置
├── deployments/                  # 部署文件
│   ├── service.yaml              # Service部署
│   └── collector.yaml            # Collector部署
├── docs/                         # 文档
│   ├── design.md                 # 设计文档
│   └── api.md                    # API文档
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

## 状态机设计

### 策略任务状态机

为了管理每个采集任务的生命周期，策略引擎需要为每个策略任务建立一个状态机。

### 状态定义

| 状态 | 说明 | 进入条件 | 退出条件 |
|-----|------|---------|---------|
| Idle | 空闲：策略已创建，等待匹配 | 策略创建 | 匹配成功（NRI事件） |
| Pending | 等待：匹配成功，等待启动 | NRI事件匹配成功 | 启动成功 |
| Running | 运行中：采集中 | 采集器启动成功 | 时长到期 或 容器停止 |
| Completed | 已完成：采集时长到期 | 时长到期 | - |
| Stopped | 已停止：pod/容器停止 | 容器停止事件 | - |
| Failed | 失败：采集器或聚合引擎失败 | 启动失败或运行异常 | - |

### 状态转换图

```
                    NRI事件匹配成功
    +--------+      (Sync/Pod启动/容器启动)
    |  Idle  | --------------------------> +---------+
    +--------+                             | Pending |
                                           +---------+
                                                |
                                                | 启动成功
                                                v
                                          +---------+
                                          | Running |
                                          +---------+
                                                |
                    +---------------------------+---------------------------+
                    |                           |                           |
                    | 时长到期                   | 容器停止                   | 启动失败/异常
                    v                           v                           v
              +-----------+               +---------+                 +---------+
              | Completed |               | Stopped |                 | Failed  |
              +-----------+               +---------+                 +---------+
```

### NRI事件与状态转换关系

| NRI事件 | 当前状态 | 目标状态 | 触发动作 |
|---------|---------|---------|---------|
| Sync/Pod启动/容器启动 | Idle | Pending | 创建任务，通知采集器启动 |
| Sync/Pod启动/容器启动 | Running | Running | 更新任务时间（如果需要） |
| Pod停止/容器停止 | Running | Stopped | 通知采集器停止，通知聚合引擎完成 |
| Pod停止/容器停止 | Pending | Stopped | 取消任务启动 |
| Sync/Pod启动/容器启动 | Stopped | Pending | 重新启动（可选，根据策略配置） |
| 时长到期 | Running | Completed | 通知采集器停止，通知聚合引擎完成 |
| 采集器启动失败 | Pending | Failed | 记录失败原因，重试或告警 |
| 聚合引擎启动失败 | Pending | Failed | 记录失败原因，重试或告警 |

### 状态机实现逻辑

```go
// PolicyTaskManagerImpl 策略任务管理器实现
type PolicyTaskManagerImpl struct {
    tasks      map[string]*Task           // 任务缓存
    taskStore  TaskStore                  // 任务持久化
    notifier   PolicyNotifier             // 策略通知器
    mutex      sync.RWMutex
}

// HandleNRIEvent 处理NRI事件
func (m *PolicyTaskManagerImpl) HandleNRIEvent(event *Event) error {
    m.mutex.Lock()
    defer m.mutex.Unlock()

    // 1. 匹配策略
    matchResult, err := m.matcher.Match(event)
    if err != nil {
        return err
    }

    // 2. 查找现有任务
    existingTask, err := m.taskStore.GetByCgroupAndPolicy(event.CgroupID, matchResult.PolicyID)
    if err != nil && err != ErrTaskNotFound {
        return err
    }

    // 3. 根据事件类型和当前状态进行状态转换
    switch event.Type {
    case NRISync, NRIPodStart, NRIContainerStart:
        if existingTask == nil {
            // 创建新任务
            return m.createNewTask(matchResult, event)
        } else {
            // 更新现有任务
            return m.updateExistingTask(existingTask, event)
        }
    case NRIPodStop, NRIContainerStop:
        if existingTask != nil && existingTask.State == TaskStateRunning {
            // 停止任务
            return m.stopTask(existingTask, event)
        }
    }

    return nil
}

// createNewTask 创建新任务
func (m *PolicyTaskManagerImpl) createNewTask(matchResult *MatchResult, event *Event) error {
    task := &Task{
        ID:        generateTaskID(),
        PolicyID:  matchResult.PolicyID,
        CgroupID:  event.CgroupID,
        State:     TaskStatePending,
        Metrics:   matchResult.Metrics,
        Duration:  matchResult.Duration,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    // 持久化任务
    if err := m.taskStore.Create(task); err != nil {
        return err
    }

    // 通知采集器启动
    if err := m.notifier.NotifyCollectorStart(task.CgroupID, task.PolicyID, task.Metrics); err != nil {
        m.updateTaskState(task.ID, TaskStateFailed, err.Error())
        return err
    }

    // 通知聚合引擎启动
    if err := m.notifier.NotifyAggregationStart(task.CgroupID, task.PolicyID, task.Duration); err != nil {
        m.updateTaskState(task.ID, TaskStateFailed, err.Error())
        return err
    }

    // 更新状态为Running
    return m.updateTaskState(task.ID, TaskStateRunning, "")
}

// stopTask 停止任务
func (m *PolicyTaskManagerImpl) stopTask(task *Task, event *Event) error {
    // 通知采集器停止
    if err := m.notifier.NotifyCollectorStop(task.CgroupID, task.PolicyID); err != nil {
        log.Printf("Failed to notify collector stop: %v", err)
    }

    // 通知聚合引擎停止
    if err := m.notifier.NotifyAggregationStop(task.CgroupID, task.PolicyID); err != nil {
        log.Printf("Failed to notify aggregation stop: %v", err)
    }

    // 更新状态为Stopped
    return m.updateTaskState(task.ID, TaskStateStopped, "Container stopped")
}

// checkTaskExpiration 检查任务过期（定时任务）
func (m *PolicyTaskManagerImpl) checkTaskExpiration() {
    m.mutex.Lock()
    defer m.mutex.Unlock()

    runningTasks, err := m.taskStore.GetByState(TaskStateRunning)
    if err != nil {
        log.Printf("Failed to get running tasks: %v", err)
        return
    }

    now := time.Now()
    for _, task := range runningTasks {
        if now.Sub(task.StartTime) >= task.Duration {
            // 任务时长到期
            m.stopTask(task, nil)
            m.updateTaskState(task.ID, TaskStateCompleted, "Duration expired")
        }
    }
}
```

### 任务持久化

```go
// TaskStore 任务存储接口
type TaskStore interface {
    Create(task *Task) error
    Update(task *Task) error
    Get(id string) (*Task, error)
    GetByCgroupAndPolicy(cgroupID, policyID string) (*Task, error)
    GetByState(state TaskState) ([]*Task, error)
    Delete(id string) error
}
```

### 时长到期检测

- 使用定时器定期检查运行中的任务是否到期
- 检查频率：每1分钟检查一次
- 到期条件：`当前时间 - 任务开始时间 >= 策略配置的采集时长`

### 容器重启处理

- 如果策略配置允许容器重启后重新采集，当检测到容器重启时，可以将状态从Stopped转换到Pending
- 默认情况下，容器停止后任务不再重启

## 诊断引擎实现逻辑

### 诊断策略选择

```go
// DiagnosticEngineImpl 诊断引擎实现
type DiagnosticEngineImpl struct {
    strategy     DiagnosticStrategy
    builtinStrategy *BuiltInDiagnosticStrategy
    aiStrategy      *AIDiagnosticStrategy
    config        *DiagnosticConfig
}

// NewDiagnosticEngine 创建诊断引擎
func NewDiagnosticEngine(config *DiagnosticConfig) (*DiagnosticEngineImpl, error) {
    engine := &DiagnosticEngineImpl{
        config: config,
    }

    // 初始化内置诊断策略
    engine.builtinStrategy = NewBuiltInDiagnosticStrategy(config.Builtin.RulesPath)

    // 如果启用AI，初始化AI诊断策略
    if config.AI.Enabled {
        aiClient, err := NewAIClient(config.AI)
        if err != nil {
            log.Printf("Failed to initialize AI client: %v, fallback to builtin", err)
            engine.strategy = engine.builtinStrategy
        } else {
            engine.aiStrategy = NewAIDiagnosticStrategy(aiClient)
            engine.strategy = engine.aiStrategy
        }
    } else {
        engine.strategy = engine.builtinStrategy
    }

    return engine, nil
}

// Analyze 执行诊断分析
func (e *DiagnosticEngineImpl) Analyze(audit *Audit) (*DiagnosisResult, error) {
    // 根据配置选择诊断策略
    var strategy DiagnosticStrategy
    if e.config.AI.Enabled && e.aiStrategy != nil {
        strategy = e.aiStrategy
    } else {
        strategy = e.builtinStrategy
    }

    // 执行诊断
    result, err := strategy.Analyze(audit)
    if err != nil {
        // 如果AI诊断失败且配置了降级，使用内置诊断
        if e.config.AI.Enabled && e.config.AI.FallbackToBuiltin && strategy == e.aiStrategy {
            log.Printf("AI diagnosis failed: %v, fallback to builtin", err)
            result, err = e.builtinStrategy.Analyze(audit)
            if err != nil {
                return nil, err
            }
        } else {
            return nil, err
        }
    }

    return result, nil
}
```

### 内置诊断策略（规则引擎）

内置诊断策略基于预定义的规则进行诊断，包括：

1. **CPU瓶颈检测**：
   - CPU使用率超过阈值（如80%）
   - CPU等待时间过长

2. **内存瓶颈检测**：
   - 内存使用率超过阈值（如80%）
   - 内存泄漏检测

3. **IO瓶颈检测**：
   - 磁盘I/O延迟过高
   - 磁盘I/O吞吐量过低

4. **网络瓶颈检测**：
   - 网络延迟过高
   - 网络丢包率过高

5. **进程瓶颈检测**：
   - 进程数量过多
   - 进程状态异常

### AI诊断策略

AI诊断策略使用大语言模型进行智能诊断：

1. **输入**：审计数据、聚合事件、系统指标
2. **输出**：性能瓶颈分析、优化建议、根因分析
3. **优势**：
   - 能够发现复杂的性能问题
   - 提供更准确的根因分析
   - 生成更详细的优化建议

### 降级策略

- AI诊断失败时自动降级到内置诊断
- 超时自动降级
- 可配置是否启用降级

## 配置设计

### Collector配置

```yaml
# configs/service.yaml
collector:
  address: "localhost:50051"  # collector服务地址（gRPC）
  timeout: "30s"  # 调用超时时间
  max_retries: 3  # 最大重试次数

# 数据库配置
storage:
  policy:
    type: "sqlite"  # sqlite, mysql, postgres, leveldb
    sqlite:
      path: "/var/lib/nuts/policies.db"
    mysql:
      host: "localhost"
      port: 3306
      database: "nuts"
      username: "nuts"
      password: "password"
    postgres:
      host: "localhost"
      port: 5432
      database: "nuts"
      username: "nuts"
      password: "password"
    leveldb:
      path: "/var/lib/nuts/policies"

  event:
    type: "influxdb"  # influxdb, clickhouse, timescaledb, leveldb
    influxdb:
      url: "http://localhost:8086"
      token: "token"
      org: "nuts"
      bucket: "events"
    clickhouse:
      host: "localhost"
      port: 9000
      database: "nuts"
      username: "default"
      password: ""
    timescaledb:
      host: "localhost"
      port: 5432
      database: "nuts"
      username: "nuts"
      password: "password"
    leveldb:
      path: "/var/lib/nuts/events"

  audit:
    type: "sqlite"  # sqlite, mysql, postgres
    sqlite:
      path: "/var/lib/nuts/audits.db"
    mysql:
      host: "localhost"
      port: 3306
      database: "nuts"
      username: "nuts"
      password: "password"
    postgres:
      host: "localhost"
      port: 5432
      database: "nuts"
      username: "nuts"
      password: "password"

  diagnosis:
    type: "sqlite"  # sqlite, mysql, postgres
    sqlite:
      path: "/var/lib/nuts/diagnoses.db"
    mysql:
      host: "localhost"
      port: 3306
      database: "nuts"
      username: "nuts"
      password: "password"
    postgres:
      host: "localhost"
      port: 5432
      database: "nuts"
      username: "nuts"
      password: "password"

# 聚合引擎配置
aggregation:
  algorithm: "simple"  # simple, timewindow, statistical, frequency, custom
  timewindow:
    window: "1m"  # 时间窗口大小（仅timewindow算法使用）
  statistical:
    metrics: ["mean", "median", "p95", "p99"]  # 统计指标（仅statistical算法使用）
  frequency:
    threshold: 10  # 频率阈值（仅frequency算法使用）

# 诊断引擎配置
diagnostic:
  strategy: "builtin"  # builtin（内置规则）或 ai（AI诊断）
  builtin:
    rules_path: "/opt/nuts/rules"  # 内置规则文件路径
  ai:
    enabled: false  # 是否启用AI诊断
    provider: "openai"  # openai, local
    model: "gpt-4"  # AI模型
    api_key: ""  # API密钥
    api_endpoint: ""  # API端点
    timeout: "30s"  # 超时时间
    max_retries: 3  # 最大重试次数
  fallback_to_builtin: true  # AI失败时是否降级到内置诊断
```

```yaml
# configs/collector.yaml
server:
  address: "0.0.0.0:50051"  # gRPC服务监听地址（独立模式用）
bpf:
  script_path: "/opt/nuts/scripts"  # BPF脚本路径
```

## 接口设计

### 策略管理接口（HTTP/gRPC）

```
POST   /api/v1/policies          - 创建策略
PUT    /api/v1/policies/{id}     - 更新策略
DELETE /api/v1/policies/{id}     - 删除策略
GET    /api/v1/policies/{id}     - 查询策略
GET    /api/v1/policies          - 策略列表
```

### 策略数据结构

```go
type Policy struct {
    ID          string            `json:"id"`
    Name        string            `json:"name"`
    Targets     []Target          `json:"targets"`      // pod/容器名称或ID
    Metrics     []string          `json:"metrics"`      // 监控指标
    Duration    time.Duration     `json:"duration"`     // 监控时长
    CreatedAt   time.Time         `json:"created_at"`
    UpdatedAt   time.Time         `json:"updated_at"`
}

type Target struct {
    Type      string `json:"type"`      // "pod" 或 "container"
    Name      string `json:"name"`
    Namespace string `json:"namespace,omitempty"`
}
```

### 内部接口（gRPC/Unix Domain Socket）

- **事件匹配接口**: DataSource -> PolicyEngine
- **启动采集接口**: PolicyEngine -> Collector（通过gRPC客户端调用Collector服务）
- **停止采集接口**: PolicyEngine -> Collector（通过gRPC客户端调用Collector服务）
- **创建任务接口**: PolicyEngine -> AggregationEngine
- **诊断通知接口**: AggregationEngine -> DiagnosticEngine

## 数据库设计

### 数据库抽象设计优势

通过接口抽象设计数据库层，系统具有以下优势：

1. **灵活性**：根据不同场景选择合适的数据库
   - 开发/测试环境：使用SQLite（无需额外部署）
   - 小规模生产：使用MySQL/PostgreSQL
   - 大规模生产：使用ClickHouse/InfluxDB（时序数据优化）
   - 边缘场景：使用LevelDB（轻量级嵌入式）

2. **可扩展性**：轻松添加新的数据库支持，不影响业务逻辑

3. **可测试性**：在单元测试中使用SQLite，提高测试效率

4. **成本优化**：根据数据量和访问模式选择最具成本效益的方案

### 数据库选型建议

| 数据类型 | 开发/测试 | 小规模生产 | 大规模生产 | 边缘场景 |
|---------|----------|-----------|-----------|---------|
| 策略数据 | SQLite | MySQL/PostgreSQL | MySQL/PostgreSQL | LevelDB |
| 事件数据 | LevelDB | InfluxDB/TimescaleDB | ClickHouse/InfluxDB | LevelDB |
| 审计数据 | SQLite | MySQL/PostgreSQL | MySQL/PostgreSQL | LevelDB |
| 诊断数据 | SQLite | MySQL/PostgreSQL | MySQL/PostgreSQL | LevelDB |

### 数据表设计

#### 策略表（policies）

**SQLite/MySQL/PostgreSQL**:
```sql
CREATE TABLE policies (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(256) NOT NULL,
    targets JSONB NOT NULL,
    metrics JSONB NOT NULL,
    duration BIGINT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

**LevelDB**:
- Key: `policy:{id}`
- Value: JSON序列化的策略对象

#### 事件表（events）- 时序数据库

**InfluxDB**:
```
measurement: events
tags: cgroup_id, policy_id, event_type
fields: event_data
time: timestamp
```

**ClickHouse**:
```sql
CREATE TABLE events (
    cgroup_id String,
    policy_id String,
    event_type String,
    event_data String,
    timestamp DateTime
) ENGINE = MergeTree()
ORDER BY (timestamp, cgroup_id, policy_id);
```

**TimescaleDB**:
```sql
CREATE TABLE events (
    cgroup_id VARCHAR(64),
    policy_id VARCHAR(64),
    event_type VARCHAR(64),
    event_data JSONB,
    timestamp TIMESTAMP
);
SELECT create_hypertable('events', 'timestamp');
```

**LevelDB**:
- Key: `event:{cgroup_id}:{policy_id}:{timestamp}:{event_type}`
- Value: JSON序列化的事件对象

#### 审计表（audits）

**SQLite/MySQL/PostgreSQL**:
```sql
CREATE TABLE audits (
    id VARCHAR(64) PRIMARY KEY,
    policy_id VARCHAR(64) NOT NULL,
    cgroup_id VARCHAR(64) NOT NULL,
    start_time TIMESTAMP NOT NULL,
    end_time TIMESTAMP NOT NULL,
    aggregated_data JSONB NOT NULL,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (policy_id) REFERENCES policies(id)
);
```

**LevelDB**:
- Key: `audit:{id}`
- Value: JSON序列化的审计对象

#### 诊断结果表（diagnoses）

**SQLite/MySQL/PostgreSQL**:
```sql
CREATE TABLE diagnoses (
    id VARCHAR(64) PRIMARY KEY,
    audit_id VARCHAR(64) NOT NULL,
    bottlenecks JSONB NOT NULL,
    report TEXT,
    created_at TIMESTAMP NOT NULL,
    FOREIGN KEY (audit_id) REFERENCES audits(id)
);
```

**LevelDB**:
- Key: `diagnosis:{id}`
- Value: JSON序列化的诊断对象

## 错误处理

### 关键错误和处理策略

| 错误编号 | 错误名称 | 处理方式 |
|---------|---------|---------|
| ERR-001 | NRI连接失败 | 重试连接（最大3次），记录日志，告警 |
| ERR-002 | 事件监听失败 | 重新注册，记录日志 |
| ERR-004 | 策略解析失败 | 返回400错误，提示具体错误 |
| ERR-005 | 策略存储失败 | 重试（最大3次），记录日志，告警 |
| ERR-007 | BPF脚本加载失败 | 停止采集，记录日志，告警 |
| ERR-008 | BPF权限不足 | 记录日志，提示提权 |
| ERR-009 | 数据写入失败 | 缓存数据，重试（最大5次） |
| ERR-016 | AI调用失败 | 降级到规则引擎 |

## 部署配置

### Collector部署配置（DaemonSet）

```yaml
# deployments/collector-daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nuts-collector
spec:
  selector:
    matchLabels:
      app: nuts-collector
  template:
    metadata:
      labels:
        app: nuts-collector
    spec:
      hostPID: true  # 需要访问主机PID namespace
      hostNetwork: true  # 需要访问主机网络
      containers:
      - name: collector
        image: nuts-collector:latest
        securityContext:
          privileged: true  # 需要特权运行eBPF
          capabilities:
            add:
            - CAP_BPF
            - CAP_SYS_ADMIN
            - CAP_PERFMON
        volumeMounts:
        - name: sys
          mountPath: /sys
        - name: proc
          mountPath: /proc
      volumes:
      - name: sys
        hostPath:
          path: /sys
      - name: proc
        hostPath:
          path: /proc
```

### Service部署配置（Deployment）

```yaml
# deployments/service-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nuts-service
spec:
  replicas: 2  # 可以多副本部署
  selector:
    matchLabels:
      app: nuts-service
  template:
    metadata:
      labels:
        app: nuts-service
    spec:
      containers:
      - name: service
        image: nuts-service:latest
        # 无需特权配置
        env:
        - name: COLLECTOR_ADDRESS
          value: "nuts-collector:50051"
```

## 风险和注意事项

### 主要风险

1. **BPF技术复杂度高**：需要深入理解内核机制
2. **containerd NRI机制集成复杂度较高**：需要处理版本兼容性
3. **数据采集可能影响目标容器性能**：需要优化BPF脚本，降低开销
4. **AI诊断模型的准确性和可靠性需要持续优化**：需要持续训练和调优
5. **主机时间变化导致事件时间不一致**：需要考虑时间同步问题

### 安全和权限考虑

**部署模式（独立进程模式）**：
- Collector：DaemonSet部署，特权容器（需要eBPF权限）
- Service：Deployment部署，普通容器（无特权，可多副本）
- 优点：
  - 安全隔离好，service无特权
  - Service可多副本部署，提高可用性
  - Collector独立升级，不影响service
- 缺点：
  - 增加gRPC通信开销
  - 部署复杂度略高
- 适用场景：生产环境、安全要求高的场景

### 安全措施

1. BPF程序运行权限严格控制
2. 策略接口进行身份认证和授权
3. 采集数据加密存储
4. 审计日志完整记录
5. 敏感数据脱敏处理

### 开发环境要求

- 第一期考虑在v11 2503上开启ebpf的环境运行
- 需要root权限运行BPF程序
- 依赖cri-o/containerd/crius作为容器运行时
- 通过cgroup来关联事件和pod/容器的关系

## 命名规则

- 模块命名采用驼峰命名法
- 接口命名采用RESTful风格
- 数据库表名采用下划线分隔

## 验收标准

### 第一阶段验收标准

- [ ] CLI工具能够成功推送策略到service
- [ ] Service能够正确存储和管理策略
- [ ] 策略验证逻辑正确
- [ ] 单元测试覆盖率 > 80%
- [ ] API文档完整

### 第二阶段验收标准

- [ ] 能够正确接收NRI事件
- [ ] 策略匹配逻辑正确
- [ ] 采集器能够按需启动和停止
- [ ] 聚合引擎能够正确聚合事件
- [ ] 数据正确写入时序数据库
- [ ] 端到端测试通过

### 第三阶段验收标准

- [ ] 诊断引擎能够正确分析审计数据
- [ ] AI诊断功能正常
- [ ] 降级策略有效
- [ ] 报告生成正确
- [ ] 性能满足要求

## 后续优化方向

1. 支持多种聚合算法可配置
2. 支持插件化采集脚本扩展
3. 支持多种诊断策略
4. 支持分布式部署
5. 支持更多容器运行时
