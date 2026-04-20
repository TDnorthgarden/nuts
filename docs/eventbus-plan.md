# 事件总线开发计划

## 目标

为任务调度模块添加多驱动源支持，通过事件总线模式统一管理任务状态转换的驱动源，同时预留消息队列接口以支持未来扩展到分布式部署。

## 设计原则

1. **接口优先**：优先定义接口，实现可替换
2. **扩展性**：预留多种事件总线实现接口
3. **向后兼容**：不影响现有单机部署
4. **平滑过渡**：从单机到分布式只需修改配置

## 接口设计

### 1. 事件总线接口（核心抽象）

**位置**：`pkg/eventbus/interface.go`

**接口定义**：
- `EventBus`：事件总线核心接口
  - `Publish(event *TaskEvent) error`：发布事件
  - `Subscribe(handler EventHandler) error`：订阅事件
  - `Start() error`：启动事件总线
  - `Stop() error`：停止事件总线
  - `Type() EventBusType`：返回总线类型

- `EventBusType`：事件总线类型枚举
  - `EventBusTypeMemory`：内存通道（当前实现）
  - `EventBusTypeRedis`：Redis Pub/Sub（预留）
  - `EventBusTypeNSQ`：NSQ 消息队列（预留）
  - `EventBusTypeKafka`：Kafka 消息队列（预留）
  - `EventBusTypeHybrid`：混合模式（预留）

**扩展性考虑**：
- 接口方法设计简洁，易于实现
- 类型枚举可扩展，添加新类型不影响现有代码
- 错误处理统一，便于日志和监控

### 2. 事件定义

**位置**：`pkg/eventbus/event.go`

**事件类型**：
- 采集事件：`CollectionStart`, `CollectionComplete`, `CollectionFailed`, `CollectionTimeout`
- 聚合事件：`AggregationStart`, `AggregationComplete`, `AggregationFailed`
- 诊断事件：`DiagnosisStart`, `DiagnosisComplete`, `DiagnosisFailed`
- 任务事件：`TaskCreated`, `TaskCompleted`, `TaskFailed`, `TaskStopped`

**事件结构**：
- `TaskEvent`：通用事件结构
  - 必需字段：ID, Type, TaskID, Timestamp
  - 可选字段：CgroupID, PolicyID, Data, Error
  - 扩展字段：Metadata（预留，用于未来扩展）

**扩展性考虑**：
- Data 字段使用 `map[string]interface{}`，支持任意数据
- Metadata 字段预留，用于自定义扩展
- 事件类型使用字符串枚举，易于添加新类型

### 3. 事件处理器接口

**位置**：`pkg/eventbus/handler.go`

**接口定义**：
- `EventHandler`：事件处理器函数类型
  - `func(event *TaskEvent) error`

**扩展性考虑**：
- 函数类型，支持匿名函数和方法
- 错误返回，便于错误处理和重试
- 可组合，支持链式处理

### 4. 消息队列接口（预留）

**位置**：`pkg/eventbus/mq_interface.go`

**接口定义**：
- `MQEventBus`：消息队列事件总线接口（继承 EventBus）
  - `Configure(config map[string]interface{}) error`：配置消息队列
  - `HealthCheck() error`：健康检查

**扩展性考虑**：
- 独立接口，不影响内存实现
- 配置使用 map，支持任意配置项
- 健康检查接口，便于监控

### 5. 事件总线工厂接口

**位置**：`pkg/eventbus/factory.go`

**接口定义**：
- `EventBusFactory`：事件总线工厂
  - `Create(config *EventBusConfig) (EventBus, error)`：创建事件总线

**配置结构**：
- `EventBusConfig`：事件总线配置
  - Type：总线类型
  - Memory：内存配置
  - Redis：Redis 配置
  - NSQ：NSQ 配置
  - Kafka：Kafka 配置

**扩展性考虑**：
- 工厂模式，隐藏实现细节
- 配置结构可扩展，添加新类型只需添加配置字段
- 默认配置，简化使用

### 6. 定时器管理器接口

**位置**：`pkg/timer/interface.go`

**接口定义**：
- `TimerManager`：定时器管理器接口
  - `StartTimer(taskID string, duration time.Duration) error`：启动定时器
  - `StopTimer(taskID string) error`：停止定时器
  - `ResetTimer(taskID string, duration time.Duration) error`：重置定时器
  - `Start() error`：启动管理器
  - `Stop() error`：停止管理器

