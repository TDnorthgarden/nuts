# NUTS 通用框架设计文档

## 前言

### 设计思路

NUTS通用框架是一个与业务逻辑无关的核心框架，旨在提供一套可复用、可扩展的基础设施，用于构建基于事件驱动的任务调度系统。

**核心设计原则**：
1. **接口抽象优先**：所有核心组件通过接口定义，支持多种实现
2. **事件驱动架构**：基于事件总线实现组件间松耦合通信
3. **配置驱动**：通过配置文件控制行为，减少硬编码
4. **工厂模式**：使用工厂注册模式支持动态扩展
5. **开闭原则**：对扩展开放，对修改关闭

**混合架构设计**：
框架采用混合架构，根据组件部署位置选择最优的通信方式，兼顾性能和分布式需求：
- **进程内通信**：数据源和策略引擎在同一进程内，通过Go Channel直接通信（零拷贝，高性能，无序列化开销）
- **跨进程通信**：TaskScheduler和引擎插件通过EventBus通信（gRPC/Redis/Kafka，支持分布式部署）
- **设计优势**：
  - 高频数据源（如NRI监控）通过Channel避免网络和序列化开销
  - 跨机器分布式部署通过EventBus实现组件解耦
  - 可根据部署场景灵活选择通信方式

**框架定位**：
- 与具体业务逻辑无关
- 提供通用的数据源、策略引擎、任务调度、数据库抽象
- 支持单机和分布式部署场景
- 可通过插件机制扩展业务功能

**插件系统设计**：
- 插件通过实现框架定义的接口来扩展功能
- 框架提供接口定义（DataSource、PolicyEngine、EventBus等）
- 插件提供具体实现（NRI数据源、libdslgo策略引擎、gRPC EventBus等）
- 通过工厂模式注册和创建插件实例
- 插件在编译时集成，无需动态加载机制
- 具体插件实现请参考plugin.md文档

### 整体架构

```mermaid
graph LR
    subgraph NUTS通用框架架构
        subgraph 数据源抽象层
            DS[DataSource Interface]
            DSF[DataSourceFactory]
            DSF -.管理.-> DS1[NRI实现]
            DSF -.管理.-> DS2[Docker实现]
            DSF -.管理.-> DS3[其他实现]
            DS1 -.实现.-> DS
            DS2 -.实现.-> DS
            DS3 -.实现.-> DS
        end
        
        subgraph 数据源管理器
            DSM[DataSourceManager]
        end
        
        subgraph 策略引擎抽象层
            PE[PolicyEngine Interface]
            DSE[DSLEngine Interface]
            DSEF[DSLEngineFactory]
            DSEF -.管理.-> DE1[libdslgo实现]
            DSEF -.管理.-> DE2[CEL实现]
            DSEF -.管理.-> DE3[Rego实现]
            DE1 -.实现.-> DSE
            DE2 -.实现.-> DSE
            DE3 -.实现.-> DSE
        end
        
        subgraph 任务调度模块
            TS[TaskScheduler]
            SM[StateMachine]
            SMF[StateMachineFactory]
            IDG[ID Generator]
            EB[EventBus Interface]
            EBF[EventBusFactory]
            SMF -.创建.-> TS
            SMF -.管理.-> SH1[采集状态处理器]
            SMF -.管理.-> SH2[聚合状态处理器]
            SMF -.管理.-> SH3[诊断状态处理器]
            EBF -.管理.-> EB1[gRPC实现]
            EBF -.管理.-> EB2[Redis实现]
            EBF -.管理.-> EB3[Kafka实现]
            EB1 -.实现.-> EB
            EB2 -.实现.-> EB
            EB3 -.实现.-> EB
        end
        
        subgraph 引擎抽象层
            EI[Engine Interface]
            EF[EngineFactory]
            EF -.管理.-> E1[聚合引擎实现]
            EF -.管理.-> E2[诊断引擎实现]
            EF -.管理.-> E3[其他引擎实现]
            E1 -.实现.-> EI
            E2 -.实现.-> EI
            E3 -.实现.-> EI
        end
        
        subgraph 数据库抽象层
            DB[DB Interface]
            TSDB[TimeSeriesDB Interface]
            KV[KV Interface]
            DF[DBFactory]
            DF -.管理.-> D1[SQLite实现]
            DF -.管理.-> D2[MySQL实现]
            DF -.管理.-> D3[InfluxDB实现]
            D1 -.实现.-> DB
            D2 -.实现.-> DB
            D3 -.实现.-> TSDB
        end
    end
    DS --> DSM
    DSM --> PE
    PE -.调用.-> DSE
    PE --> TS
    TS --> SM
    TS --> IDG
    TS --> EB
    EB -.通信.-> EI
    EI --> DB
    EI --> TSDB
    EI --> KV
```

### 核心组件

1. **数据源抽象层**：提供DataSource接口和DataSourceFactory，工厂管理具体数据源实现（NRI、Docker等）
2. **策略引擎抽象层**：提供PolicyEngine接口和DSLEngine接口，DSLEngineFactory管理具体DSL引擎实现（libdslgo、CEL、Rego等）
3. **任务调度模块**：TaskScheduler包含StateMachine、EventBus和IDGenerator，StateMachineFactory管理状态处理器实现，EventBusFactory管理事件总线实现（gRPC、Redis、Kafka等）
4. **引擎抽象层**：提供Engine接口和EngineFactory，工厂管理具体业务引擎实现（聚合引擎、诊断引擎等）
5. **数据库抽象层**：提供DB、TimeSeriesDB、KV接口和DBFactory，工厂管理具体数据库实现（SQLite、MySQL、InfluxDB等）

### 设计优势

- **业务无关**：框架与具体业务逻辑解耦，可复用于不同场景
- **可扩展**：通过接口抽象和工厂模式支持动态扩展
- **可配置**：通过配置文件控制行为，无需修改代码
- **场景适配**：支持单机和分布式部署场景
- **技术栈无关**：不依赖特定的技术栈，可灵活选择实现

---

## 一、通用数据结构定义

### 1.1 统一Event结构

为了确保各模块使用统一的事件格式，定义通用的Event结构。

```go
// pkg/common/event.go
package common

import "time"

// Event 统一事件结构
type Event struct {
    // ID 事件ID
    ID string `json:"id"`
    
    // Type 事件类型
    Type string `json:"type"`
    
    // Topic 事件主题
    Topic string `json:"topic"`
    
    // Timestamp 时间戳
    Timestamp time.Time `json:"timestamp"`
    
    // Payload 事件载荷
    Payload map[string]interface{} `json:"payload"`
    
    // TraceID 链路追踪ID
    TraceID string `json:"trace_id,omitempty"`
    
    // Source 事件来源
    Source string `json:"source,omitempty"`
}
```

---

## 二、数据源接口抽象设计

### 2.1 设计目标

为了支持多种数据源（NRI、Docker SDK、containerd SDK等），需要对数据源进行接口抽象，实现以下目标：

1. **统一接口**：定义统一的数据源接口，屏蔽底层实现差异
2. **可配置性**：通过配置文件选择和配置数据源
3. **可扩展性**：支持动态添加新的数据源实现
4. **数据标准化**：统一不同数据源的事件格式

### 2.1.1 事件统一转化设计

**设计原则**：事件统一转化应该在DataSourceManager（数据源层）完成，而不是在PolicyEngine中。

**原因**：

1. **职责分离**：
   - DataSourceManager负责数据标准化和转换
   - PolicyEngine负责策略匹配和任务创建
   - 符合单一职责原则

2. **可扩展性**：
   - 添加新数据源时，只需要在DataSourceManager中添加新的转换逻辑
   - PolicyEngine无需关心数据来源，只处理统一格式的事件

3. **可维护性**：
   - 数据格式变化只影响DataSourceManager
   - PolicyEngine的代码保持稳定

4. **性能考虑**：
   - 在DataSourceManager中进行转换，可以提前过滤无效数据
   - 减少PolicyEngine的处理负担

**实现方式**：

每个数据源在内部实现时，应该在接收到原始事件后立即转换为统一的Event格式，然后发送到eventCh。DataSourceManager不需要关心具体的转换逻辑，只负责转发已经转换好的事件。

具体的数据源实现（如NRI、Docker SDK、containerd SDK等）请参考plugin.md文档。

**设计优势**：
1. **自动化转换**：数据源内部自动完成转换，框架无需调用转换方法
2. **封装性**：转换逻辑封装在数据源内部，DataSourceManager无需关心
3. **一致性**：所有数据源通过Subscribe()返回统一格式的Event
4. **易于扩展**：新数据源只需在内部实现转换逻辑，无需修改框架代码

DataSourceManager负责聚合所有数据源的事件，并输出统一格式。

具体实现请参考plugin.md文档。

### 2.1.2 接口设计

```go
// DataSource 数据源接口
type DataSource interface {
    // Start 启动数据源
    Start() error

    // Stop 停止数据源
    Stop() error

    // Subscribe 订阅事件
    Subscribe() <-chan Event

    // GetName 获取数据源名称
    GetName() string

    // GetStatus 获取数据源状态
    GetStatus() DataSourceStatus
}

// DataSourceStatus 数据源状态
type DataSourceStatus int

const (
    StatusStopped DataSourceStatus = iota
    StatusRunning
    StatusError
)
```

### 2.2 数据源管理器设计

数据源管理器采用工厂模式，支持注册多种数据源实现，但同一时刻仅有一种数据源生效。

#### 2.2.1 设计目标

1. **工厂模式**：通过工厂注册和管理数据源实现
2. **单数据源生效**：同一时刻仅使用一种数据源，简化管理
3. **动态切换**：支持运行时切换数据源（需重启服务）
4. **配置驱动**：通过配置文件指定当前使用的数据源

#### 2.2.2 接口设计

