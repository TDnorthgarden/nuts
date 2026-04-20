# NUTS 故障分析插件实例设计文档

## 文档说明

本文档是 NUTS 项目的故障分析插件实例设计文档，描述基于通用框架实现的故障分析插件的具体设计，包括采集器、聚合引擎、诊断引擎等业务相关组件。

---

## 一、数据源具体实现

### 1.1 NRI数据源实现

```go
// pkg/datasource/nri/nri.go
package nri

import (
    "time"
    "github.com/containerd/nri/pkg/api"
)

type NRIDataSource struct {
    name     string
    status   DataSourceStatus
    eventCh  chan Event
    nriPlugin *api.Plugin
    config   *NRIConfig
}

func NewNRIDataSource(config *NRIConfig) (*NRIDataSource, error) {
    return &NRIDataSource{
        name:    "nri",
        status:  StatusStopped,
        eventCh: make(chan Event, 100),
        config:  config,
    }, nil
}

func (ds *NRIDataSource) Start() error {
    // 初始化NRI插件
    plugin, err := api.NewPlugin(api.Config{
        Name:    "nuts-datasource",
        Version: "0.1.0",
    })
    if err != nil {
        return err
    }

    // 注册事件处理器，在内部转换为统一格式
    plugin.OnStart(func(r *api.RunPodSandboxRequest) {
        event := Event{
            Type:      "CreateContainer",
            Timestamp: time.Now(),
            Metadata: map[string]interface{}{
                "container_id":  r.Config.PodSandboxId,
                "pod_id":        r.Config.PodSandboxId,
                "pod_name":      r.Config.PodName,
                "namespace":     r.Config.PodNamespace,
                "cgroup_id":     ds.fillCgroup(r.Pid),
                "pid":           r.Pid,
                "raw_data":      r,
            },
        }
        ds.eventCh <- event
    })

    ds.nriPlugin = plugin
    ds.status = StatusRunning
    return nil
}

func (ds *NRIDataSource) fillCgroup(pid int) string {
    // 根据PID填充cgroup ID
    return fmt.Sprintf("/proc/%d/cgroup", pid)
}
```

### 1.2 Docker SDK数据源实现

```go
// pkg/datasource/docker/docker.go
package docker

import (
    "context"
    "fmt"
    "time"
    "github.com/docker/docker/api/types"
    "github.com/docker/docker/client"
)

type DockerDataSource struct {
    name     string
    status   DataSourceStatus
    eventCh  chan Event
    client   *client.Client
    config   *DockerConfig
}

func NewDockerDataSource(config *DockerConfig) (*DockerDataSource, error) {
    cli, err := client.NewClientWithOpts(client.FromEnv)
    if err != nil {
        return nil, err
    }

    return &DockerDataSource{
        name:    "docker",
        status:  StatusStopped,
        eventCh: make(chan Event, 100),
        client:  cli,
        config:  config,
    }, nil
}

func (ds *DockerDataSource) Start() error {
    ctx := context.Background()

    events, err := ds.client.Events(ctx, types.EventsOptions{})
    if err != nil {
        return err
    }

    go func() {
        for dockerEvent := range events {
            var eventType string
            switch dockerEvent.Action {
            case "start":
                eventType = "StartContainer"
            case "die":
                eventType = "StopContainer"
            }

            container, _ := ds.client.ContainerInspect(ctx, dockerEvent.Actor.ID)

            event := Event{
                Type:      eventType,
                Timestamp: time.Unix(dockerEvent.Time, 0),
                Metadata: map[string]interface{}{
                    "container_id": dockerEvent.Actor.ID,
                    "cgroup_id":   fmt.Sprintf("/proc/%d/cgroup", container.State.Pid),
                    "pid":         container.State.Pid,
                    "raw_data":    dockerEvent,
                },
            }
            ds.eventCh <- event
        }
    }()

    ds.status = StatusRunning
    return nil
}
```

---

## 二、策略引擎具体实现

### 2.1 libdslgo引擎实现

```go
// pkg/dsl/libdslgo/engine.go
package libdslgo

import "github.com/nuts-project/nuts/pkg/libdslgo"

type Engine struct {
    engine *libdslgo.Engine
}

func NewEngine(config map[string]interface{}) (DSLEngine, error) {
    engine := libdslgo.NewEngine()
    return &Engine{engine: engine}, nil
}

func (e *Engine) Name() string {
    return "libdslgo"
}

func (e *Engine) Compile(rules []string) error {
    return e.engine.Compile(false)
}

func (e *Engine) Evaluate(event map[string]interface{}) (bool, string, error) {
    return e.engine.Evaluate(event)
}

func (e *Engine) AddRule(ruleName string, rule string) error {
    return e.engine.AddRule(ruleName, rule)
}

func (e *Engine) RemoveRule(ruleName string) error {
    return e.engine.RemoveRule(ruleName)
}
```

### 2.2 DSL引擎工厂实现

```go
// pkg/dsl/factory.go
package dsl

type DSLEngineFactory struct {
    engines map[string]func(config map[string]interface{}) (DSLEngine, error)
}

func NewDSLEngineFactory() *DSLEngineFactory {
    return &DSLEngineFactory{
        engines: make(map[string]func(config map[string]interface{}) (DSLEngine, error)),
    }
}

func (f *DSLEngineFactory) RegisterEngine(name string, factory func(config map[string]interface{}) (DSLEngine, error)) {
    f.engines[name] = factory
}

func (f *DSLEngineFactory) CreateEngine(name string, config map[string]interface{}) (DSLEngine, error) {
    factory, ok := f.engines[name]
    if !ok {
        return nil, fmt.Errorf("DSL engine not found: %s", name)
    }
    return factory(config)
}
```

---

## 三、任务调度具体实现

### 3.1 Snowflake算法实现

```go
// pkg/common/id/snowflake.go
package id

import (
    "errors"
    "sync"
    "time"
)

const (
    nodeIDBits      = 10
    sequenceBits    = 12
    maxNodeID       = ^(-1 << nodeIDBits)
    maxSequence     = ^(-1 << sequenceBits)
    nodeIDShift     = sequenceBits
    timestampShift  = sequenceBits + nodeIDBits
)

type Snowflake struct {
    nodeID        int64
    sequence      int64
    lastTimestamp int64
    mutex         sync.Mutex
}

func NewSnowflake(nodeID int64) (*Snowflake, error) {
    if nodeID < 0 || nodeID > maxNodeID {
        return nil, errors.New("node ID must be between 0 and 1023")
    }
    return &Snowflake{nodeID: nodeID}, nil
}

func (s *Snowflake) Generate() int64 {
    s.mutex.Lock()
    defer s.mutex.Unlock()

    timestamp := time.Now().UnixMilli()

    if timestamp == s.lastTimestamp {
        s.sequence = (s.sequence + 1) & maxSequence
        if s.sequence == 0 {
            timestamp = s.waitNextMillis(s.lastTimestamp)
        }
    } else {
        s.sequence = 0
    }

    s.lastTimestamp = timestamp

    return (timestamp << timestampShift) |
           (s.nodeID << nodeIDShift) |
           s.sequence
}

func (s *Snowflake) waitNextMillis(lastTimestamp int64) int64 {
    timestamp := time.Now().UnixMilli()
    for timestamp <= lastTimestamp {
        time.Sleep(time.Millisecond)
        timestamp = time.Now().UnixMilli()
    }
    return timestamp
}
```