**扩展性考虑**：
- 接口抽象，支持不同实现
- 支持重置，便于采集时长调整
- 生命周期管理，避免资源泄漏

### 7. 任务调度器接口扩展

**位置**：`pkg/scheduler/interface.go`（新建）

**接口定义**：
- `TaskScheduler`：任务调度器接口（扩展现有）
  - `PublishEvent(event *TaskEvent) error`：发布事件（新增）
  - `SubscribeToEvents(handler EventHandler) error`：订阅事件（新增）

**扩展性考虑**：
- 在现有接口基础上扩展，保持兼容
- 发布/订阅模式，解耦各模块
- 错误处理统一

## 实现计划

### 阶段一：接口定义（Week 1）

#### Task 1.1：事件总线接口定义
- 文件：`pkg/eventbus/interface.go`
- 内容：
  - 定义 `EventBus` 接口
  - 定义 `EventBusType` 枚举
  - 定义 `MQEventBus` 接口（预留）
- 验收标准：
  - 接口方法签名清晰
  - 注释完整
  - 单元测试通过

#### Task 1.2：事件定义
- 文件：`pkg/eventbus/event.go`
- 内容：
  - 定义 `EventType` 枚举
  - 定义 `TaskEvent` 结构
  - 定义事件构造函数
- 验收标准：
  - 所有事件类型定义完整
  - 结构体字段注释完整
  - JSON 序列化测试通过

#### Task 1.3：事件处理器接口
- 文件：`pkg/eventbus/handler.go`
- 内容：
  - 定义 `EventHandler` 类型
  - 定义常用处理器（日志处理器、错误处理器）
- 验收标准：
  - 处理器类型定义正确
  - 示例处理器可用

#### Task 1.4：定时器管理器接口
- 文件：`pkg/timer/interface.go`
- 内容：
  - 定义 `TimerManager` 接口
  - 定义 `TimerEvent` 结构
- 验收标准：
  - 接口方法完整
  - 注释清晰

#### Task 1.5：事件总线工厂接口
- 文件：`pkg/eventbus/factory.go`
- 内容：
  - 定义 `EventBusFactory` 接口
  - 定义 `EventBusConfig` 结构
  - 定义各类型配置结构
- 验收标准：
  - 配置结构完整
  - 支持所有预留类型

#### Task 1.6：任务调度器接口扩展
- 文件：`pkg/scheduler/interface.go`（新建）
- 内容：
  - 扩展现有任务调度器接口
  - 添加事件发布/订阅方法
- 验收标准：
  - 接口扩展正确
  - 保持向后兼容

### 阶段二：内存事件总线实现（Week 2）

#### Task 2.1：内存事件总线实现
- 文件：`pkg/eventbus/memory.go`
- 内容：
  - 实现 `EventBus` 接口
  - 使用 Go channel
  - 支持并发安全
- 验收标准：
  - 接口实现完整
  - 并发测试通过
  - 性能测试通过

#### Task 2.2：内存事件总线测试
- 文件：`pkg/eventbus/memory_test.go`
- 内容：
  - 单元测试
  - 并发测试
  - 性能基准测试
- 验收标准：
  - 测试覆盖率 > 90%
  - 无竞态条件
  - 性能满足要求

#### Task 2.3：事件总线工厂实现
- 文件：`pkg/eventbus/factory_impl.go`
- 内容：
  - 实现 `EventBusFactory` 接口
  - 支持创建内存事件总线
  - 配置解析和验证
- 验收标准：
  - 工厂实现正确
  - 配置验证完整
  - 错误处理完善

### 阶段三：定时器管理器实现（Week 3）

#### Task 3.1：定时器管理器实现
- 文件：`pkg/timer/manager.go`
- 内容：
  - 实现 `TimerManager` 接口
  - 管理多个定时器
  - 支持定时器清理
- 验收标准：
  - 接口实现完整
  - 定时器管理正确
  - 无资源泄漏

#### Task 3.2：定时器管理器测试
- 文件：`pkg/timer/manager_test.go`
- 内容：
  - 单元测试
  - 并发测试
  - 资源泄漏测试