```go
// pkg/datasource/manager.go
package datasource

// DataSourceFactory 数据源工厂
type DataSourceFactory struct {
    mu      sync.RWMutex
    drivers map[string]DataSourceConstructor
}

// DataSourceConstructor 数据源构造函数
type DataSourceConstructor func(config map[string]interface{}) (DataSource, error)

// NewDataSourceFactory 创建数据源工厂
func NewDataSourceFactory() *DataSourceFactory {
    return &DataSourceFactory{
        drivers: make(map[string]DataSourceConstructor),
    }
}

// RegisterDriver 注册数据源驱动
func (f *DataSourceFactory) RegisterDriver(name string, constructor DataSourceConstructor) {
    f.mu.Lock()
    defer f.mu.Unlock()
    f.drivers[name] = constructor
}

// Create 创建数据源实例
func (f *DataSourceFactory) Create(name string, config map[string]interface{}) (DataSource, error) {
    f.mu.RLock()
    constructor, ok := f.drivers[name]
    f.mu.RUnlock()

    if !ok {
        return nil, fmt.Errorf("datasource driver not found: %s", name)
    }

    return constructor(config)
}

// DataSourceManager 数据源管理器接口
// 支持注册多种数据源，但同一时刻仅有一种数据源生效
type DataSourceManager interface {
    // SetActive 设置当前激活的数据源
    SetActive(name string, source DataSource) error
    
    // GetActive 获取当前激活的数据源
    GetActive() (DataSource, error)
    
    // Start 启动当前激活的数据源
    Start() error
    
    // Stop 停止当前激活的数据源
    Stop() error
    
    // Subscribe 订阅数据源事件
    // 在混合架构下，数据源和策略引擎在同一进程内，通过channel直接通信
    Subscribe() <-chan Event
}
```

#### 2.2.3 配置设计

```yaml
datasource:
  # 当前启用的数据源
  active: "nri"
  
  # 数据源配置
  nri:
    socket_path: "/var/run/nri.sock"
  docker:
    host: "unix:///var/run/docker.sock"
```

#### 2.2.4 数据源与策略引擎交互流程

在混合架构下，数据源和策略引擎在同一进程内，通过channel直接通信，无需经过EventBus。

**交互流程**：
1. DataSourceManager调用DataSource.Subscribe()获取数据源事件channel
2. DataSourceManager订阅数据源事件channel
3. DataSourceManager将事件通过内部channel转发给PolicyEngine
4. PolicyEngine接收事件后调用MatchAll()进行策略匹配
5. 匹配成功后，PolicyEngine通过EventBus发布事件给TaskScheduler

**实现示例**：
```go
// pkg/datasource/manager.go

type DataSourceManagerImpl struct {
    mu           sync.RWMutex
    active       DataSource
    eventCh      chan Event
    policyEngine policy.PolicyEngine
}

func (m *DataSourceManagerImpl) Subscribe() <-chan Event {
    return m.eventCh
}

func (m *DataSourceManagerImpl) Start() error {
    m.mu.Lock()
    defer m.mu.Unlock()
    
    if m.active == nil {
        return fmt.Errorf("no active datasource")
    }
    
    // 启动数据源
    if err := m.active.Start(); err != nil {
        return err
    }
    
    // 订阅数据源事件
    sourceCh := m.active.Subscribe()
    
    // 启动事件转发goroutine
    go func() {
        for event := range sourceCh {
            // 转发给策略引擎
            if m.policyEngine != nil {
                m.eventCh <- event
            }
        }
    }()
    
    return nil
}

func (m *DataSourceManagerImpl) SetPolicyEngine(pe policy.PolicyEngine) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.policyEngine = pe
}
```

### 2.3 目录结构

```
pkg/datasource/
├── interface.go          # 数据源接口定义
├── manager.go            # 数据源工厂和管理器
├── event.go              # 统一事件格式
└── factory.go            # 数据源工厂
```

具体的数据源实现（如nri、docker、containerd等）在插件层实现，详见plugin.md文档。

---

## 三、策略引擎接口抽象设计

### 3.1 设计目标

为了支持不同的策略DSL引擎（如libdslgo、CEL、Rego等），需要对策略引擎进行接口抽象，实现以下目标：

1. **DSL引擎可插拔**：支持不同的DSL引擎实现
2. **策略配置通用化**：策略配置不包含特定业务字段
3. **职责单一**：策略引擎只负责DSL解析和匹配，不负责业务通知
4. **事件驱动**：匹配成功后通过事件总线发布事件，TaskScheduler订阅事件

### 3.2 接口设计

```go
// pkg/policy/interface.go
package policy

import "github.com/sig-cloudnative/nuts/pkg/common"

// Policy 策略结构（通用化）
type Policy struct {
    ID         string
    Name       string
    DSL        string              // DSL规则
    TaskConfig map[string]interface{}  // 通用任务配置
    Metadata   map[string]interface{}  // 元数据
    CreatedAt  time.Time
    UpdatedAt  time.Time
}

// PolicyReceiver 策略接收器接口
type PolicyReceiver interface {
    // Receive 接收策略
    Receive(policy *Policy) error
    
    // Update 更新策略
    Update(policy *Policy) error
    
    // Delete 删除策略
    Delete(id string) error
    
    // Get 获取策略
    Get(id string) (*Policy, error)
    
    // List 列出所有策略
    List() ([]*Policy, error)
}

// PolicyMatcher 策略匹配器接口
type PolicyMatcher interface {
    // Match 匹配事件是否符合策略
    Match(event *common.Event, policy *Policy) (bool, string, error)
}

// PolicyEngine 策略引擎接口
type PolicyEngine interface {
    PolicyReceiver
    PolicyMatcher
    
    // ParseDSL 解析DSL规则
    ParseDSL(dsl string) error
    
    // MatchAll 匹配事件是否符合所有策略
    MatchAll(event *common.Event) ([]*MatchResult, error)
    
    // Subscribe 订阅数据源事件
    // 在混合架构下，通过channel接收DataSourceManager转发的事件
    Subscribe(eventCh <-chan *common.Event)
}

// MatchResult 匹配结果
type MatchResult struct {
    PolicyID   string
    Matched    bool
    Reason     string
    TaskConfig map[string]interface{}  // 策略的任务配置
}
```

#### 3.2.1 PolicyEngine实现示例

```go
// pkg/policy/engine.go

type PolicyEngineImpl struct {
    mu         sync.RWMutex
    policies   map[string]*Policy
    dslEngine  dsl.DSLEngine
    eventBus   eventbus.EventBus
    eventCh    <-chan *common.Event
    stopCh     chan struct{}
}

func (e *PolicyEngineImpl) Subscribe(eventCh <-chan *common.Event) {
    e.mu.Lock()
    defer e.mu.Unlock()
    e.eventCh = eventCh
    
    // 启动事件处理goroutine
    go e.processEvents()
}

func (e *PolicyEngineImpl) processEvents() {
    for {
        select {
        case event := <-e.eventCh:
            // 匹配策略
            results, err := e.MatchAll(event)
            if err != nil {
                log.Printf("MatchAll error: %v", err)
                continue
            }
            
            // 发布匹配成功的事件
            for _, result := range results {
                if result.Matched {
                    e.publishMatchedEvent(event, result)
                }
            }
        case <-e.stopCh:
            return
        }
    }
}

func (e *PolicyEngineImpl) publishMatchedEvent(event *common.Event, result *MatchResult) {
    matchedEvent := &common.Event{
        ID:        uuid.New().String(),
        Type:      "PolicyMatched",
        Topic:     "policy.matched",
        Timestamp: time.Now(),
        Payload: map[string]interface{}{
            "original_event": event,
            "policy_id":      result.PolicyID,
            "task_config":    result.TaskConfig,
        },
    }
    
    if err := e.eventBus.Publish("policy.matched", matchedEvent); err != nil {
        log.Printf("Publish PolicyMatched event error: %v", err)
    }
}
```

### 3.3 DSL引擎接口抽象

为了支持不同的DSL引擎（libdslgo、CEL、Rego等），需要对DSL引擎进行接口抽象，使其与具体引擎实现无关。

```go
// pkg/dsl/interface.go
package dsl

import (
    "time"
    "github.com/sig-cloudnative/nuts/pkg/policy"
)

// EvaluationResult 评估结果
type EvaluationResult struct {
    Matched   bool                   `json:"matched"`    // 是否匹配
    RuleID    string                 `json:"rule_id"`    // 匹配的规则ID
    RuleName  string                 `json:"rule_name"`  // 规则名称
    Output    string                 `json:"output"`     // 输出信息
    Variables map[string]interface{} `json:"variables"`  // 提取的变量
    Error     error                  `json:"error"`      // 错误信息
}

// DSLEngine DSL引擎接口
type DSLEngine interface {
    // Init 初始化引擎
    Init(config map[string]interface{}) error
    
    // Name 引擎名称
    Name() string
    
    // Validate 验证规则语法
    // 参数：规则字符串（引擎特定格式）
    // 返回：错误信息，nil表示验证通过
    Validate(rule string) error
    
    // AddRule 添加规则
    // 参数：规则字符串（引擎特定格式）
    // 返回：错误信息
    AddRule(rule string) error
    
    // GetRule 获取规则
    // 参数：规则ID
    // 返回：规则字符串，错误信息
    GetRule(ruleID string) (string, error)
    
    // UpdateRule 更新规则
    // 参数：规则字符串（引擎特定格式）
    // 返回：错误信息
    UpdateRule(rule string) error
    
    // DeleteRule 删除规则
    // 参数：规则ID
    // 返回：错误信息
    DeleteRule(ruleID string) error
    
    // ListRules 列出所有规则
    // 返回：策略列表（参考pkg/policy/interface.go中的Policy结构）
    ListRules() ([]*policy.Policy, error)
    
    // Evaluate 评估事件是否符合规则
    // 返回所有匹配的规则及其结果
    Evaluate(event map[string]interface{}) ([]*EvaluationResult, error)
    
    // Close 关闭引擎
    Close() error
}
```