### 3.2 全局ID生成器实现

```go
// pkg/common/id/generator.go
package id

import (
    "os"
    "strconv"
    "sync"
)

var (
    globalIDGenerator *Snowflake
    once              sync.Once
)

func InitIDGenerator() error {
    var err error
    once.Do(func() {
        nodeID := int64(0)

        if nodeIDStr := os.Getenv("NODE_ID"); nodeIDStr != "" {
            if id, parseErr := strconv.ParseInt(nodeIDStr, 10, 64); parseErr == nil {
                nodeID = id
            }
        }

        globalIDGenerator, err = NewSnowflake(nodeID)
    })

    return err
}

func GenerateTaskID() string {
    if globalIDGenerator == nil {
        globalIDGenerator, _ = NewSnowflake(0)
    }
    return strconv.FormatInt(globalIDGenerator.Generate(), 10)
}
```

### 3.3 状态机具体实现

```go
// pkg/scheduler/state_machine_impl.go
package scheduler

type StateMachineImpl struct {
    config            *StateMachineConfig
    states            map[string]State
    currentState      State
    handlers          map[string]StateHandler
    transitionHandler TransitionHandler
    mutex             sync.RWMutex
}

func (sm *StateMachineImpl) GetCurrentState() State {
    sm.mutex.RLock()
    defer sm.mutex.RUnlock()
    return sm.currentState
}

func (sm *StateMachineImpl) Transition(event Event) error {
    sm.mutex.Lock()
    defer sm.mutex.Unlock()

    currentState := sm.currentState
    currentName := currentState.GetName()

    transition := sm.findTransition(currentName, event.Type)
    if transition == nil {
        return fmt.Errorf("no transition defined from %s for event %s", currentName, event.Type)
    }

    targetState := sm.states[transition.To]
    if targetState == nil {
        return fmt.Errorf("target state '%s' not found", transition.To)
    }

    if sm.transitionHandler != nil {
        canTransition, err := sm.transitionHandler.CanTransition(&StateContext{
            CurrentState: currentState,
            Event:        event,
        }, currentName, transition.To)
        if err != nil {
            return fmt.Errorf("transition handler error: %w", err)
        }
        if !canTransition {
            return fmt.Errorf("transition not allowed")
        }
    }

    return sm.executeTransition(transition, targetState, event)
}

func (sm *StateMachineImpl) findTransition(fromState, eventType string) *TransitionConfig {
    transitions, ok := sm.config.Transitions[fromState]
    if !ok {
        return nil
    }

    for _, transition := range transitions {
        if transition.Event == eventType {
            return &transition
        }
    }
    return nil
}

func (sm *StateMachineImpl) executeTransition(transition *TransitionConfig, targetState *State, event Event) error {
    currentState := sm.currentState

    if err := currentState.OnExit(&StateContext{
        CurrentState:  currentState,
        PreviousState: currentState,
        Event:         event,
    }); err != nil {
        return fmt.Errorf("state exit failed: %w", err)
    }

    if err := targetState.OnEnter(&StateContext{
        CurrentState:  targetState,
        PreviousState: currentState,
        Event:         event,
    }); err != nil {
        return fmt.Errorf("state enter failed: %w", err)
    }

    sm.currentState = targetState
    return nil
}

func (sm *StateMachineImpl) GetStateMachineConfig() *StateMachineConfig {
    return sm.config
}
```

### 3.4 状态机工厂实现

```go
// pkg/scheduler/factory.go
package scheduler

type StateMachineFactory struct {
    handlers          map[string]StateHandler
    transitionHandler TransitionHandler
    eventBus          eventbus.EventBus
    executors         map[string]TaskExecutor
}

func NewStateMachineFactory(eventBus eventbus.EventBus) *StateMachineFactory {
    return &StateMachineFactory{
        handlers:          make(map[string]StateHandler),
        transitionHandler: &NoOpTransitionHandler{},
        eventBus:          eventBus,
        executors:         make(map[string]TaskExecutor),
    }
}

func (f *StateMachineFactory) RegisterHandler(name string, handler StateHandler) {
    f.handlers[name] = handler
}

func (f *StateMachineFactory) RegisterTransitionHandler(handler TransitionHandler) {
    f.transitionHandler = handler
}

func (f *StateMachineFactory) RegisterExecutor(state string, executor TaskExecutor) {
    f.executors[state] = executor
}

func (f *StateMachineFactory) CreateStateMachine(config *StateMachineConfig) (StateMachine, error) {
    sm := &StateMachineImpl{
        config:            config,
        states:            make(map[string]State),
        handlers:          make(map[string]StateHandler),
        transitionHandler: f.transitionHandler,
    }

    for name, stateConfig := range config.States {
        handler, ok := f.handlers[stateConfig.Handler]
        if !ok {
            return nil, fmt.Errorf("handler '%s' not registered for state '%s'", stateConfig.Handler, name)
        }

        state := &StateImpl{
            name:    name,
            config:  stateConfig,
            handler: handler,
        }
        sm.states[name] = state
        sm.handlers[name] = handler
    }

    if initialState, ok := sm.states[config.InitialState]; ok {
        sm.currentState = initialState
    }

    return sm, nil
}
```

### 3.5 采集状态处理器实现

```go
// pkg/scheduler/handlers/collecting.go
package handlers

import (
    "github.com/nuts-project/nuts/pkg/eventbus"
)

type CollectingStateHandler struct {
    eventBus eventbus.EventBus
}

func (h *CollectingStateHandler) OnEnter(ctx *StateContext) error {
    event := eventbus.Event{
        Type: "CollectionStart",
        Payload: map[string]interface{}{
            "task_id":   ctx.Task.ID,
            "cgroup_id": ctx.Task.CgroupID,
            "policy_id": ctx.Task.PolicyID,
            "duration":  ctx.Task.Duration,
            "metrics":   ctx.Task.Metrics,
        },
    }
    return h.eventBus.Publish("collection", event)
}

func (h *CollectingStateHandler) OnExit(ctx *StateContext) error {
    event := eventbus.Event{
        Type: "CollectionStop",
        Payload: map[string]interface{}{
            "task_id":   ctx.Task.ID,
            "cgroup_id": ctx.Task.CgroupID,
        },
    }
    return h.eventBus.Publish("collection", event)
}

func (h *CollectingStateHandler) Execute(ctx *StateContext) error {
    return nil
}

func (h *CollectingStateHandler) CanTransition(ctx *StateContext, nextState string) bool {
    return true
}

---

## 四、数据库具体实现

### 4.1 SQLite实现

```go
// pkg/storage/policy/sqlite.go
package policy