- 验收标准：
  - 测试覆盖率 > 90%
  - 无内存泄漏
  - 定时准确

#### Task 3.3：定时器与事件总线集成
- 文件：`pkg/timer/manager.go`（扩展）
- 内容：
  - 定时器到期发布事件
  - 集成事件总线
- 验收标准：
  - 事件发布正确
  - 集成测试通过

### 阶段四：任务调度器集成（Week 4）

#### Task 4.1：任务调度器重构
- 文件：`pkg/scheduler/scheduler.go`（新建）
- 内容：
  - 集成事件总线
  - 集成定时器管理器
  - 实现事件处理器
- 验收标准：
  - 集成正确
  - 事件处理逻辑完整
  - 错误处理完善

#### Task 4.2：任务调度器测试
- 文件：`pkg/scheduler/scheduler_test.go`
- 内容：
  - 单元测试
  - 集成测试
  - 端到端测试
- 验收标准：
  - 测试覆盖率 > 85%
  - 状态转换正确
  - 错误场景覆盖

#### Task 4.3：Service 层集成
- 文件：`internal/service/service.go`（修改）
- 内容：
  - 使用新的任务调度器
  - 配置事件总线
  - 配置定时器管理器
- 验收标准：
  - 集成正确
  - 配置加载正确
  - 启动流程正确

### 阶段五：各引擎模块集成（Week 5-6）

#### Task 5.1：采集器集成
- 文件：`pkg/collector/client.go`（修改/新建）
- 内容：
  - 集成事件发布
  - 发布采集开始/完成/失败事件
- 验收标准：
  - 事件发布正确
  - 集成测试通过

#### Task 5.2：聚合引擎集成
- 文件：`pkg/aggregation/engine.go`（修改/新建）
- 内容：
  - 集成事件发布
  - 发布聚合开始/完成/失败事件
- 验收标准：
  - 事件发布正确
  - 集成测试通过

#### Task 5.3：诊断引擎集成
- 文件：`pkg/diagnostic/engine.go`（修改/新建）
- 内容：
  - 集成事件发布
  - 发布诊断开始/完成/失败事件
- 验收标准：
  - 事件发布正确
  - 集成测试通过

#### Task 5.4：PolicyNotifier 重构
- 文件：`pkg/policy/interface.go`（修改）
- 内容：
  - 使用事件总线替代直接回调
  - 发布通知事件
- 验收标准：
  - 向后兼容
  - 事件发布正确

### 阶段六：配置和文档（Week 7）

#### Task 6.1：配置文件
- 文件：`configs/scheduler.yaml`（新建）
- 内容：
  - 事件总线配置
  - 定时器配置
  - 任务调度器配置
- 验收标准：
  - 配置结构正确
  - 支持所有配置项
  - 配置验证完整

#### Task 6.2：配置加载
- 文件：`internal/config/config.go`（扩展）
- 内容：
  - 加载事件总线配置
  - 加载定时器配置
  - 配置验证
- 验收标准：
  - 配置加载正确
  - 错误处理完善
  - 默认配置合理

#### Task 6.3：文档更新
- 文件：`docs/eventbus-implementation.md`（新建）
- 内容：
  - 接口文档
  - 使用指南
  - 扩展指南
- 验收标准：
  - 文档完整
  - 示例清晰
  - 扩展说明详细

#### Task 6.4：API 文档更新
- 文件：`docs/api.md`（更新）
- 内容：
  - 添加事件相关 API
  - 更新任务 API
- 验收标准：
  - API 文档完整
  - 示例正确

### 阶段七：测试和优化（Week 8）

#### Task 7.1：集成测试
- 文件：`tests/integration/eventbus_test.go`（新建）
- 内容：
  - 端到端测试
  - 场景测试
  - 性能测试
- 验收标准：
  - 测试覆盖率 > 80%
  - 性能满足要求
  - 稳定性测试通过

#### Task 7.2：压力测试
- 文件：`tests/stress/eventbus_stress_test.go`（新建）
- 内容：
  - 高并发测试
  - 长时间运行测试
  - 资源使用测试
- 验收标准：
  - 无内存泄漏
  - 性能稳定
  - 资源使用合理

#### Task 7.3：优化和调整
- 内容：
  - 性能优化
  - 错误处理优化
  - 日志优化