### 3.4 DSL引擎工厂

```go
// pkg/dsl/factory.go
package dsl

type DSLEngineFactory struct {
    engines map[string]func(config map[string]interface{}) (DSLEngine, error)
}

func NewDSLEngineFactory() *DSLEngineFactory
func (f *DSLEngineFactory) RegisterEngine(name string, factory func(config map[string]interface{}) (DSLEngine, error))
func (f *DSLEngineFactory) CreateEngine(name string, config map[string]interface{}) (DSLEngine, error)
```

具体实现请参考plugin.md文档。

### 3.5 PolicyEngine工厂

```go
// pkg/policy/factory.go
package policy

type PolicyEngineFactory struct {
    engines map[string]func(config map[string]interface{}) (PolicyEngine, error)
}

func NewPolicyEngineFactory() *PolicyEngineFactory
func (f *PolicyEngineFactory) RegisterEngine(name string, factory func(config map[string]interface{}) (PolicyEngine, error))
func (f *PolicyEngineFactory) CreateEngine(name string, config map[string]interface{}) (PolicyEngine, error)
```

### 3.6 设计优势

1. **职责单一**：策略引擎只负责DSL解析和匹配，不负责业务通知
2. **通用化配置**：Policy结构使用TaskConfig传递通用配置，不包含特定业务字段
3. **事件驱动**：匹配成功后通过事件总线发布事件，TaskScheduler订阅事件
4. **DSL可插拔**：支持不同的DSL引擎实现（libdslgo、CEL、Rego等）
5. **易于扩展**：新增策略字段只需扩展TaskConfig，无需修改Policy结构

---

## 四、任务调度模块设计

### 4.1 TaskScheduler接口设计

#### 4.1.1 接口定义

```go
// pkg/scheduler/interface.go
package scheduler

import "github.com/sig-cloudnative/nuts/pkg/common"

// TaskScheduler 任务调度器接口
type TaskScheduler interface {
    // CreateTask 创建任务
    CreateTask(task *Task) error
    
    // GetTask 获取任务
    GetTask(id string) (*Task, error)
    
    // UpdateTask 更新任务
    UpdateTask(task *Task) error
    
    // DeleteTask 删除任务
    DeleteTask(id string) error
    
    // ListTasks 列出任务
    ListTasks(filter *TaskFilter) ([]*Task, error)
    
    // Start 启动调度器
    Start() error
    
    // Stop 停止调度器
    Stop() error
    
    // GetStateMachine 获取状态机
    GetStateMachine() StateMachine
    
    // GetEventBus 获取事件总线
    GetEventBus() eventbus.EventBus
}
```

#### 4.1.2 Task结构定义

```go
// pkg/scheduler/task.go
package scheduler

import "time"

// Task 任务定义
type Task struct {
    ID          string                 `json:"id"`
    PolicyID    string                 `json:"policy_id"`
    State       string                 `json:"state"`
    Metadata    map[string]interface{} `json:"metadata"`
    CreatedAt   time.Time              `json:"created_at"`
    UpdatedAt   time.Time              `json:"updated_at"`
    StartedAt   *time.Time             `json:"started_at,omitempty"`
    CompletedAt *time.Time             `json:"completed_at,omitempty"`
}
```

#### 4.1.3 TaskExecutor接口定义

```go
// pkg/scheduler/executor.go
package scheduler

// TaskExecutor 任务执行器接口
type TaskExecutor interface {
    // Execute 执行任务
    Execute(ctx *StateContext) error
    
    // Cancel 取消任务
    Cancel(ctx *StateContext) error
    
    // GetProgress 获取执行进度
    GetProgress(ctx *StateContext) (float64, error)
}
```

### 4.2 分布式ID生成设计

在分布式场景下，需要保证多个节点上产生的任务ID唯一。同时，该方案需要兼容单机部署场景。

#### 4.2.1 设计目标

1. **全局唯一**：在分布式环境下保证ID唯一
2. **时间有序**：ID包含时间信息，便于排序和查询
3. **高性能**：不依赖外部存储，本地生成
4. **场景通用**：同时支持单机和分布式场景

#### 4.2.2 方案选择

**推荐方案：雪花算法（Snowflake）**

**算法结构**：
```
0 | 00000000000000000000000000000000000000000 | 0000000000 | 000000000000
  |                    41位时间戳                  |  10位节点ID  | 12位序列号
```

**各部分说明**：
- **1位符号位**：始终为0
- **41位时间戳**：毫秒级，可用69年（从1970年开始）
- **10位节点ID**：支持1024个节点
- **12位序列号**：每毫秒可生成4096个ID

#### 4.2.3 节点ID分配机制

**单机部署**：
- 节点ID固定为0
- 简化实现，无需节点ID分配

**分布式部署**：
- 节点ID分配方式：
  1. **配置文件指定**：在配置文件中手动指定节点ID
  2. **环境变量**：通过环境变量NODE_ID指定
  3. **自动分配**：使用Redis/ZooKeeper协调分配

#### 4.2.4 ID生成器接口

```go
// pkg/common/id/interface.go
package id

// IDGenerator ID生成器接口
type IDGenerator interface {
    // Init 初始化生成器
    Init(config map[string]interface{}) error
    
    // Generate 生成唯一ID
    Generate() (string, error)
    
    // Validate 验证ID合法性
    Validate(id string) (*IDInfo, error)
}

// IDInfo ID信息
type IDInfo struct {
    Timestamp int64
    NodeID    int64
    Sequence  int64
}
```

具体实现（如Snowflake算法）请参考plugin.md文档。

#### 4.2.5 任务存储设计

任务存储用于持久化任务状态，支持系统重启后恢复任务状态。

#### 4.2.5.1 设计目标

1. **持久化**：任务状态持久化到数据库
2. **状态恢复**：系统重启后可恢复任务状态
3. **查询支持**：支持按任务ID查询和列表查询
4. **事务支持**：支持事务操作保证数据一致性

#### 4.2.5.2 接口设计

```go
// pkg/scheduler/store.go
package scheduler

// TaskStore 任务存储接口
type TaskStore interface {
    // Save 保存任务
    Save(task *Task) error
    
    // Get 获取任务
    Get(id string) (*Task, error)
    
    // Update 更新任务状态
    Update(id string, state string) error
    
    // UpdateTask 更新任务完整信息
    UpdateTask(task *Task) error
    
    // List 列出所有任务
    List(filter *TaskFilter) ([]*Task, error)
    
    // Delete 删除任务
    Delete(id string) error
}

// TaskFilter 任务查询过滤器
type TaskFilter struct {
    State    string
    PolicyID string
    Limit    int
    Offset   int
}
```

具体实现请参考plugin.md文档。

### 4.3 状态机接口抽象设计

为了提高系统的灵活性和可扩展性，状态机应该抽象为接口，支持根据不同的需求设定不同的状态转换规则以及每个状态要执行的操作。

#### 4.3.1 设计目标

1. **可配置性**：通过配置文件定义状态转换规则和状态处理器
2. **可扩展性**：支持添加新的状态和转换规则，无需修改核心代码
3. **多任务类型支持**：不同类型的任务可以有不同的状态机定义
4. **状态处理器抽象**：每个状态的操作通过处理器接口实现

#### 4.3.2 接口设计

```go
// StateMachine 状态机接口
type StateMachine interface {
    // GetCurrentState 获取当前状态
    GetCurrentState() State

    // Transition 状态转换
    Transition(event *common.Event) error

    // GetStateMachineConfig 获取状态机配置
    GetStateMachineConfig() *StateMachineConfig
}

// State 状态接口
type State interface {
    // GetName 获取状态名称
    GetName() string

    // OnEnter 进入状态时执行
    OnEnter(ctx *StateContext) error

    // OnExit 退出状态时执行
    OnExit(ctx *StateContext) error

    // HandleEvent 处理事件
    HandleEvent(ctx *StateContext, event *common.Event) (State, error)
}

// StateContext 状态上下文
type StateContext struct {
    Task          *Task
    Data          map[string]interface{}
    CurrentState  State
    PreviousState State
    Event         *common.Event
}

// StateMachineConfig 状态机配置
type StateMachineConfig struct {
    Name           string
    InitialState   string
    States         map[string]*StateConfig
    Transitions    map[string][]*TransitionConfig
}

// StateConfig 状态配置
type StateConfig struct {
    Name        string
    Handler     string  // 处理器名称
    Timeout     int64   // 超时时间（秒）
    RetryCount  int     // 重试次数
}

// TransitionConfig 转换配置
type TransitionConfig struct {
    From      string
    To        string
    Event     string
    Condition string  // 条件表达式（可选）
}
```

#### 4.3.4 状态处理器

状态处理器负责在状态进入、退出、转换时执行相应的操作。

```go
// StateHandler 状态处理器接口
type StateHandler interface {
    // OnEnter 进入状态时执行
    OnEnter(ctx *StateContext) error

    // OnExit 退出状态时执行
    OnExit(ctx *StateContext) error

    // Execute 执行状态操作
    Execute(ctx *StateContext) error

    // CanTransition 判断是否可以转换到下一个状态
    CanTransition(ctx *StateContext, nextState string) bool
}
```

#### 4.3.5 状态转换处理器

状态转换处理器负责判断在什么条件下可以从当前状态转换到下一个状态。不同的场景可以有不同的转换逻辑。