import (
    "context"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
    "github.com/nuts-project/nuts/pkg/storage"
)

type SQLiteDB struct {
    db *sql.DB
}

func NewSQLiteDB(dsn string) (storage.DB, error) {
    db, err := sql.Open("sqlite3", dsn)
    if err != nil {
        return nil, err
    }
    return &SQLiteDB{db: db}, nil
}

func (s *SQLiteDB) Close() error {
    return s.db.Close()
}

func (s *SQLiteDB) Ping(ctx context.Context) error {
    return s.db.PingContext(ctx)
}

func (s *SQLiteDB) Begin(ctx context.Context) (storage.Tx, error) {
    tx, err := s.db.BeginTx(ctx, nil)
    if err != nil {
        return nil, err
    }
    return &SQLiteTx{tx: tx}, nil
}
```

### 5.2 数据库工厂实现

```go
// pkg/storage/factory.go
package storage

type DBFactory struct {
    drivers map[string]DBOpenFunc
}

func NewDBFactory() *DBFactory {
    return &DBFactory{
        drivers: make(map[string]DBOpenFunc),
    }
}

func (f *DBFactory) RegisterDriver(name string, openFunc DBOpenFunc) {
    f.drivers[name] = openFunc
}

func (f *DBFactory) Open(driver, dsn string) (DB, error) {
    openFunc, ok := f.drivers[driver]
    if !ok {
        return nil, fmt.Errorf("unsupported database driver: %s", driver)
    }
    return openFunc(dsn)
}
```

---

## 五、事件总线具体实现

### 5.1 架构设计

EventBus采用中心化架构：
- **服务端**：由TaskScheduler管理，负责事件路由和分发
- **客户端**：其他模块（Collector、AggregationEngine等）作为客户端连接服务端

**部署模式**：
- **单节点部署**：使用gRPC，TaskScheduler启动gRPC服务端
- **多节点部署**：使用Redis或Kafka，所有TaskScheduler连接同一个消息队列

### 5.2 gRPC事件总线实现（单节点）

**适用场景**：单节点部署，无需额外基础设施

**特点**：
- 强类型定义（Protocol Buffers）
- 流式通信支持
- 天然支持跨进程通信
- 易于调试和监控
- 无需额外部署基础设施

#### Protocol Buffers定义

```protobuf
// pkg/eventbus/proto/eventbus.proto
syntax = "proto3";

package eventbus;

option go_package = "github.com/nuts-project/nuts/pkg/eventbus/proto";

service EventBusService {
  // 订阅事件（流式）
  rpc Subscribe(SubscribeRequest) returns (stream Event);
  
  // 发布事件
  rpc Publish(PublishRequest) returns (PublishResponse);
}

message SubscribeRequest {
  string topic = 1;
}

message Event {
  string type = 1;
  int64 timestamp = 2;
  map<string, string> payload = 3;
}

message PublishRequest {
  string topic = 1;
  Event event = 2;
}

message PublishResponse {
  bool success = 1;
  string message = 2;
}
```

#### gRPC Server端实现

```go
// pkg/eventbus/grpc/server.go
package grpc

import (
    "sync"
    "github.com/nuts-project/nuts/pkg/eventbus/proto"
)

type EventBusServer struct {
    proto.UnimplementedEventBusServiceServer
    subscribers map[string]map[string]chan *proto.Event
    mutex       sync.RWMutex
}

func NewEventBusServer() *EventBusServer {
    return &EventBusServer{
        subscribers: make(map[string]map[string]chan *proto.Event),
    }
}

func (s *EventBusServer) Subscribe(req *proto.SubscribeRequest, stream proto.EventBusService_SubscribeServer) error {
    topic := req.Topic
    ch := make(chan *proto.Event, 1000)
    
    s.mutex.Lock()
    if _, ok := s.subscribers[topic]; !ok {
        s.subscribers[topic] = make(map[string]chan *proto.Event)
    }
    subscriberID := generateSubscriberID()
    s.subscribers[topic][subscriberID] = ch
    s.mutex.Unlock()
    
    defer func() {
        s.mutex.Lock()
        delete(s.subscribers[topic], subscriberID)
        close(ch)
        s.mutex.Unlock()
    }()
    
    for event := range ch {
        if err := stream.Send(event); err != nil {
            return err
        }
    }
    
    return nil
}

func (s *EventBusServer) Publish(ctx context.Context, req *proto.PublishRequest) (*proto.PublishResponse, error) {
    s.mutex.RLock()
    defer s.mutex.RUnlock()
    
    topic := req.Topic
    if subscribers, ok := s.subscribers[topic]; ok {
        for _, ch := range subscribers {
            ch <- req.Event
        }
    }
    
    return &proto.PublishResponse{Success: true}, nil
}

func generateSubscriberID() string {
    return fmt.Sprintf("sub-%d", time.Now().UnixNano())
}
```

#### gRPC Client端实现

```go
// pkg/eventbus/grpc/client.go
package grpc

import (
    "context"
    "fmt"
    "sync"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "github.com/nuts-project/nuts/pkg/eventbus/proto"
)

type GrpcEventBus struct {
    client proto.EventBusServiceClient
    conn   *grpc.ClientConn
}

func NewGrpcEventBus(config map[string]interface{}) (eventbus.EventBus, error) {
    addr, ok := config["addr"].(string)
    if !ok {
        return nil, fmt.Errorf("eventbus addr is required")
    }
    
    conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, err
    }
    
    return &GrpcEventBus{
        client: proto.NewEventBusServiceClient(conn),
        conn:   conn,
    }, nil
}

func (b *GrpcEventBus) Publish(topic string, event eventbus.Event) error {
    pbEvent := &proto.Event{
        Type:      event.Type,
        Timestamp: event.Timestamp,
        Payload:   convertPayloadToString(event.Payload),
    }
    
    _, err := b.client.Publish(context.Background(), &proto.PublishRequest{
        Topic:  topic,
        Event:  pbEvent,
    })
    return err
}