- 验收标准：
  - 性能提升 > 20%
  - 错误信息清晰
  - 日志合理

## 目录结构

```
pkg/
├── eventbus/              # 事件总线模块
│   ├── interface.go       # 接口定义
│   ├── event.go           # 事件定义
│   ├── handler.go         # 事件处理器
│   ├── mq_interface.go    # 消息队列接口（预留）
│   ├── factory.go         # 工厂接口
│   ├── factory_impl.go    # 工厂实现
│   ├── memory.go          # 内存事件总线实现
│   ├── memory_test.go     # 内存事件总线测试
│   ├── redis.go           # Redis 事件总线（预留）
│   ├── nsq.go             # NSQ 事件总线（预留）
│   └── kafka.go           # Kafka 事件总线（预留）
├── timer/                 # 定时器管理器模块
│   ├── interface.go       # 接口定义
│   ├── manager.go         # 管理器实现
│   └── manager_test.go    # 管理器测试
├── scheduler/             # 任务调度器模块（新建）
│   ├── interface.go       # 接口定义
│   ├── scheduler.go       # 调度器实现
│   └── scheduler_test.go  # 调度器测试
└── ...
```

## 配置示例

```yaml
# configs/scheduler.yaml
event_bus:
  type: "memory"  # 当前使用内存
  memory:
    buffer_size: 1000
  # 未来切换到 Redis:
  # type: "redis"
  # redis:
  #   addr: "localhost:6379"
  #   channel: "nuts:events"
  #   password: ""
  #   db: 0

timer:
  # 定时器配置
  default_timeout: 300s  # 默认超时时间
  cleanup_interval: 1m    # 清理间隔

scheduler:
  # 任务调度器配置
  poll_interval: 1s       # 轮询间隔（如果使用轮询模式）
  max_concurrent_tasks: 1000  # 最大并发任务数
```

## 扩展指南

### 添加新的事件总线实现

1. 在 `pkg/eventbus/` 下创建新文件（如 `myqueue.go`）
2. 实现 `EventBus` 接口（如需高级功能，实现 `MQEventBus` 接口）
3. 在 `EventBusType` 枚举中添加新类型
4. 在 `EventBusConfig` 中添加配置结构
5. 在 `EventBusFactory.Create()` 中添加创建逻辑
6. 编写测试和文档

### 添加新的事件类型

1. 在 `EventType` 枚举中添加新类型
2. 在事件构造函数中添加支持
3. 在任务调度器的事件处理器中添加处理逻辑
4. 更新文档和测试

## 验收标准

### 功能验收
- [ ] 所有接口定义完整
- [ ] 内存事件总线实现正确
- [ ] 定时器管理器实现正确
- [ ] 任务调度器集成正确
- [ ] 各引擎模块集成正确
- [ ] 配置加载正确

### 性能验收
- [ ] 事件发布延迟 < 1ms（内存模式）
- [ ] 支持并发 > 10000 事件/秒
- [ ] 无内存泄漏
- [ ] CPU 使用率合理

### 质量验收
- [ ] 单元测试覆盖率 > 90%
- [ ] 集成测试覆盖率 > 80%
- [ ] 所有测试通过
- [ ] 代码审查通过
- [ ] 文档完整

### 扩展性验收
- [ ] 接口设计清晰
- [ ] 预留接口完整
- [ ] 配置结构可扩展
- [ ] 扩展指南完整

## 风险和缓解

### 风险1：接口设计不够灵活
**缓解**：
- 充分讨论接口设计
- 参考业界最佳实践
- 预留扩展字段

### 风险2：性能不满足要求
**缓解**：
- 早期性能测试
- 使用 channel 优化
- 考虑批处理

### 风险3：集成复杂度高
**缓解**：
- 分阶段集成
- 充分测试
- 保持向后兼容

### 风险4：分布式扩展困难
**缓解**：
- 预留接口
- 提供扩展指南
- 编写示例代码

## 时间安排

- **Week 1**：接口定义
- **Week 2**：内存事件总线实现
- **Week 3**：定时器管理器实现
- **Week 4**：任务调度器集成
- **Week 5-6**：各引擎模块集成
- **Week 7**：配置和文档
- **Week 8**：测试和优化

总计：8周