```go
// TransitionHandler 状态转换处理器接口（可选，用于动态转换判断）
type TransitionHandler interface {
    // CanTransition 判断是否可以执行状态转换
    // 用于处理需要动态业务逻辑判断的复杂转换场景
    // 大部分场景下，转换规则由配置文件定义，无需实现此接口
    CanTransition(ctx *StateContext, fromState, toState string, event *common.Event) (bool, error)
}

// NoOpTransitionHandler 空操作转换处理器（默认实现）
type NoOpTransitionHandler struct{}

func (h *NoOpTransitionHandler) CanTransition(ctx *StateContext, fromState, toState string, event *common.Event) (bool, error) {
    return true, nil
}
```

#### 4.3.6 状态机工厂

```go
// StateMachineFactory 状态机工厂
type StateMachineFactory struct {
    handlers          map[string]StateHandler
    transitionHandler TransitionHandler
    eventBus          eventbus.EventBus
    executors         map[string]TaskExecutor
}

func NewStateMachineFactory(eventBus eventbus.EventBus) *StateMachineFactory
func (f *StateMachineFactory) RegisterHandler(name string, handler StateHandler)
func (f *StateMachineFactory) RegisterTransitionHandler(handler TransitionHandler)
func (f *StateMachineFactory) RegisterExecutor(state string, executor TaskExecutor)
func (f *StateMachineFactory) CreateStateMachine(config *StateMachineConfig) (StateMachine, error)
```

具体实现请参考plugin.md文档。

#### 4.3.7 职责分工

**配置文件负责**（静态配置）：
- 状态定义：状态名称、处理器名称、超时时间、重试次数
- 转换规则：from状态、to状态、触发事件
- 这些都是静态的、可配置的结构信息

**代码负责**（动态逻辑）：
- StateHandler：状态的进入/退出/超时行为（业务逻辑）
- TransitionHandler：可选，只处理需要动态业务逻辑判断的复杂转换场景

**设计原则**：
- 大部分场景下，转换规则由配置文件定义，无需实现TransitionHandler
- 只有当转换需要复杂的动态判断时，才需要实现自定义TransitionHandler
- 配置文件和代码职责清晰，避免重复

### 4.4 事件总线接口抽象设计

#### 4.4.1 设计目标

为了支持单机和分布式部署场景，需要对事件总线进行接口抽象，实现以下目标：

1. **统一接口**：定义统一的事件总线接口，屏蔽底层实现差异
2. **多实现支持**：支持gRPC、Redis、Kafka等多种实现
3. **事件序列化**：定义统一的事件序列化格式（protobuf）
4. **订阅发布**：支持主题订阅和发布模式

#### 4.4.2 事件序列化格式

使用protobuf定义事件序列化格式，确保跨语言支持和高效传输。

```protobuf
// proto/event.proto
syntax = "proto3";

package nuts.event;

option go_package = "github.com/sig-cloudnative/nuts/proto/event";

// Event 事件定义
message Event {
    string type = 1;           // 事件类型
    string topic = 2;          // 事件主题
    map<string, string> payload = 3;  // 事件负载
    int64 timestamp = 4;       // 时间戳（毫秒）
    string trace_id = 5;       // 链路追踪ID
    string source = 6;         // 事件来源
}

// PublishRequest 发布请求
message PublishRequest {
    string topic = 1;
    Event event = 2;
}

// PublishResponse 发布响应
message PublishResponse {
    bool success = 1;
    string error = 2;
}

// SubscribeRequest 订阅请求
message SubscribeRequest {
    string topic = 1;
}

// SubscribeResponse 订阅响应
message SubscribeResponse {
    Event event = 1;
}
```

#### 4.4.3 接口设计

```go
// pkg/eventbus/interface.go
package eventbus

import "github.com/sig-cloudnative/nuts/pkg/common"

// EventBus 事件总线接口
type EventBus interface {
    // Publish 发布事件
    Publish(topic string, event *common.Event) error
    
    // Subscribe 订阅事件
    Subscribe(topic string) <-chan *common.Event
    
    // Unsubscribe 取消订阅
    Unsubscribe(topic string) error
    
    // Close 关闭事件总线
    Close() error
}
```

#### 4.4.4 事件总线工厂（注册模式）

```go
// pkg/eventbus/factory.go
package eventbus

// EventBusFactory 事件总线工厂
type EventBusFactory struct {
    creators map[string]func(config map[string]interface{}) (EventBus, error)
    mutex    sync.RWMutex
}

// NewEventBusFactory 创建工厂
func NewEventBusFactory() *EventBusFactory {
    factory := &EventBusFactory{
        creators: make(map[string]func(config map[string]interface{}) (EventBus, error)),
    }
    
    // 注册默认实现
    factory.Register("grpc", NewGrpcEventBus)
    factory.Register("redis", NewRedisEventBus)
    factory.Register("kafka", NewKafkaEventBus)
    
    return factory
}

// Register 注册事件总线实现
func (f *EventBusFactory) Register(name string, creator func(config map[string]interface{}) (EventBus, error)) {
    f.mutex.Lock()
    defer f.mutex.Unlock()
    f.creators[name] = creator
}

// Create 创建事件总线实例
func (f *EventBusFactory) Create(name string, config map[string]interface{}) (EventBus, error) {
    f.mutex.RLock()
    defer f.mutex.RUnlock()
    
    creator, ok := f.creators[name]
    if !ok {
        return nil, fmt.Errorf("event bus type not registered: %s", name)
    }
    
    return creator(config)
}
```

#### 4.4.5 配置设计

EventBus由TaskScheduler管理作为服务端，其他模块作为客户端连接。

**单节点部署（gRPC）**：
```yaml
eventbus:
  type: "grpc"
  addr: "localhost:50051"  # TaskScheduler的EventBus服务地址
```

**多节点部署（Redis）**：
```yaml
eventbus:
  type: "redis"
  addr: "redis://localhost:6379"  # Redis地址，所有TaskScheduler共享
```

**多节点部署（Kafka）**：
```yaml
eventbus:
  type: "kafka"
  addr: "kafka-1:9092,kafka-2:9092"  # Kafka集群地址
```

**工作模式**：
- TaskScheduler启动EventBus服务端（gRPC）或连接消息队列（Redis/Kafka）
- 其他模块（Collector、AggregationEngine等）配置EventBus地址作为客户端
- 所有模块通过同一个EventBus发布/订阅事件

#### 4.4.6 设计优势

1. **场景统一**：针对不同的部署场景，无需区分进程内/跨进程
2. **默认gRPC**：强类型、流式通信、天然跨进程、易于调试
3. **可扩展**：支持通过工厂注册模式添加新实现，无需修改工厂代码
4. **开闭原则**：新增实现只需注册，符合开闭原则
5. **部署简单**：使用gRPC无需额外部署基础设施

具体实现（gRPC、Redis、Kafka）请参考plugin.md文档。

### 4.5 任务管理模块、状态机、事件总线的结合

#### 4.5.1 三者关系

```mermaid
graph TB
    subgraph "TaskScheduler 任务调度模块"
        TaskScheduler[TaskScheduler<br/>通用调度框架]
        StateMachine[StateMachine<br/>状态机引擎]
        TaskStore[(TaskStore<br/>任务持久化)]
    end

    subgraph "状态机组件"
        StateHandlers[StateHandlers<br/>状态处理器]
        TransitionHandler[TransitionHandler<br/>转换处理器]
    end

    subgraph "事件总线 EventBus"
        EventBus[EventBus<br/>事件总线]
        Publisher[Publisher<br/>事件发布者]
        Subscriber[Subscriber<br/>事件订阅者]
    end

    %% TaskScheduler内部
    TaskScheduler --> StateMachine
    TaskScheduler --> TaskStore

    %% 状态机内部
    StateMachine --> StateHandlers
    StateMachine --> TransitionHandler

    %% 事件总线连接
    StateHandlers -->|发布事件| EventBus
    EventBus -->|订阅事件| StateHandlers

    %% TaskScheduler订阅事件
    TaskScheduler -->|订阅事件| EventBus
```

#### 4.5.2 解耦设计优势

1. **松耦合**：组件间通过事件总线异步通信，不直接依赖
2. **可扩展**：新增组件只需订阅/发布相应事件，无需修改现有代码
3. **可测试**：可以模拟事件进行单元测试
4. **高可用**：事件总线可以支持多种实现（gRPC、Redis、Kafka等）
5. **状态机可配置**：通过配置文件定义状态转换规则和处理器

### 4.6 事件驱动模式

框架采用事件驱动架构，所有组件通过EventBus或Channel进行异步通信。根据部署场景，采用混合架构：
- **进程内通信**：数据源和策略引擎在同一进程内，通过Channel直接通信
- **跨进程通信**：TaskScheduler和引擎插件通过EventBus通信（gRPC/Redis/Kafka）

#### 4.6.1 工作原理

**混合架构设计**：
- **本地层**：DataSource和PolicyEngine在同一进程内，通过Channel通信（零拷贝，高性能）
- **跨进程层**：TaskScheduler和引擎插件通过EventBus通信（支持分布式）
- **分布式层**：跨机器部署时使用Redis/Kafka作为EventBus

**完整事件流程**：
1. DataSource产生事件，通过Channel发送给DataSourceManager
2. DataSourceManager将事件转发给PolicyEngine（进程内Channel）
3. PolicyEngine调用MatchAll()进行策略匹配
4. 匹配成功后，PolicyEngine通过EventBus发布PolicyMatched事件
5. TaskScheduler订阅PolicyMatched事件，创建任务
6. TaskScheduler通过EventBus发布TaskCreated事件
7. 引擎插件订阅TaskCreated事件，执行任务
8. 引擎插件通过EventBus发布TaskCompleted事件
9. StateHandler接收TaskCompleted事件，触发状态转换

**EventBus中心化架构**：
- EventBus由TaskScheduler管理作为服务端
- 其他模块（Collector、AggregationEngine、DiagnosticEngine等）作为客户端连接
- 所有跨进程模块通过EventBus发布/订阅事件