func (b *GrpcEventBus) Subscribe(topic string) <-chan eventbus.Event {
    ch := make(chan eventbus.Event, 1000)
    
    stream, err := b.client.Subscribe(context.Background(), &proto.SubscribeRequest{
        Topic: topic,
    })
    if err != nil {
        close(ch)
        return ch
    }
    
    go func() {
        defer close(ch)
        for {
            pbEvent, err := stream.Recv()
            if err != nil {
                return
            }
            
            event := eventbus.Event{
                Type:      pbEvent.Type,
                Timestamp: pbEvent.Timestamp,
                Payload:   convertPayloadToInterface(pbEvent.Payload),
            }
            ch <- event
        }
    }()
    
    return ch
}

func (b *GrpcEventBus) Unsubscribe(topic string) error {
    // gRPC流式订阅通过关闭stream自动取消订阅
    return nil
}

func (b *GrpcEventBus) Close() error {
    return b.conn.Close()
}

func convertPayloadToString(payload map[string]interface{}) map[string]string {
    result := make(map[string]string)
    for k, v := range payload {
        result[k] = fmt.Sprintf("%v", v)
    }
    return result
}

func convertPayloadToInterface(payload map[string]string) map[string]interface{} {
    result := make(map[string]interface{})
    for k, v := range payload {
        result[k] = v
    }
    return result
}
```

#### 注册函数

```go
// pkg/eventbus/grpc/grpc.go
package grpc

func NewGrpcEventBus(config map[string]interface{}) (eventbus.EventBus, error) {
    return NewGrpcEventBus(config)
}
```

### 5.3 Redis事件总线实现（多节点）

**适用场景**：多节点部署，已有Redis基础设施

**特点**：
- 利用Redis Pub/Sub机制
- 支持分布式部署
- 性能较好，延迟低
- 消息不持久化，Redis重启后消息丢失
- 所有TaskScheduler连接同一个Redis实例

```go
// pkg/eventbus/redis/redis.go
package redis

import (
    "context"
    "fmt"
    "sync"
    "github.com/go-redis/redis/v8"
    "github.com/nuts-project/nuts/pkg/eventbus"
)

type RedisEventBus struct {
    client   *redis.Client
    channels map[string]*redis.PubSub
    mutex    sync.RWMutex
}

func NewRedisEventBus(config map[string]interface{}) (eventbus.EventBus, error) {
    addr, ok := config["addr"].(string)
    if !ok {
        return nil, fmt.Errorf("redis addr is required")
    }
    
    return &RedisEventBus{
        client: redis.NewClient(&redis.Options{
            Addr: addr,
        }),
        channels: make(map[string]*redis.PubSub),
    }, nil
}

func (b *RedisEventBus) Publish(topic string, event eventbus.Event) error {
    data, err := json.Marshal(event)
    if err != nil {
        return err
    }
    
    return b.client.Publish(context.Background(), topic, data).Err()
}

func (b *RedisEventBus) Subscribe(topic string) <-chan eventbus.Event {
    ch := make(chan eventbus.Event, 1000)
    
    pubsub := b.client.Subscribe(context.Background(), topic)
    
    b.mutex.Lock()
    b.channels[topic] = pubsub
    b.mutex.Unlock()
    
    go func() {
        defer close(ch)
        defer pubsub.Close()
        
        for msg := range pubsub.Channel() {
            var event eventbus.Event
            if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
                continue
            }
            ch <- event
        }
    }()
    
    return ch
}

func (b *RedisEventBus) Unsubscribe(topic string) error {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    if pubsub, ok := b.channels[topic]; ok {
        pubsub.Close()
        delete(b.channels, topic)
    }
    return nil
}

func (b *RedisEventBus) Close() error {
    b.client.Close()
    
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    for _, pubsub := range b.channels {
        pubsub.Close()
    }
    return nil
}
```

#### 注册函数

```go
// pkg/eventbus/redis/redis.go

func NewRedisEventBus(config map[string]interface{}) (eventbus.EventBus, error) {
    return NewRedisEventBus(config)
}
```

### 5.4 Kafka事件总线实现（多节点）

**适用场景**：大规模分布式部署，需要高吞吐量和持久化

**特点**：
- 支持分布式部署
- 高吞吐量，持久化存储
- 支持消费者组，实现负载均衡
- 需要额外部署Kafka集群
- 所有TaskScheduler连接同一个Kafka集群

```go
// pkg/eventbus/kafka/kafka.go
package kafka

import (
    "context"
    "fmt"
    "sync"
    "strings"
    "github.com/segmentio/kafka-go"
    "github.com/nuts-project/nuts/pkg/eventbus"
)

type KafkaEventBus struct {
    producer *kafka.Writer
    readers  map[string]*kafka.Reader
    mutex    sync.RWMutex
}

func NewKafkaEventBus(config map[string]interface{}) (eventbus.EventBus, error) {
    addr, ok := config["addr"].(string)
    if !ok {
        return nil, fmt.Errorf("kafka addr is required")
    }
    
    brokers := strings.Split(addr, ",")
    
    return &KafkaEventBus{
        producer: &kafka.Writer{
            Addr:     kafka.TCP(brokers...),
            Balancer: &kafka.LeastBytes{},
        },
        readers: make(map[string]*kafka.Reader),
    }, nil
}

func (b *KafkaEventBus) Publish(topic string, event eventbus.Event) error {
    data, err := json.Marshal(event)
    if err != nil {
        return err
    }
    
    msg := kafka.Message{
        Topic: topic,
        Value: data,
    }
    
    return b.producer.WriteMessages(context.Background(), msg)
}

func (b *KafkaEventBus) Subscribe(topic string) <-chan eventbus.Event {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    ch := make(chan eventbus.Event, 1000)
    
    reader := kafka.NewReader(kafka.ReaderConfig{
        Brokers: b.producer.Addr,
        Topic:   topic,
        GroupID: "nuts-consumer-group",
    })
    
    b.readers[topic] = reader
    
    go func() {
        defer close(ch)
        for {
            msg, err := reader.ReadMessage(context.Background())
            if err != nil {
                return
            }
            
            var event eventbus.Event
            if err := json.Unmarshal(msg.Value, &event); err != nil {
                continue
            }
            ch <- event
        }
    }()
    
    return ch
}

func (b *KafkaEventBus) Unsubscribe(topic string) error {
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    if reader, ok := b.readers[topic]; ok {
        reader.Close()
        delete(b.readers, topic)
    }
    return nil
}

func (b *KafkaEventBus) Close() error {
    b.producer.Close()
    
    b.mutex.Lock()
    defer b.mutex.Unlock()
    
    for _, reader := range b.readers {
        reader.Close()
    }
    return nil
}
```

#### 注册函数

```go
// pkg/eventbus/kafka/kafka.go

