package component

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
)

// Component 组件接口
type Component interface {
	// Info 获取组件信息
	Info() ComponentInfo

	// Init 初始化
	Init(config ComponentConfig) error

	// Start 启动处理循环
	Start(ctx context.Context) error

	// Stop 优雅关闭
	Stop() error

	// Health 健康检查
	Health() error
}

// ComponentInfo 组件信息
type ComponentInfo struct {
	Name         string
	Version      string
	HandlesState string // 处理的状态
	NextState    string // 成功后的下一个状态
	FailureState string // 失败后的状态
}

// ComponentConfig 组件配置
type ComponentConfig struct {
	EventBusServer    string
	ReconnectInterval time.Duration

	// Processing config
	MaxConcurrent int
	Timeout       time.Duration
	RetryAttempts int
}

// BaseComponent 基础组件实现
// 提供通用功能，具体组件只需实现业务逻辑
type BaseComponent struct {
	info       ComponentInfo
	config     ComponentConfig
	bus        eventbus.EventBus
	workerPool *WorkerPool
	state      ComponentState
	mu         sync.RWMutex

	// 业务逻辑回调
	handler EventHandler

	// 生命周期
	ctx    context.Context
	cancel context.CancelFunc
}

// ComponentState 组件状态
type ComponentState struct {
	Status    string // "initializing", "running", "stopping", "stopped", "error"
	StartedAt time.Time
	StoppedAt time.Time
	Processed int64
	Failed    int64
	LastEvent time.Time
	Error     error
}

// EventHandler 事件处理回调
type EventHandler func(event *common.Event) error

// NewBaseComponent 创建基础组件
func NewBaseComponent(info ComponentInfo, config ComponentConfig, bus eventbus.EventBus, handler EventHandler) *BaseComponent {
	return &BaseComponent{
		info:       info,
		config:     config,
		bus:        bus,
		handler:    handler,
		workerPool: NewWorkerPool(config.MaxConcurrent),
	}
}

// Init 初始化组件
func (c *BaseComponent) Init(config ComponentConfig) error {
	c.config = config
	return nil
}

// Start 启动组件
func (c *BaseComponent) Start(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.state.Status == "running" {
		return fmt.Errorf("component already running")
	}

	c.state.Status = "running"
	c.state.StartedAt = time.Now()
	c.ctx, c.cancel = context.WithCancel(ctx)

	// 订阅特定状态 topic
	topic := fmt.Sprintf("%s%s", common.TaskEventTopicPrefix, c.info.HandlesState)
	ch := c.bus.Subscribe(topic)

	// 启动 worker pool
	c.workerPool.Start(ch, c.handler)

	return nil
}

// Stop 停止组件
func (c *BaseComponent) Stop() error {
	c.mu.Lock()

	if c.state.Status != "running" {
		c.mu.Unlock()
		return fmt.Errorf("component not running")
	}

	c.state.Status = "stopping"
	c.cancel()
	c.mu.Unlock()

	c.workerPool.Stop()

	c.mu.Lock()
	c.state.Status = "stopped"
	c.state.StoppedAt = time.Now()
	c.mu.Unlock()

	return nil
}

// GetState 获取组件状态
func (c *BaseComponent) GetState() ComponentState {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Health 健康检查
func (c *BaseComponent) Health() error {
	// 检查 EventBus 连接
	if err := c.bus.Health(); err != nil {
		return fmt.Errorf("eventbus unhealthy: %w", err)
	}
	return nil
}

// Info 获取组件信息
func (c *BaseComponent) Info() ComponentInfo {
	return c.info
}

// PublishStateTransition 发布状态切换命令
func (c *BaseComponent) PublishStateTransition(taskID, currentState, targetState string, success bool, message string) error {
	// 构造状态切换命令事件
	event := common.NewEvent(
		"StateTransitionCommand",
		"state.transition.command",
		c.info.Name,
	).WithContext(context.Background())
	// 构建强类型 payload（必须使用 TypedPayload，ProtobufSerializer 只序列化 TypedPayload）
	event.TypedPayload = &api.Event_Component{
		Component: &api.ComponentEventPayload{
			TaskId:        taskID,
			CurrentState:  currentState,
			TargetState:   targetState,
			ComponentName: c.info.Name,
			Success:       success,
			Message:       message,
		},
	}
	// 发布到 EventBus
	return c.bus.Publish("state.transition.command", event)
}