#### 4.6.2 优势

1. **完全解耦**：引擎间无直接依赖，通过EventBus通信
2. **灵活扩展**：新增引擎只需订阅/发布事件
3. **异步处理**：事件驱动天然支持异步
4. **易于测试**：可以模拟事件进行单元测试
5. **场景统一**：单节点和多节点使用相同的事件驱动模式
6. **性能优化**：进程内使用Channel避免序列化开销，跨进程使用EventBus支持分布式
7. **架构灵活**：可根据部署场景选择Channel或EventBus

---

## 五、引擎插件设计

### 5.1 设计目标

引擎插件（如Collector、AggregationEngine、DiagnosticEngine等）通过EventBus与框架通信，无需统一的接口定义。

**设计目标**：
1. **事件驱动**：引擎插件通过EventBus订阅和发布事件
2. **无接口约束**：引擎插件无需实现统一接口，只需遵循事件协议
3. **独立部署**：引擎插件可作为独立进程运行
4. **灵活扩展**：新增引擎插件只需遵循事件协议即可

### 5.2 引擎插件与EventBus集成

#### 5.2.1 订阅事件

引擎插件通过EventBus订阅感兴趣的事件主题，使用事件处理器注册器避免硬编码switch：

```go
// 引擎插件订阅事件示例
func (e *CollectorEngine) Start(eventBus eventbus.EventBus) error {
    // 订阅采集相关事件
    eventCh, err := eventBus.Subscribe("collection")
    if err != nil {
        return err
    }
    
    // 注册事件处理器
    e.RegisterHandler("CollectionStart", e.handleCollectionStart)
    e.RegisterHandler("CollectionStop", e.handleCollectionStop)
    
    // 启动事件处理循环
    go e.handleEvents(eventCh)
    return nil
}

func (e *CollectorEngine) handleEvents(eventCh <-chan *common.Event) {
    for event := range eventCh {
        // 通过注册器查找处理器
        handler, ok := e.GetHandler(event.Type)
        if ok {
            handler(event)
        }
    }
}
```

**事件处理器注册器设计**：

```go
// pkg/engine/handler.go
package engine

import "github.com/sig-cloudnative/nuts/pkg/common"

// EventHandler 事件处理器函数类型
type EventHandler func(event *common.Event)

// EventHandlerRegistry 事件处理器注册器
type EventHandlerRegistry struct {
    handlers map[string]EventHandler
}

func NewEventHandlerRegistry() *EventHandlerRegistry {
    return &EventHandlerRegistry{
        handlers: make(map[string]EventHandler),
    }
}

// RegisterHandler 注册事件处理器
func (r *EventHandlerRegistry) RegisterHandler(eventType string, handler EventHandler) {
    r.handlers[eventType] = handler
}

// GetHandler 获取事件处理器
func (r *EventHandlerRegistry) GetHandler(eventType string) (EventHandler, bool) {
    handler, ok := r.handlers[eventType]
    return handler, ok
}

// UnregisterHandler 取消注册事件处理器
func (r *EventHandlerRegistry) UnregisterHandler(eventType string) {
    delete(r.handlers, eventType)
}
```

**引擎插件集成示例**：

```go
// pkg/engine/collector.go
package engine

type CollectorEngine struct {
    eventBus eventbus.EventBus
    *EventHandlerRegistry
}

func NewCollectorEngine() *CollectorEngine {
    return &CollectorEngine{
        EventHandlerRegistry: NewEventHandlerRegistry(),
    }
}

func (e *CollectorEngine) Start(eventBus eventbus.EventBus) error {
    e.eventBus = eventBus
    
    // 注册事件处理器
    e.RegisterHandler("CollectionStart", e.handleCollectionStart)
    e.RegisterHandler("CollectionStop", e.handleCollectionStop)
    e.RegisterHandler("CollectionPause", e.handleCollectionPause)
    
    // 订阅事件
    eventCh, err := eventBus.Subscribe("collection")
    if err != nil {
        return err
    }
    
    go e.handleEvents(eventCh)
    return nil
}

func (e *CollectorEngine) handleEvents(eventCh <-chan *common.Event) {
    for event := range eventCh {
        if handler, ok := e.GetHandler(event.Type); ok {
            handler(event)
        }
    }
}
```

#### 5.2.2 发布事件

引擎插件处理完成后，通过EventBus发布新事件：

```go
// 引擎插件发布事件示例
func (e *CollectorEngine) handleCollectionStart(event *common.Event) {
    // 执行采集逻辑
    result := e.collect(event.Payload)
    
    // 发布采集完成事件
    completeEvent := &common.Event{
        Type:      "CollectionComplete",
        Topic:     "collection",
        Timestamp: time.Now(),
        Payload:   result,
        TraceID:   event.TraceID,
    }
    
    e.eventBus.Publish("collection", completeEvent)
}
```

### 5.3 引擎插件生命周期

#### 5.3.1 启动流程

1. 引擎插件从配置文件获取EventBus连接信息
2. 连接到EventBus
3. 订阅感兴趣的事件主题
4. 启动事件处理循环

#### 5.3.2 停止流程

1. 取消事件订阅
2. 断开EventBus连接
3. 清理资源

### 5.4 引擎插件配置

```yaml
engines:
  collector:
    enabled: true
    eventbus:
      type: "grpc"
      address: "localhost:9090"
    config:
      interval: 60
      
  aggregation:
    enabled: true
    eventbus:
      type: "grpc"
      address: "localhost:9090"
    config:
      window_size: 300
```

### 5.5 事件协议

引擎插件之间通过事件协议通信，具体的事件类型定义请参考plugin.md文档。

**通用事件字段**：
- Type：事件类型
- Topic：事件主题
- Timestamp：时间戳
- Payload：事件载荷
- TraceID：链路追踪ID
- Source：事件来源

具体引擎插件实现请参考plugin.md文档。

---

## 六、数据库设计

### 6.1 数据库抽象层接口设计

#### 6.1.1 关系型数据库抽象接口

```go
// pkg/storage/interface.go
package storage

import "context"

// DB 关系型数据库统一接口
type DB interface {
    // Close 关闭数据库连接
    Close() error

    // Ping 检查数据库连接
    Ping(ctx context.Context) error

    // Begin 开启事务
    Begin(ctx context.Context) (Tx, error)
}

// Tx 事务接口
type Tx interface {
    // Commit 提交事务
    Commit() error

    // Rollback 回滚事务
    Rollback() error

    // Exec 执行SQL语句
    Exec(ctx context.Context, query string, args ...interface{}) (Result, error)

    // Query 查询单行
    Query(ctx context.Context, query string, args ...interface{}) (*Row, error)

    // QueryRows 查询多行
    QueryRows(ctx context.Context, query string, args ...interface{}) (*Rows, error)
}

// Result 执行结果
type Result interface {
    // LastInsertId 获取最后插入的ID
    LastInsertId() (int64, error)

    // RowsAffected 获取影响的行数
    RowsAffected() (int64, error)
}

// Row 单行结果
type Row interface {
    // Scan 扫描到目标结构
    Scan(dest ...interface{}) error
}

// Rows 多行结果
type Rows interface {
    // Next 移动到下一行
    Next() bool

    // Scan 扫描当前行
    Scan(dest ...interface{}) error

    // Close 关闭结果集
    Close() error

    // Err 获取错误
    Err() error
}

// DBOpenFunc 数据库打开函数类型
type DBOpenFunc func(dsn string) (DB, error)

// DBFactory 数据库工厂
type DBFactory struct {
    drivers map[string]DBOpenFunc
}

func NewDBFactory() *DBFactory
func (f *DBFactory) RegisterDriver(name string, openFunc DBOpenFunc)
func (f *DBFactory) Open(driver, dsn string) (DB, error)
```

具体数据库实现（如SQLite、MySQL、InfluxDB等）请参考plugin.md文档。

#### 6.1.2 时序数据库抽象接口

```go
// pkg/storage/event/interface.go
package event

import (
    "time"
    "github.com/sig-cloudnative/nuts/pkg/common"
)

// TimeSeriesDB 时序数据库统一接口
type TimeSeriesDB interface {
    // Close 关闭数据库连接
    Close() error

    // Write 写入时序数据
    Write(measurement string, tags map[string]string, fields map[string]interface{}, timestamp time.Time) error

    // Query 查询时序数据
    Query(measurement string, tags map[string]string, startTime, endTime time.Time) ([]*common.Event, error)

    // BatchWrite 批量写入
    BatchWrite(events []*common.Event) error
}
```

#### 6.1.3 键值存储抽象接口

```go
// pkg/storage/kv/interface.go
package kv

import "context"

// KV 键值存储统一接口
type KV interface {
    // Close 关闭存储
    Close() error

    // Get 获取值
    Get(ctx context.Context, key []byte) ([]byte, error)

    // Put 存储值
    Put(ctx context.Context, key, value []byte) error

    // Delete 删除值
    Delete(ctx context.Context, key []byte) error

    // Scan 扫描键值对
    Scan(ctx context.Context, prefix []byte) (Iterator, error)
}
```

### 6.2 数据库抽象设计优势

通过接口抽象设计数据库层，系统具有以下优势：

1. **灵活性**：根据不同场景选择合适的数据库
   - 开发/测试环境：使用SQLite（无需额外部署）
   - 小规模生产：使用MySQL/PostgreSQL
   - 大规模生产：使用ClickHouse/InfluxDB（时序数据优化）
   - 边缘场景：使用LevelDB（轻量级嵌入式）

2. **可扩展性**：轻松添加新的数据库支持，不影响业务逻辑

3. **可测试性**：在单元测试中使用SQLite，提高测试效率