func NewKafkaEventBus(config map[string]interface{}) (eventbus.EventBus, error) {
    return NewKafkaEventBus(config)
}
```

### 5.5 选择建议

- **轻量化部署**：使用gRPC EventBus，无需额外部署
- **模块化部署**：默认使用gRPC，如需更高吞吐量可切换到Kafka
- **已有Redis**：可使用Redis EventBus，利用现有基础设施
- **大规模生产**：使用Kafka EventBus，高吞吐量且持久化

---

## 六、数据源管理器具体实现

### 6.1 DataSourceManager实现

```go
// pkg/datasource/manager.go
package datasource

type DataSourceManager struct {
    dataSources    map[string]DataSource
    factories      map[string]DataSourceFactory
    eventBus       eventbus.EventBus
    mutex          sync.RWMutex
}

func NewDataSourceManager(eventBus eventbus.EventBus) *DataSourceManager {
    return &DataSourceManager{
        dataSources: make(map[string]DataSource),
        factories:   make(map[string]DataSourceFactory),
        eventBus:    eventBus,
    }
}

func (m *DataSourceManager) RegisterFactory(name string, factory DataSourceFactory) {
    m.mutex.Lock()
    defer m.mutex.Unlock()
    m.factories[name] = factory
}

func (m *DataSourceManager) CreateDataSource(name string, config map[string]interface{}) error {
    m.mutex.Lock()
    defer m.mutex.Unlock()
    
    dsType, ok := config["type"].(string)
    if !ok {
        return fmt.Errorf("datasource type is required")
    }
    
    factory, ok := m.factories[dsType]
    if !ok {
        return fmt.Errorf("datasource factory not found: %s", dsType)
    }
    
    ds, err := factory.Create(config)
    if err != nil {
        return err
    }
    
    m.dataSources[name] = ds
    return nil
}

func (m *DataSourceManager) GetDataSource(name string) (DataSource, error) {
    m.mutex.RLock()
    defer m.mutex.RUnlock()
    
    ds, ok := m.dataSources[name]
    if !ok {
        return nil, fmt.Errorf("datasource not found: %s", name)
    }
    return ds, nil
}

func (m *DataSourceManager) StartAll() error {
    m.mutex.RLock()
    defer m.mutex.RUnlock()
    
    for name, ds := range m.dataSources {
        if err := ds.Start(); err != nil {
            log.Printf("Failed to start datasource %s: %v", name, err)
        }
    }
    return nil
}

func (m *DataSourceManager) StopAll() error {
    m.mutex.RLock()
    defer m.mutex.RUnlock()
    
    for name, ds := range m.dataSources {
        if err := ds.Stop(); err != nil {
            log.Printf("Failed to stop datasource %s: %v", name, err)
        }
    }
    return nil
}
```

---

## 七、配置管理具体实现

### 7.1 文件配置源实现

```go
// pkg/config/file.go
package config

import (
    "os"
    "gopkg.in/yaml.v3"
)

type FileConfig struct {
    data     map[string]interface{}
    filePath string
    mutex    sync.RWMutex
}