4. **成本优化**：根据数据量和访问模式选择最具成本效益的方案

---

## 七、配置管理抽象设计

### 7.1 设计目标

为了统一管理各模块的配置，提供配置验证、配置版本管理等功能，需要对配置管理进行抽象设计。

**设计目标**：
1. **统一接口**：定义统一的配置管理接口
2. **模块化配置**：支持数据源、策略引擎、状态机、事件总线等模块的独立配置
3. **配置驱动**：通过配置文件控制工厂创建实例，减少硬编码
4. **配置验证**：提供配置验证机制，确保配置正确性
5. **解析器注册**：各模块注册自己的配置解析器，实现配置与模块的解耦

### 7.2 整体配置结构

配置管理采用分层结构，支持全局配置和模块级配置，所有配置集中在一份配置文件中：

```yaml
# 全局配置
global:
  log_level: "info"
  log_format: "json"
  
# 数据源配置
datasource:
  active: "nri"
  nri:
    socket_path: "/var/run/nri.sock"
  docker:
    host: "unix:///var/run/docker.sock"
      
# 策略引擎配置
policy:
  engine:
    type: "libdslgo"
    storage:
      type: "sqlite"
      dsn: "/data/policies.db"
      
# 事件总线配置
eventbus:
  type: "grpc"
  addr: "localhost:50051"
    
# 状态机配置
statemachine:
  config_file: "/etc/nuts/statemachine.yaml"
  
# 任务调度配置
scheduler:
  max_concurrent_tasks: 10
  task_timeout: 3600
  
# 数据库配置
database:
  event_db:
    type: "influxdb"
    dsn: "http://localhost:8086"
  audit_db:
    type: "postgresql"
    dsn: "postgres://user:pass@localhost/audit"
  diagnosis_db:
    type: "postgresql"
    dsn: "postgres://user:pass@localhost/diagnosis"

# 引擎配置
engines:
  collector:
    enabled: true
    eventbus:
      type: "grpc"
      address: "localhost:9090"
    config:
      interval: 60
  aggregation:
    enabled: true
    eventbus:
      type: "grpc"
      address: "localhost:9090"
    config:
      window_size: 300
  diagnostic:
    enabled: true
    eventbus:
      type: "grpc"
      address: "localhost:9090"
    config:
      strategy: "builtin"
```

### 7.3 接口设计

```go
// pkg/config/interface.go
package config

// Config 配置管理接口
type Config interface {
    // Get 获取配置值
    Get(key string) (interface{}, error)

    // Set 设置配置值
    Set(key string, value interface{}) error

    // Delete 删除配置值
    Delete(key string) error

    // GetModule 获取模块配置
    GetModule(module string) (map[string]interface{}, error)
    
    // Save 保存配置
    Save() error
    
    // Reload 重新加载配置文件
    Reload() error
}

// ConfigParser 配置解析器接口
type ConfigParser interface {
    // Parse 解析模块配置
    Parse(config map[string]interface{}) (interface{}, error)
    
    // Validate 验证模块配置
    Validate(config map[string]interface{}) error
}

// ConfigRegistry 配置解析器注册表
type ConfigRegistry struct {
    parsers map[string]ConfigParser
}

func NewConfigRegistry() *ConfigRegistry
func (r *ConfigRegistry) RegisterParser(module string, parser ConfigParser)
func (r *ConfigRegistry) GetParser(module string) (ConfigParser, error)
func (r *ConfigRegistry) ParseModule(module string, config map[string]interface{}) (interface{}, error)
```

### 7.4 配置加载实现

```go
// pkg/config/file.go
package config

import (
    "os"
    "sync"
    "gopkg.in/yaml.v3"
)

type FileConfig struct {
    data     map[string]interface{}
    filePath string
    mutex    sync.RWMutex
    registry *ConfigRegistry
}

func NewFileConfig(filePath string, registry *ConfigRegistry) (*FileConfig, error) {
    cfg := &FileConfig{
        filePath: filePath,
        data:     make(map[string]interface{}),
        registry: registry,
    }
    
    if err := cfg.Reload(); err != nil {
        return nil, err
    }
    
    return cfg, nil
}

func (c *FileConfig) Reload() error {
    c.mutex.Lock()
    defer c.mutex.Unlock()
    
    data, err := os.ReadFile(c.filePath)
    if err != nil {
        return err
    }
    
    var config map[string]interface{}
    if err := yaml.Unmarshal(data, &config); err != nil {
        return err
    }
    
    c.data = config
    return nil
}

func (c *FileConfig) Get(key string) (interface{}, error) {
    c.mutex.RLock()
    defer c.mutex.RUnlock()
    
    keys := strings.Split(key, ".")
    var current interface{} = c.data
    
    for _, k := range keys {
        if m, ok := current.(map[string]interface{}); ok {
            if val, exists := m[k]; exists {
                current = val
            } else {
                return nil, fmt.Errorf("key not found: %s", key)
            }
        } else {
            return nil, fmt.Errorf("invalid key path: %s", key)
        }
    }
    
    return current, nil
}

func (c *FileConfig) GetModule(module string) (map[string]interface{}, error) {
    val, err := c.Get(module)
    if err != nil {
        return nil, err
    }
    
    if m, ok := val.(map[string]interface{}); ok {
        return m, nil
    }
    
    return nil, fmt.Errorf("module config is not a map: %s", module)
}

func (c *FileConfig) ParseModule(module string) (interface{}, error) {
    config, err := c.GetModule(module)
    if err != nil {
        return nil, err
    }
    
    return c.registry.ParseModule(module, config)
}
```

### 7.5 模块配置解析器注册

各模块通过ConfigRegistry注册自己的配置解析器，实现配置与模块的解耦。

#### 7.5.1 数据源配置解析器

```go
// pkg/datasource/config.go
package datasource

type DataSourceConfig struct {
    Active  string
    Sources map[string]SourceConfig
}

type SourceConfig struct {
    SocketPath  string
    Host        string
}

type DataSourceConfigParser struct{}

func (p *DataSourceConfigParser) Parse(config map[string]interface{}) (interface{}, error) {
    var cfg DataSourceConfig
    if active, ok := config["active"].(string); ok {
        cfg.Active = active
    }
    
    if sources, ok := config["sources"].(map[string]interface{}); ok {
        cfg.Sources = make(map[string]SourceConfig)
        for name, src := range sources {
            if srcMap, ok := src.(map[string]interface{}); ok {
                srcCfg := SourceConfig{}
                if enabled, ok := srcMap["enabled"].(bool); ok {
                    srcCfg.Enabled = enabled
                }
                if socketPath, ok := srcMap["socket_path"].(string); ok {
                    srcCfg.SocketPath = socketPath
                }
                if host, ok := srcMap["host"].(string); ok {
                    srcCfg.Host = host
                }
                cfg.Sources[name] = srcCfg
            }
        }
    }
    
    return &cfg, nil
}

func (p *DataSourceConfigParser) Validate(config map[string]interface{}) error {
    if _, ok := config["active"]; !ok {
        return fmt.Errorf("datasource active is required")
    }
    return nil
}

// 注册配置解析器
func RegisterConfigParser(registry *config.ConfigRegistry) {
    registry.RegisterParser("datasource", &DataSourceConfigParser{})
}
```

#### 7.5.2 策略引擎配置解析器

```go
// pkg/policy/config.go
package policy

type PolicyEngineConfig struct {
    EngineType string
    Storage    StorageConfig
}

type StorageConfig struct {
    Type string
    DSN  string
}

type PolicyConfigParser struct{}

func (p *PolicyConfigParser) Parse(config map[string]interface{}) (interface{}, error) {
    var cfg PolicyEngineConfig
    // 解析逻辑...
    return &cfg, nil
}

func (p *PolicyConfigParser) Validate(config map[string]interface{}) error {
    // 验证逻辑...
    return nil
}

func RegisterConfigParser(registry *config.ConfigRegistry) {
    registry.RegisterParser("policy", &PolicyConfigParser{})
}
```

### 7.6 模块配置集成

各模块通过ConfigRegistry获取解析后的配置：

```go
// pkg/datasource/manager.go
package datasource

func NewDataSourceManager(cfg config.Config, eventBus eventbus.EventBus) (*DataSourceManager, error) {
    // 使用配置解析器获取解析后的配置
    parsedConfig, err := cfg.ParseModule("datasource")
    if err != nil {
        return nil, err
    }
    
    dsConfig := parsedConfig.(*DataSourceConfig)
    
    manager := &DataSourceManager{
        config:   dsConfig,
        eventBus: eventBus,
    }
    
    // 根据配置创建数据源
    for name, srcCfg := range dsConfig.Sources {
        if srcCfg.Enabled {
            source, err := factory.Create(name, map[string]interface{}{
                "socket_path": srcCfg.SocketPath,
                "host":        srcCfg.Host,
            })
            if err != nil {
                return nil, err
            }
            manager.SetActive(name, source)
        }
    }
    
    return manager, nil
}
```

### 7.7 配置初始化流程

```go
// cmd/service/main.go
package main

func main() {
    // 创建配置解析器注册表
    registry := config.NewConfigRegistry()
    
    // 注册各模块的配置解析器
    datasource.RegisterConfigParser(registry)
    policy.RegisterConfigParser(registry)
    statemachine.RegisterConfigParser(registry)
    eventbus.RegisterConfigParser(registry)
    
    // 加载配置文件
    cfg, err := config.NewFileConfig("/etc/nuts/config.yaml", registry)
    if err != nil {
        log.Fatal(err)
    }
    
    // 创建各模块
    eventBus, err := eventbus.NewFromConfig(cfg)
    if err != nil {
        log.Fatal(err)
    }
    
    dsManager, err := datasource.NewDataSourceManager(cfg, eventBus)
    if err != nil {
        log.Fatal(err)
    }
    
    // ...
}
```

---

## 八、监控和可观测性设计

### 8.1 设计目标

为了提供系统的可观测性，支持日志、指标、链路追踪等功能，需要对监控和可观测性进行抽象设计。

**设计目标**：
1. **统一接口**：定义统一的日志、指标、追踪接口，屏蔽底层实现差异
2. **结构化日志**：支持结构化日志输出，便于查询和分析
3. **指标采集**：支持Prometheus等标准指标格式
4. **分布式追踪**：支持OpenTelemetry等分布式追踪标准
5. **健康检查**：提供统一健康检查接口

### 8.2 日志接口设计

```go
// pkg/logger/interface.go
package logger

// Logger 日志接口
type Logger interface {
    // Debug 调试日志
    Debug(msg string, fields ...Field)
    
    // Info 信息日志
    Info(msg string, fields ...Field)
    
    // Warn 警告日志
    Warn(msg string, fields ...Field)
    
    // Error 错误日志
    Error(msg string, fields ...Field)
    
    // WithFields 添加字段
    WithFields(fields ...Field) Logger
}

// Field 日志字段
type Field struct {
    Key   string
    Value interface{}
}
```

### 8.3 指标接口设计

```go
// pkg/metrics/interface.go
package metrics

// Counter 计数器接口
type Counter interface {
    Inc()
    Add(delta float64)
}

// Gauge 仪表盘接口
type Gauge interface {
    Set(value float64)
    Inc()
    Dec()
    Add(delta float64)
    Sub(delta float64)
}

// Histogram 直方图接口
type Histogram interface {
    Observe(value float64)
}

// Metrics 指标接口
type Metrics interface {
    NewCounter(name string, labels ...string) Counter
    NewGauge(name string, labels ...string) Gauge
    NewHistogram(name string, labels ...string) Histogram
}
```

### 8.4 链路追踪接口设计

```go
// pkg/tracer/interface.go
package tracer

import "context"

// Tracer 追踪器接口
type Tracer interface {
    // StartSpan 开始一个span
    StartSpan(ctx context.Context, name string) (context.Context, Span)
}

// Span 接口
type Span interface {
    // End 结束span
    End()
    
    // SetTag 设置标签
    SetTag(key string, value interface{})
    
    // SetError 设置错误
    SetError(err error)
}
```

---

## 九、安全设计

### 9.1 设计目标

为了保障系统的安全性，需要对认证、授权、加密等安全机制进行抽象设计。

**设计目标**：
1. **认证接口**：定义统一的认证接口，支持多种认证方式
2. **授权接口**：定义统一的授权接口，支持RBAC等授权模型
3. **加密接口**：定义统一的加密接口，支持多种加密算法
4. **安全策略**：提供统一的安全策略配置
5. **审计日志**：记录所有安全相关操作

### 9.2 认证接口设计

```go
// pkg/auth/interface.go
package auth

import "context"

// Authenticator 认证器接口
type Authenticator interface {
    // Authenticate 执行认证
    Authenticate(ctx context.Context, credentials Credentials) (Token, error)
    
    // Validate 验证token
    Validate(ctx context.Context, token string) (Claims, error)
}

// Credentials 认证凭证
type Credentials struct {
    Username string
    Password string
    APIKey   string
}

// Token 认证令牌
type Token struct {
    AccessToken string
    ExpiresAt   time.Time
}

// Claims 认证声明
type Claims struct {
    Subject   string
    IssuedAt  time.Time
    ExpiresAt time.Time
}
```

### 9.3 授权接口设计

```go
// pkg/authz/interface.go
package authz

import "context"

// Authorizer 授权器接口
type Authorizer interface {
    // Authorize 执行授权检查
    Authorize(ctx context.Context, subject, resource, action string) bool
    
    // AddPolicy 添加授权策略
    AddPolicy(policy Policy) error
    
    // RemovePolicy 移除授权策略
    RemovePolicy(policy Policy) error
}

// Policy 授权策略
type Policy struct {
    Subject  string
    Resource string
    Action   string
    Effect   string // allow/deny
}
```

### 9.4 加密接口设计

```go
// pkg/crypto/interface.go
package crypto

// Encryptor 加密器接口
type Encryptor interface {
    // Encrypt 加密
    Encrypt(plaintext []byte) ([]byte, error)
    
    // Decrypt 解密
    Decrypt(ciphertext []byte) ([]byte, error)
}

// Hasher 哈希器接口
type Hasher interface {
    // Hash 计算哈希
    Hash(data []byte) ([]byte, error)
    
    // Verify 验证哈希
    Verify(data, hash []byte) bool
}
```

具体实现请参考plugin.md文档。

---

## 十、测试设计

### 10.1 设计目标

为了确保框架的质量和稳定性，需要对测试策略进行设计。

**设计目标**：
1. **单元测试**：对每个模块进行单元测试，确保代码质量
2. **集成测试**：对模块间的集成进行测试，确保接口正确性
3. **性能测试**：对关键路径进行性能测试，确保性能指标达标
4. **Mock接口**：提供Mock接口，便于单元测试
5. **测试覆盖率**：确保测试覆盖率 > 80%

### 10.2 单元测试指导

**测试原则**：
1. **独立性**：每个测试用例独立运行，不依赖其他测试
2. **可重复性**：测试结果可重复，不依赖外部环境
3. **快速执行**：单元测试应该快速执行
4. **清晰断言**：断言应该清晰明确

**测试重点**：
- 接口实现测试
- 工厂模式测试
- 状态机转换测试
- 事件总线通信测试

### 10.3 集成测试指导

**测试场景**：
1. **数据源集成**：测试数据源与PolicyEngine的集成
2. **策略引擎集成**：测试PolicyEngine与TaskScheduler的集成
3. **事件总线集成**：测试EventBus与各模块的集成
4. **数据库集成**：测试各模块与数据库的集成

### 10.4 Mock接口设计

```go
// pkg/mock/eventbus.go
package mock

import "github.com/nuts-project/nuts/pkg/eventbus"

// MockEventBus Mock事件总线
type MockEventBus struct {
    PublishedEvents []eventbus.Event
    Subscribers     map[string][]chan eventbus.Event
}

func NewMockEventBus() *MockEventBus
func (m *MockEventBus) Publish(event eventbus.Event) error
func (m *MockEventBus) Subscribe(topic string) (<-chan eventbus.Event, error)
func (m *MockEventBus) Unsubscribe(topic string, ch <-chan eventbus.Event) error
func (m *MockEventBus) Close() error
```

具体实现请参考plugin.md文档。

---

## 十一、版本兼容性设计

### 11.1 设计目标

为了支持框架的演进和升级，需要对版本兼容性进行设计。

**设计目标**：
1. **接口版本管理**：为接口定义版本号，支持多版本共存
2. **向后兼容**：新版本保持对旧版本的兼容性
3. **平滑升级**：支持平滑升级，无需停机
4. **弃用策略**：明确接口弃用策略，提供迁移指导
5. **兼容性测试**：提供兼容性测试工具

### 11.2 接口版本管理

```go
// pkg/version/interface.go
package version

// Version 版本信息
type Version struct {
    Major int
    Minor int
    Patch int
}

// String 返回版本字符串
func (v *Version) String() string

// Compare 比较版本
func (v *Version) Compare(other *Version) int

// Compatible 检查兼容性
func (v *Version) Compatible(other *Version) bool
```

### 11.3 向后兼容策略

**兼容性规则**：
1. **Major版本**：不兼容的API修改
2. **Minor版本**：向后兼容的功能新增
3. **Patch版本**：向后兼容的问题修复

**兼容性保证**：
- 新增接口不影响旧接口
- 新增字段不影响旧实现
- 删除功能需要经历弃用期

具体实现请参考plugin.md文档。

---

## 十二、资源管理设计

### 12.1 设计目标

为了有效管理系统资源，防止资源耗尽，需要对资源管理进行抽象设计。

**设计目标**：
1. **资源限制**：对CPU、内存、磁盘等资源进行限制
2. **配额管理**：支持配额管理，防止过度使用
3. **资源回收**：支持资源自动回收机制
4. **资源监控**：实时监控资源使用情况
5. **资源调度**：基于资源使用情况进行智能调度

### 12.2 资源限制接口设计

```go
// pkg/resource/interface.go
package resource

// Limiter 资源限制器接口
type Limiter interface {
    // LimitCPU 限制CPU使用
    LimitCPU(limit float64) error
    
    // LimitMemory 限制内存使用
    LimitMemory(limit int64) error
    
    // LimitDisk 限制磁盘使用
    LimitDisk(limit int64) error
    
    // GetUsage 获取资源使用情况
    GetUsage() (*Usage, error)
}

// Usage 资源使用情况
type Usage struct {
    CPU     float64
    Memory  int64
    Disk    int64
    Network int64
}
```

### 12.3 配额管理接口设计

```go
// pkg/quota/interface.go
package quota

// Manager 配额管理器接口
type Manager interface {
    // SetQuota 设置配额
    SetQuota(subject string, quota *Quota) error
    
    // GetQuota 获取配额
    GetQuota(subject string) (*Quota, error)
    
    // CheckQuota 检查配额
    CheckQuota(subject string, resource string, amount int64) bool
    
    // UseQuota 使用配额
    UseQuota(subject string, resource string, amount int64) error
    
    // ReleaseQuota 释放配额
    ReleaseQuota(subject string, resource string, amount int64) error
}

// Quota 配额
type Quota struct {
    CPU    float64
    Memory int64
    Disk   int64
}
```

具体实现请参考plugin.md文档。

---

## 十三、分布式系统设计

### 13.1 限流机制设计

#### 13.1.1 设计目标

为了防止分布式系统中的雪崩效应，需要对请求进行限流控制。