func NewFileConfig(filePath string) (*FileConfig, error) {
    cfg := &FileConfig{
        filePath: filePath,
        data:     make(map[string]interface{}),
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

func (c *FileConfig) Set(key string, value interface{}) error {
    c.mutex.Lock()
    defer c.mutex.Unlock()
    
    keys := strings.Split(key, ".")
    current := c.data
    
    for i, k := range keys {
        if i == len(keys)-1 {
            if m, ok := current.(map[string]interface{}); ok {
                m[k] = value
            }
        } else {
            if m, ok := current.(map[string]interface{}); ok {
                if _, exists := m[k]; !exists {
                    m[k] = make(map[string]interface{})
                }
                current = m[k]
            }
        }
    }
    
    return nil
}

func (c *FileConfig) Save() error {
    c.mutex.RLock()
    defer c.mutex.RUnlock()
    
    data, err := yaml.Marshal(c.data)
    if err != nil {
        return err
    }
    
    return os.WriteFile(c.filePath, data, 0644)
}
```

---

## 八、监控和日志具体实现

### 8.1 日志实现

```go
// pkg/logger/zap.go
package logger

import (
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

type ZapLogger struct {
    logger *zap.Logger
}

func NewZapLogger(level string) (*ZapLogger, error) {
    var zapLevel zapcore.Level
    switch level {
    case "debug":
        zapLevel = zapcore.DebugLevel
    case "info":
        zapLevel = zapcore.InfoLevel
    case "warn":
        zapLevel = zapcore.WarnLevel
    case "error":
        zapLevel = zapcore.ErrorLevel
    default:
        zapLevel = zapcore.InfoLevel
    }
    
    config := zap.Config{
        Level:       zap.NewAtomicLevelAt(zapLevel),
        Development: false,
        Encoding:    "json",
        EncoderConfig: zapcore.EncoderConfig{
            TimeKey:        "timestamp",
            LevelKey:       "level",
            NameKey:        "logger",
            CallerKey:      "caller",
            MessageKey:     "msg",
            StacktraceKey:  "stacktrace",
            LineEnding:     zapcore.DefaultLineEnding,
            EncodeLevel:    zapcore.LowercaseLevelEncoder,
            EncodeTime:     zapcore.ISO8601TimeEncoder,
            EncodeDuration: zapcore.SecondsDurationEncoder,
            EncodeCaller:   zapcore.ShortCallerEncoder,
        },
        OutputPaths:      []string{"stdout"},
        ErrorOutputPaths: []string{"stderr"},
    }
    
    logger, err := config.Build()
    if err != nil {
        return nil, err
    }
    
    return &ZapLogger{logger: logger}, nil
}

func (l *ZapLogger) Debug(msg string, fields ...Field) {
    l.logger.Debug(msg, l.convertFields(fields)...)
}

func (l *ZapLogger) Info(msg string, fields ...Field) {
    l.logger.Info(msg, l.convertFields(fields)...)
}

func (l *ZapLogger) Warn(msg string, fields ...Field) {
    l.logger.Warn(msg, l.convertFields(fields)...)
}

func (l *ZapLogger) Error(msg string, fields ...Field) {
    l.logger.Error(msg, l.convertFields(fields)...)
}

func (l *ZapLogger) WithFields(fields ...Field) Logger {
    return &ZapLogger{logger: l.logger.With(l.convertFields(fields)...)}
}

func (l *ZapLogger) convertFields(fields []Field) []zap.Field {
    zapFields := make([]zap.Field, len(fields))
    for i, f := range fields {
        zapFields[i] = zap.Any(f.Key, f.Value)
    }
    return zapFields
}
```

### 8.2 指标实现

```go
// pkg/metrics/prometheus.go
package metrics

import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promauto"
)

type PrometheusMetrics struct {
    registry *prometheus.Registry
}

func NewPrometheusMetrics() *PrometheusMetrics {
    return &PrometheusMetrics{
        registry: prometheus.NewRegistry(),
    }
}

func (m *PrometheusMetrics) NewCounter(name string, labels ...string) Counter {
    opts := prometheus.CounterOpts{
        Name: name,
    }
    counter := promauto.With(m.registry).NewCounter(opts)
    return &PrometheusCounter{counter: counter}
}

func (m *PrometheusMetrics) NewGauge(name string, labels ...string) Gauge {
    opts := prometheus.GaugeOpts{
        Name: name,
    }
    gauge := promauto.With(m.registry).NewGauge(opts)
    return &PrometheusGauge{gauge: gauge}
}

func (m *PrometheusMetrics) NewHistogram(name string, labels ...string) Histogram {
    opts := prometheus.HistogramOpts{
        Name: name,
    }
    histogram := promauto.With(m.registry).NewHistogram(opts)
    return &PrometheusHistogram{histogram: histogram}
}

type PrometheusCounter struct {
    counter prometheus.Counter
}

func (c *PrometheusCounter) Inc() {
    c.counter.Inc()
}

func (c *PrometheusCounter) Add(delta float64) {
    c.counter.Add(delta)
}

type PrometheusGauge struct {
    gauge prometheus.Gauge
}

func (g *PrometheusGauge) Set(value float64) {
    g.gauge.Set(value)
}

func (g *PrometheusGauge) Inc() {
    g.gauge.Inc()
}

func (g *PrometheusGauge) Dec() {
    g.gauge.Dec()
}

func (g *PrometheusGauge) Add(delta float64) {
    g.gauge.Add(delta)
}

func (g *PrometheusGauge) Sub(delta float64) {
    g.gauge.Sub(delta)
}

type PrometheusHistogram struct {
    histogram prometheus.Histogram
}

func (h *PrometheusHistogram) Observe(value float64) {
    h.histogram.Observe(value)
}
```

### 8.3 链路追踪实现

```go
// pkg/tracer/opentelemetry.go
package tracer

import (
    "context"
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
)

type OpenTelemetryTracer struct {
    tracer trace.Tracer
}

func NewOpenTelemetryTracer(serviceName string) (*OpenTelemetryTracer, error) {
    tracer := otel.Tracer(serviceName)
    return &OpenTelemetryTracer{tracer: tracer}, nil
}

func (t *OpenTelemetryTracer) StartSpan(ctx context.Context, name string) (context.Context, Span) {
    ctx, span := t.tracer.Start(ctx, name)
    return ctx, &OpenTelemetrySpan{span: span}
}

type OpenTelemetrySpan struct {
    span trace.Span
}

func (s *OpenTelemetrySpan) End() {
    s.span.End()
}

func (s *OpenTelemetrySpan) SetTag(key string, value interface{}) {
    s.span.SetAttributes(key, value)
}

func (s *OpenTelemetrySpan) SetError(err error) {
    s.span.RecordError(err)
}
```

---

## 九、安全具体实现

### 9.1 认证实现

```go
// pkg/auth/jwt.go
package auth

import (
    "github.com/golang-jwt/jwt/v5"
)

type JWTAuthenticator struct {
    secretKey []byte
}

func NewJWTAuthenticator(secretKey string) *JWTAuthenticator {
    return &JWTAuthenticator{secretKey: []byte(secretKey)}
}

func (a *JWTAuthenticator) Authenticate(token string) (*Claims, error) {
    parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return a.secretKey, nil
    })
    
    if err != nil {
        return nil, err
    }
    
    if claims, ok := parsedToken.Claims.(jwt.MapClaims); ok && parsedToken.Valid {
        return &Claims{
            Subject:   claims["sub"].(string),
            IssuedAt:  time.Unix(int64(claims["iat"].(float64)), 0),
            ExpiresAt: time.Unix(int64(claims["exp"].(float64)), 0),
        }, nil
    }
    
    return nil, fmt.Errorf("invalid token")
}
```

### 9.2 加密实现

```go
// pkg/crypto/aes.go
package crypto

import (
    "crypto/aes"
    "crypto/cipher"
    "crypto/rand"
    "io"
)

type AESEncryptor struct {
    key []byte
}

func NewAESEncryptor(key []byte) (*AESEncryptor, error) {
    if len(key) != 32 {
        return nil, fmt.Errorf("key must be 32 bytes")
    }
    return &AESEncryptor{key: key}, nil
}

func (e *AESEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
    block, err := aes.NewCipher(e.key)
    if err != nil {
        return nil, err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    
    nonce := make([]byte, gcm.NonceSize())
    if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
        return nil, err
    }
    
    ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)
    return ciphertext, nil
}

func (e *AESEncryptor) Decrypt(ciphertext []byte) ([]byte, error) {
    block, err := aes.NewCipher(e.key)
    if err != nil {
        return nil, err
    }
    
    gcm, err := cipher.NewGCM(block)
    if err != nil {
        return nil, err
    }
    
    nonceSize := gcm.NonceSize()
    if len(ciphertext) < nonceSize {
        return nil, fmt.Errorf("ciphertext too short")
    }
    
    nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
    plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
    if err != nil {
        return nil, err
    }
    
    return plaintext, nil
}
```

---

## 十、测试具体实现

### 10.1 单元测试示例

```go
// pkg/eventbus/eventbus_test.go
package eventbus_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/nuts-project/nuts/pkg/eventbus"
)

func TestEventBusPublishSubscribe(t *testing.T) {
    // 创建Mock EventBus
    mockBus := &MockEventBus{}
    
    // 订阅主题
    ch := mockBus.Subscribe("test")
    
    // 发布事件
    event := eventbus.Event{
        Type:      "test",
        Timestamp: time.Now().Unix(),
        Payload:   map[string]interface{}{"key": "value"},
    }
    err := mockBus.Publish("test", event)
    assert.NoError(t, err)
    
    // 接收事件
    received := <-ch
    assert.Equal(t, "test", received.Type)
    assert.Equal(t, "value", received.Payload["key"])
}
```

### 10.2 集成测试示例

```go
// pkg/scheduler/scheduler_test.go
package scheduler_test

import (
    "testing"
    "github.com/stretchr/testify/assert"
    "github.com/nuts-project/nuts/pkg/scheduler"
)