**设计目标**：
1. **统一接口**：定义统一的限流器接口
2. **多种算法**：支持令牌桶、漏桶、滑动窗口等算法
3. **分布式支持**：支持Redis等分布式限流
4. **动态配置**：支持运行时调整限流参数

#### 13.1.2 接口设计

```go
// pkg/ratelimit/interface.go
package ratelimit

import "time"

// RateLimiter 限流器接口
type RateLimiter interface {
    // Allow 检查是否允许通过
    Allow() bool
    
    // Wait 等待直到允许通过
    Wait(ctx context.Context) error
    
    // Limit 获取限流配置
    Limit() Rate
}

// Rate 限流配置
type Rate struct {
    Limit int64         // 限制数量
    Window time.Duration // 时间窗口
}
```

### 13.2 熔断机制设计

#### 13.2.1 设计目标

为了防止分布式系统中的级联故障，需要实现熔断机制。

**设计目标**：
1. **统一接口**：定义统一的熔断器接口
2. **状态管理**：支持关闭、开启、半开三种状态
3. **自动恢复**：支持自动从开启状态恢复到半开状态
4. **可配置**：支持配置失败阈值、恢复时间等参数

#### 13.2.2 接口设计

```go
// pkg/circuitbreaker/interface.go
package circuitbreaker

// CircuitBreaker 熔断器接口
type CircuitBreaker interface {
    // Execute 执行操作
    Execute(fn func() error) error
    
    // State 获取熔断器状态
    State() State
    
    // Reset 重置熔断器
    Reset() error
}

// State 熔断器状态
type State int

const (
    StateClosed State = iota
    StateOpen
    StateHalfOpen
)
```

### 13.3 服务发现机制设计

#### 13.3.1 设计目标

为了支持分布式部署，需要实现服务发现机制。

**设计目标**：
1. **统一接口**：定义统一的服务发现接口
2. **多实现支持**：支持Consul、Etcd、Kubernetes等
3. **健康检查**：支持服务健康检查
4. **动态更新**：支持服务列表动态更新

#### 13.3.2 接口设计

```go
// pkg/discovery/interface.go
package discovery

// ServiceDiscovery 服务发现接口
type ServiceDiscovery interface {
    // Register 注册服务
    Register(service *Service) error
    
    // Deregister 注销服务
    Deregister(serviceID string) error
    
    // Discover 发现服务
    Discover(serviceName string) ([]*Service, error)
    
    // Watch 监控服务变化
    Watch(serviceName string) (<-chan []*Service, error)
}

// Service 服务定义
type Service struct {
    ID      string
    Name    string
    Address string
    Port    int
    Metadata map[string]string
}
```

### 13.4 上下文传递机制设计

#### 13.4.1 设计目标

为了支持分布式追踪、超时控制等功能，需要统一的上下文传递机制。

**设计目标**：
1. **统一接口**：定义统一的上下文接口
2. **值传递**：支持键值对传递
3. **超时控制**：支持超时和取消
4. **链路追踪**：支持TraceID传递

#### 13.4.2 接口设计

```go
// pkg/context/interface.go
package context

import "time"

// Context 上下文接口
type Context interface {
    // WithValue 设置值
    WithValue(key string, value interface{}) Context
    
    // Value 获取值
    Value(key string) interface{}
    
    // WithTimeout 设置超时
    WithTimeout(timeout time.Duration) (Context, CancelFunc)
    
    // WithCancel 可取消
    WithCancel() (Context, CancelFunc)
    
    // Deadline 获取截止时间
    Deadline() (time.Time, bool)
    
    // Done 获取完成通道
    Done() <-chan struct{}
    
    // Err 获取错误
    Err() error
}

// CancelFunc 取消函数
type CancelFunc func()
```

### 13.5 缓存机制设计

#### 13.5.1 设计目标

为了提高系统性能，需要缓存机制减少重复计算和数据库访问。

**设计目标**：
1. **统一接口**：定义统一的缓存接口
2. **多实现支持**：支持内存缓存、Redis等
3. **过期策略**：支持TTL过期策略
4. **缓存淘汰**：支持LRU等淘汰策略

#### 13.5.2 接口设计

```go
// pkg/cache/interface.go
package cache

import "time"

// Cache 缓存接口
type Cache interface {
    // Get 获取值
    Get(key string) (interface{}, error)
    
    // Set 设置值
    Set(key string, value interface{}, ttl time.Duration) error
    
    // Delete 删除值
    Delete(key string) error
    
    // Clear 清空缓存
    Clear() error
    
    // Exists 检查键是否存在
    Exists(key string) (bool, error)
}
```

---

## 十四、目录结构设计

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
│   ├── datasource/               # 数据源库
│   │   ├── interface.go          # 数据源接口定义
│   │   ├── manager.go            # 数据源管理器
│   │   ├── event.go              # 事件处理
│   │   ├── nri/                  # NRI数据源实现
│   │   ├── docker/               # Docker SDK数据源实现
│   │   └── containerd/           # containerd SDK数据源实现
│   ├── policy/                   # 策略引擎库
│   │   ├── interface.go          # 策略接口定义
│   │   ├── matcher.go            # PolicyMatcher实现
│   │   ├── receiver.go           # PolicyReceiver实现
│   │   └── manager.go            # PolicyManager实现
│   ├── scheduler/                 # 任务调度库
│   │   ├── interface.go          # 调度器接口定义
│   │   ├── scheduler.go          # TaskScheduler实现
│   │   ├── state_machine.go      # 状态机实现
│   │   └── store.go              # TaskStore实现
│   ├── storage/                  # 数据库抽象层
│   │   ├── interface.go          # 数据库接口定义
│   │   ├── policy/               # 策略存储实现
│   │   ├── event/                # 事件存储实现
│   │   └── kv/                   # 键值存储实现
│   └── eventbus/                 # 事件总线
│       ├── interface.go          # 事件总线接口
│       ├── factory.go            # 工厂注册
│       ├── grpc/                 # gRPC实现（默认）
│       ├── redis/                # Redis实现
│       └── kafka/                # Kafka实现
├── internal/                     # 内部实现
│   ├── service/                  # Service内部实现
│   │   ├── server.go             # HTTP服务器
│   │   ├── handler.go            # API处理器
│   │   └── config.go             # 配置管理
│   └── config/                   # 配置
│       └── config.go             # 配置结构
├── api/                          # API定义
│   ├── proto/                    # gRPC proto文件
│   └── openapi/                  # OpenAPI文档
├── configs/                      # 配置文件
│   ├── service.yaml              # Service配置
│   └── collector.yaml            # Collector配置
├── deployments/                  # 部署文件
│   ├── service.yaml              # Service部署
│   └── collector.yaml            # Collector部署
├── docs/                         # 文档
│   ├── framework.md             # 通用框架设计文档
│   └── plugin.md                # 插件实例设计文档
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

---

## 十五、错误处理

### 15.1 关键错误和处理策略

| 错误编号 | 错误名称 | 处理方式 |
|---------|---------|---------|
| ERR-001 | 数据源连接失败 | 重试连接（最大3次），记录日志，告警 |
| ERR-002 | 事件监听失败 | 重新注册，记录日志 |
| ERR-003 | 策略解析失败 | 返回400错误，提示具体错误 |
| ERR-004 | 策略存储失败 | 重试（最大3次），记录日志，告警 |
| ERR-005 | 状态转换失败 | 记录日志，任务标记为失败状态 |
| ERR-006 | 数据库连接失败 | 重试（最大5次），降级到备用数据库 |
| ERR-007 | 事件总线发布失败 | 缓存事件，重试（最大3次） |

### 15.2 错误处理原则

1. **优雅降级**：关键组件失败时，系统应降级到可用状态
2. **重试机制**：对临时性错误实施指数退避重试
3. **错误隔离**：单个任务失败不应影响其他任务
4. **完整日志**：记录详细的错误信息用于排查
5. **告警机制**：关键错误触发告警通知

### 15.3 统一错误接口设计

```go
// pkg/errors/interface.go
package errors

// Error 错误接口
type Error interface {
    error
    
    // Code 错误码
    Code() string
    
    // Type 错误类型
    Type() ErrorType
    
    // Details 错误详情
    Details() map[string]interface{}
    
    // Cause 原因错误
    Cause() error
}

// ErrorType 错误类型
type ErrorType int

const (
    ErrorTypeValidation ErrorType = iota
    ErrorTypeNotFound
    ErrorTypeConflict
    ErrorTypeInternal
    ErrorTypeExternal
)

// NewError 创建错误
func NewError(code string, msg string, errType ErrorType) Error
func WrapError(err error, code string, msg string) Error
```

---

## 十六、后续优化方向

### 16.1 框架优化

1. **性能优化**
   - 优化事件总线吞吐量
   - 减少状态机锁竞争
   - 优化数据库查询性能

2. **可观测性增强**
   - 添加Prometheus指标采集
   - 实现分布式链路追踪
   - 增强日志结构化输出

3. **高可用性**
   - 实现Leader选举机制
   - 支持故障自动转移
   - 增加健康检查机制

4. **可扩展性**
   - 支持水平扩展
   - 实现负载均衡策略
   - 优化资源利用率

### 16.2 功能扩展

1. **多租户支持**
   - 实现租户隔离
   - 支持配额管理
   - 增加审计追踪

2. **插件生态**
   - 开发插件市场
   - 提供插件开发SDK
   - 建立插件认证机制

3. **AI增强**
   - 集成更多AI模型
   - 优化诊断准确性
   - 实现预测性分析

---

**文档结束**

本框架设计文档描述了NUTS项目的通用框架部分，包括数据源抽象、策略引擎抽象、任务调度、数据库抽象等核心组件。框架设计遵循通用化、可扩展、可配置的原则，为具体的业务插件提供稳定可靠的基础设施。