func TestTaskSchedulerIntegration(t *testing.T) {
    // 创建依赖
    config := config.NewMemoryConfig()
    eventBus := eventbus.NewInProcessEventBus()
    stateMachineFactory := statemachine.NewFactory(config, eventBus)
    
    // 创建TaskScheduler
    scheduler := scheduler.NewTaskScheduler(config, eventBus, stateMachineFactory)
    
    // 提交任务
    task := &scheduler.Task{
        ID:       "test-task",
        CgroupID: "/test",
        PolicyID: "test-policy",
    }
    
    err := scheduler.Submit(task)
    assert.NoError(t, err)
}
```

---

## 十一、性能监控具体实现

### 11.1 性能指标采集

```go
// pkg/perf/monitor.go
package perf

import (
    "github.com/prometheus/client_golang/prometheus"
)

type PerformanceMonitor struct {
    taskCounter      prometheus.Counter
    stateTransition  prometheus.Counter
    eventPublished   prometheus.Counter
    eventReceived    prometheus.Counter
}

func NewPerformanceMonitor() *PerformanceMonitor {
    return &PerformanceMonitor{
        taskCounter: promauto.NewCounter(prometheus.CounterOpts{
            Name: "nuts_tasks_total",
            Help: "Total number of tasks",
        }),
        stateTransition: promauto.NewCounter(prometheus.CounterOpts{
            Name: "nuts_state_transitions_total",
            Help: "Total number of state transitions",
        }),
        eventPublished: promauto.NewCounter(prometheus.CounterOpts{
            Name: "nuts_events_published_total",
            Help: "Total number of events published",
        }),
        eventReceived: promauto.NewCounter(prometheus.CounterOpts{
            Name: "nuts_events_received_total",
            Help: "Total number of events received",
        }),
    }
}

func (m *PerformanceMonitor) RecordTaskCreated() {
    m.taskCounter.Inc()
}

func (m *PerformanceMonitor) RecordStateTransition() {
    m.stateTransition.Inc()
}

func (m *PerformanceMonitor) RecordEventPublished() {
    m.eventPublished.Inc()
}

func (m *PerformanceMonitor) RecordEventReceived() {
    m.eventReceived.Inc()
}
```

---

## 十二、项目概述

### 项目背景

NUTS（NRI Utility for Troubleshooting System）是一个基于容器运行时接口（NRI）的故障分析系统，用于实时监控、采集、分析和诊断容器性能问题。

### 项目目标

1. **实时监控**：通过NRI接口实时监听容器生命周期事件
2. **智能采集**：根据策略自动采集容器性能数据
3. **数据聚合**：对采集的数据进行聚合处理
4. **故障诊断**：使用AI和规则引擎诊断容器性能问题
5. **可视化展示**：提供友好的可视化界面展示分析结果

### 架构设计

```
┌─────────────────────────────────────────────────────────────┐
│                        NUTS 架构                              │
└─────────────────────────────────────────────────────────────┘
┌──────────────┐  ┌──────────────┐  ┌──────────────┐         │
│  CLI Tool    │  │   Service    │  │  Collector   │         │
│              │  │              │  │              │         │
│ - 策略推送   │  │ - DataSource │  │ - eBPF采集   │         │
│ - 状态查询   │  │ - PolicyEngine│  │ - 数据上传   │         │
│ - 日志查看   │  │ - TaskScheduler│ │ - 独立部署   │         │
│              │  │ - Aggregation │  │              │         │
│              │  │ - Diagnostic │  │              │         │
└──────┬───────┘  └──────┬───────┘  └──────┬───────┘         │
       │                  │                  │                  │
       └──────────────────┼──────────────────┘                  │
                          │                                     │
                    ┌─────▼─────┐                               │
                    │  EventBus │                               │
                    │  (gRPC)   │                               │
                    └─────┬─────┘                               │
                          │                                     │
         ┌────────────────┼────────────────┐                   │
         ↓                ↓                ↓                   │
  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
  │  EventDB     │  │  AuditDB     │  │  DiagnosisDB │      │
  │ (InfluxDB)   │  │ (PostgreSQL) │  │ (PostgreSQL) │      │
  └──────────────┘  └──────────────┘  └──────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

---

## 十三、采集器设计

### 设计目标

采集器负责采集容器性能数据，使用eBPF技术实现低开销的数据采集。

**设计目标**：
1. **低开销**：使用eBPF技术，最小化对目标容器性能的影响
2. **可配置**：支持配置采集指标、采样频率、采集时长
3. **可扩展**：支持添加新的采集器类型
4. **独立部署**：作为独立进程运行，需要特权权限

### 采集器类型

**系统调用采集器**：
- 采集系统调用信息
- 分析系统调用延迟
- 检测异常系统调用

**网络采集器**：
- 采集网络流量信息
- 分析网络延迟
- 检测网络异常

**CPU采集器**：
- 采集CPU使用率
- 分析CPU调度延迟
- 检测CPU瓶颈

**内存采集器**：
- 采集内存使用情况
- 分析内存分配模式
- 检测内存泄漏

### 部署方式

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: collector
spec:
  selector:
    matchLabels:
      app: collector
  template:
    metadata:
      labels:
        app: collector
    spec:
      containers:
      - name: collector
        image: nuts/collector:latest
        securityContext:
          privileged: true
        volumeMounts:
        - name: sys
          mountPath: /sys
        - name: debugfs
          mountPath: /sys/kernel/debug
        - name: bpf
          mountPath: /sys/fs/bpf
      volumes:
      - name: sys
        hostPath:
          path: /sys
      - name: debugfs
        hostPath:
          path: /sys/kernel/debug
      - name: bpf
        hostPath:
          path: /sys/fs/bpf
```

---

## 十四、聚合引擎设计

### 设计目标

聚合引擎负责对采集的数据进行聚合处理，生成可分析的聚合数据。

**设计目标**：
1. **高效聚合**：支持大规模数据的高效聚合
2. **灵活配置**：支持配置聚合规则和聚合窗口
3. **实时处理**：支持实时数据流处理
4. **可扩展**：支持添加新的聚合函数

### 聚合类型

**时间聚合**：
- 按时间窗口聚合数据
- 支持固定窗口和滑动窗口
- 支持多种时间粒度

**空间聚合**：
- 按容器聚合数据
- 按节点聚合数据
- 按集群聚合数据

**指标聚合**：
- 求和、平均值、最大值、最小值
- 百分位数计算
- 趋势分析

---

## 十五、诊断引擎设计

### 设计目标

诊断引擎负责分析聚合数据，诊断容器性能问题。

**设计目标**：
1. **智能诊断**：使用AI算法进行智能诊断
2. **规则驱动**：支持规则引擎进行规则诊断
3. **可解释**：提供可解释的诊断结果
4. **可扩展**：支持添加新的诊断策略

### 诊断策略

**内置诊断策略**：
- 基于规则的诊断
- 基于阈值的诊断
- 基于模式的诊断

**AI诊断策略**：
- 基于机器学习的诊断
- 基于深度学习的诊断
- 基于知识图谱的诊断

### 降级策略

- AI诊断失败时自动降级到内置诊断
- 超时自动降级
- 可配置是否启用降级

---

## 十六、部署场景设计

### 轻量化部署

**架构**：
```
Service进程: DataSource + PolicyEngine + TaskScheduler + AggregationEngine + DiagnosticEngine
Collector进程: 独立

通信方式:
- TaskScheduler → AggregationEngine: 事件总线（gRPC）
- TaskScheduler → DiagnosticEngine: 事件总线（gRPC）
- Collector → Service: 事件总线（gRPC）
```

**配置**：
```yaml
eventbus:
  type: "grpc"
  addr: "localhost:50051"
```

### 模块化部署

**架构**：
```
Service进程: DataSource + PolicyEngine + TaskScheduler
Aggregation进程: AggregationEngine
Diagnostic进程: DiagnosticEngine
Collector进程: 独立

通信方式:
- Service → Aggregation: 事件总线（gRPC）
- Service → Diagnostic: 事件总线（gRPC）
- Collector → Service: 事件总线（gRPC）
```

**配置**：
```yaml
eventbus:
  type: "grpc"
  addr: "service:50051"
```

### 多节点部署

**架构**：
```
多个Service进程: DataSource + PolicyEngine + TaskScheduler
多个Aggregation进程: AggregationEngine
多个Diagnostic进程: DiagnosticEngine
多个Collector进程: 独立

通信方式:
- 所有组件通过Redis/Kafka通信
```

**配置**：
```yaml
eventbus:
  type: "redis"
  addr: "redis://localhost:6379"
```

或

```yaml
eventbus:
  type: "kafka"
  addr: "kafka-1:9092,kafka-2:9092"
```

---

## 十七、开发阶段

### 第一阶段：CLI策略推送（预计2个月）

**目标**：实现基本的策略管理功能，支持CLI命令行工具推送策略给service

#### 任务分解

**Week 1-2: 项目初始化**
- [ ] 创建项目目录结构
- [ ] 初始化Go模块
- [ ] 配置CI/CD

**Week 3-4: 数据源模块**
- [ ] 实现数据源接口
- [ ] 实现SQLite数据源
- [ ] 实现数据源管理器

**Week 5-6: 策略引擎模块**
- [ ] 实现策略引擎接口
- [ ] 实现libdslgo策略引擎
- [ ] 实现策略存储

**Week 7-8: CLI工具**
- [ ] 实现CLI框架
- [ ] 实现策略推送命令
- [ ] 实现策略查询命令

### 第二阶段：任务调度和状态机（预计2个月）

**目标**：实现任务调度和状态机功能

#### 任务分解

**Week 9-10: 状态机模块**
- [ ] 实现状态机接口
- [ ] 实现状态处理器
- [ ] 实现状态机工厂

**Week 11-12: 任务调度模块**
- [ ] 实现任务调度器
- [ ] 实现任务队列
- [ ] 实现任务生命周期管理

**Week 13-14: 事件总线**
- [ ] 实现事件总线接口
- [ ] 实现gRPC事件总线
- [ ] 实现事件发布订阅

### 第三阶段：采集器和聚合引擎（预计3个月）

**目标**：实现采集器和聚合引擎功能

#### 任务分解

**Week 15-18: 采集器**
- [ ] 实现eBPF采集器
- [ ] 实现系统调用采集
- [ ] 实现网络采集
- [ ] 实现CPU/内存采集

**Week 19-22: 聚合引擎**
- [ ] 实现聚合引擎接口
- [ ] 实现时间聚合
- [ ] 实现空间聚合
- [ ] 实现指标聚合

**Week 23-24: 集成测试**
- [ ] 端到端测试
- [ ] 性能测试
- [ ] 压力测试

### 第四阶段：诊断引擎和可视化（预计3个月）

**目标**：实现诊断引擎和可视化界面

#### 任务分解

**Week 25-28: 诊断引擎**
- [ ] 实现诊断引擎接口
- [ ] 实现规则诊断
- [ ] 实现AI诊断
- [ ] 实现降级策略

**Week 29-32: 可视化**
- [ ] 实现Web界面
- [ ] 实现数据可视化
- [ ] 实现诊断结果展示

**Week 33-36: 部署和优化**
- [ ] 容器化部署
- [ ] 性能优化
- [ ] 文档完善

---

## 十八、验收标准

### 第一阶段验收标准

**CLI策略推送**：
- [ ] CLI工具能够成功推送策略到Service
- [ ] Service能够正确解析和存储策略
- [ ] 支持策略的增删改查
- [ ] 单元测试覆盖率 > 80%

**数据源模块**：
- [ ] 支持SQLite数据源
- [ ] 支持策略的CRUD操作
- [ ] 支持事务处理

**策略引擎模块**：
- [ ] 支持libdslgo策略引擎
- [ ] 能够正确解析策略
- [ ] 能够正确执行策略

### 第二阶段验收标准

**状态机模块**：
- [ ] 支持状态转换
- [ ] 支持状态处理器
- [ ] 支持配置驱动

**任务调度模块**：
- [ ] 支持任务提交
- [ ] 支持任务生命周期管理
- [ ] 支持任务队列

**事件总线**：
- [ ] 支持事件发布订阅
- [ ] 支持gRPC实现
- [ ] 支持多主题订阅

### 第三阶段验收标准

**采集器**：
- [ ] 支持eBPF采集
- [ ] 支持多种指标采集
- [ ] 采集开销 < 5%

**聚合引擎**：
- [ ] 支持时间聚合
- [ ] 支持空间聚合
- [ ] 支持实时处理

### 第四阶段验收标准

**诊断引擎**：
- [ ] 支持规则诊断
- [ ] 支持AI诊断
- [ ] 支持降级策略

**可视化**：
- [ ] 支持Web界面
- [ ] 支持数据可视化
- [ ] 支持诊断结果展示

**部署**：
- [ ] 支持容器化部署
- [ ] 支持多种部署场景
- [ ] 文档完善
- [ ] 实现PDF导出
- [ ] 实现报告存储
- [ ] 编写单元测试

---

## 十九、验收标准

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

---

**文档结束**

本插件实例设计文档描述了NUTS项目的故障分析插件具体实现，包括采集器、聚合引擎、诊断引擎等业务组件的设计和实现细节。这些组件基于通用框架构建，实现了具体的故障分析业务逻辑。
