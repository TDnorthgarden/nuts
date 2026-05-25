package datasource

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerd/containerd"
	containerdEvents "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/events"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/typeurl/v2"
	nutsapi "github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// ContainerdDataSource containerd 数据源实现
// 通过 containerd Go Client SDK 的 Subscribe() API 订阅容器生命周期事件
type ContainerdDataSource struct {
	config *ContainerdConfig
	logger log.Logger

	// containerd 客户端
	client *containerd.Client

	// 事件通道
	eventCh chan<- *common.Event

	// 生命周期管理
	ownCtx  context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Bool
	readyCh chan struct{}

	// 统计
	startTime      time.Time
	eventsReceived atomic.Int64
	eventsSent     atomic.Int64
	eventsDropped  atomic.Int64
}

// NewContainerdDataSource 创建 containerd 数据源
func NewContainerdDataSource(config *ContainerdConfig) (*ContainerdDataSource, error) {
	if config == nil {
		config = &ContainerdConfig{
			SocketPath: "/run/containerd/containerd.sock",
			Namespace:  "k8s.io",
			Events:     []string{"TaskCreate", "TaskStart", "TaskExit", "TaskDelete"},
			BufferSize: 1000,
		}
	}

	return &ContainerdDataSource{
		config:  config,
		logger:  log.GetDefault(),
		readyCh: make(chan struct{}),
	}, nil
}

// SetLogger 设置日志记录器
func (c *ContainerdDataSource) SetLogger(logger log.Logger) {
	c.logger = logger
}

// ParseConfig 解析配置
func (c *ContainerdDataSource) ParseConfig(config map[string]interface{}) error {
	if socketPath, ok := config["socket_path"].(string); ok {
		c.config.SocketPath = socketPath
	}
	if namespace, ok := config["namespace"].(string); ok {
		c.config.Namespace = namespace
	}
	if events, ok := config["events"].([]string); ok {
		c.config.Events = events
	}
	if bufferSize, ok := config["buffer_size"].(int); ok {
		c.config.BufferSize = bufferSize
	}

	return c.config.Validate()
}

// Start 启动数据源
func (c *ContainerdDataSource) Start(ctx context.Context, eventCh chan<- *common.Event) error {
	if !c.started.CompareAndSwap(false, true) {
		return fmt.Errorf("containerd data source is already running")
	}

	c.ownCtx, c.stop = context.WithCancel(ctx)
	c.eventCh = eventCh
	c.startTime = time.Now()

	c.logger.Info("Starting containerd data source",
		log.String("socket_path", c.config.SocketPath),
		log.String("namespace", c.config.Namespace))

	// 创建 containerd 客户端
	client, err := containerd.New(
		c.config.SocketPath,
		containerd.WithDefaultNamespace(c.config.Namespace),
	)
	if err != nil {
		c.stop()
		c.started.Store(false)
		c.logger.Error("Failed to create containerd client", log.Error(err))
		return fmt.Errorf("failed to create containerd client: %w", err)
	}
	c.client = client

	c.logger.Info("Containerd client created successfully")

	// 启动事件循环
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				c.logger.Error("Containerd event loop panic", log.Any("recover", r))
			}
		}()
		c.runEventLoop()
	}()

	c.logger.Info("Containerd data source started successfully")
	return nil
}

// Stop 停止 containerd 数据源
func (c *ContainerdDataSource) Stop() error {
	if !c.started.CompareAndSwap(true, false) {
		return nil
	}

	c.logger.Info("Stopping containerd data source")

	// 取消上下文，通知 goroutine 退出
	c.stop()

	// 等待 goroutine 结束，但设置超时防止无限等待
	waitCh := make(chan struct{})
	go func() {
		c.wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		c.logger.Info("Containerd data source stopped gracefully")
	case <-time.After(5 * time.Second):
		c.logger.Warn("Containerd data source stop timeout, forcing exit")
	}

	// 关闭客户端连接
	if c.client != nil {
		if err := c.client.Close(); err != nil {
			c.logger.Warn("Failed to close containerd client", log.Error(err))
		}
		c.client = nil
	}

	c.logger.Info("Containerd data source stopped")
	return nil
}

// Health 健康检查
func (c *ContainerdDataSource) Health() error {
	if !c.started.Load() {
		return fmt.Errorf("containerd data source is not running")
	}

	if c.client == nil {
		return fmt.Errorf("containerd client is not initialized")
	}

	return nil
}

// GetStats 获取数据源统计信息
func (c *ContainerdDataSource) GetStats() *DataSourceStats {
	return &DataSourceStats{
		EventsReceived: c.eventsReceived.Load(),
		EventsSent:     c.eventsSent.Load(),
		EventsDropped:  c.eventsDropped.Load(),
		LastEventTime:  time.Now(),
		Connected:      c.started.Load(),
		Uptime:         time.Since(c.startTime),
	}
}

// Ready 返回就绪信号通道
func (c *ContainerdDataSource) Ready() <-chan struct{} {
	return c.readyCh
}

// buildFilters 构建事件订阅过滤器
func (c *ContainerdDataSource) buildFilters() []string {
	filters := make([]string, 0, len(c.config.Events))
	for _, eventType := range c.config.Events {
		switch eventType {
		case "TaskCreate":
			filters = append(filters, `topic=="/tasks/create"`)
		case "TaskStart":
			filters = append(filters, `topic=="/tasks/start"`)
		case "TaskExit":
			filters = append(filters, `topic=="/tasks/exit"`)
		case "TaskDelete":
			filters = append(filters, `topic=="/tasks/delete"`)
		case "TaskPause":
			filters = append(filters, `topic=="/tasks/paused"`)
		case "TaskResume":
			filters = append(filters, `topic=="/tasks/resumed"`)
		case "ContainerCreate":
			filters = append(filters, `topic=="/containers/create"`)
		case "ContainerDelete":
			filters = append(filters, `topic=="/containers/delete"`)
		}
	}
	return filters
}

// runEventLoop 运行事件循环，包含重连逻辑
func (c *ContainerdDataSource) runEventLoop() {
	c.logger.Info("Containerd event loop started")

	backoff := 1 * time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-c.ownCtx.Done():
			c.logger.Info("Containerd event loop stopped")
			return
		default:
		}

		// 创建带 namespace 的上下文
		nsCtx := namespaces.WithNamespace(c.ownCtx, c.config.Namespace)

		// 订阅事件
		filters := c.buildFilters()
		c.logger.Info("Subscribing to containerd events",
			log.String("filters", fmt.Sprintf("%v", filters)))

		eventStream, errStream := c.client.Subscribe(nsCtx, filters...)

		// 处理事件流
		c.processEventStream(eventStream, errStream)

		// 检查是否需要退出
		select {
		case <-c.ownCtx.Done():
			return
		default:
		}

		// 重连退避
		c.logger.Warn("Containerd event stream disconnected, reconnecting...",
			log.String("backoff", backoff.String()))

		select {
		case <-c.ownCtx.Done():
			return
		case <-time.After(backoff):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// processEventStream 处理事件流
func (c *ContainerdDataSource) processEventStream(eventStream <-chan *events.Envelope, errStream <-chan error) {
	for {
		select {
		case <-c.ownCtx.Done():
			return

		case err, ok := <-errStream:
			if !ok {
				// 错误通道关闭
				return
			}
			if err != nil {
				c.logger.Error("Containerd event stream error", log.Error(err))
				return
			}

		case envelope, ok := <-eventStream:
			if !ok {
				// 事件通道关闭
				return
			}

			c.eventsReceived.Add(1)

			event := c.handleEnvelope(envelope)
			if event == nil {
				continue
			}

			// 发送到 EventBus
			select {
			case c.eventCh <- event:
				c.eventsSent.Add(1)
			case <-c.ownCtx.Done():
				return
			default:
				c.eventsDropped.Add(1)
				c.logger.Warn("Event dropped due to full channel",
					log.String("event_type", event.Type))
			}
		}
	}
}

// handleEnvelope 处理 containerd 事件信封，转换为 common.Event
func (c *ContainerdDataSource) handleEnvelope(envelope *events.Envelope) *common.Event {
	if envelope == nil || envelope.Event == nil {
		return nil
	}

	// 解码事件
	decoded, err := typeurl.UnmarshalAny(envelope.Event)
	if err != nil {
		c.logger.Warn("Failed to unmarshal containerd event",
			log.String("topic", envelope.Topic),
			log.Error(err))
		return nil
	}

	// 根据 topic 分发处理
	switch envelope.Topic {
	case "/tasks/create":
		return c.handleTaskCreate(decoded)
	case "/tasks/start":
		return c.handleTaskStart(decoded)
	case "/tasks/exit":
		return c.handleTaskExit(decoded)
	case "/tasks/delete":
		return c.handleTaskDelete(decoded)
	case "/tasks/paused":
		return c.handleTaskPaused(decoded)
	case "/tasks/resumed":
		return c.handleTaskResumed(decoded)
	case "/containers/create":
		return c.handleContainerCreate(decoded)
	case "/containers/delete":
		return c.handleContainerDelete(decoded)
	default:
		c.logger.Debug("Unknown containerd event topic",
			log.String("topic", envelope.Topic))
		return nil
	}
}

// handleTaskCreate 处理 TaskCreate 事件
func (c *ContainerdDataSource) handleTaskCreate(decoded interface{}) *common.Event {
	taskCreate, ok := decoded.(*containerdEvents.TaskCreate)
	if !ok {
		return nil
	}

	extensions := map[string]string{
		"container_id": taskCreate.ContainerID,
		"bundle":       taskCreate.Bundle,
		"pid":          strconv.FormatUint(uint64(taskCreate.Pid), 10),
	}

	if taskCreate.Checkpoint != "" {
		extensions["checkpoint"] = taskCreate.Checkpoint
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-task-create-%s-%d", taskCreate.ContainerID, time.Now().UnixNano()),
		Type:      "TaskCreate",
		Topic:     "TaskCreate",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: extensions,
			},
		},
	}
}

// handleTaskStart 处理 TaskStart 事件
func (c *ContainerdDataSource) handleTaskStart(decoded interface{}) *common.Event {
	taskStart, ok := decoded.(*containerdEvents.TaskStart)
	if !ok {
		return nil
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-task-start-%s-%d", taskStart.ContainerID, time.Now().UnixNano()),
		Type:      "TaskStart",
		Topic:     "TaskStart",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: map[string]string{
					"container_id": taskStart.ContainerID,
					"pid":          strconv.FormatUint(uint64(taskStart.Pid), 10),
				},
			},
		},
	}
}

// handleTaskExit 处理 TaskExit 事件
func (c *ContainerdDataSource) handleTaskExit(decoded interface{}) *common.Event {
	taskExit, ok := decoded.(*containerdEvents.TaskExit)
	if !ok {
		return nil
	}

	extensions := map[string]string{
		"container_id": taskExit.ContainerID,
		"exit_status":  strconv.FormatUint(uint64(taskExit.ExitStatus), 10),
		"pid":          strconv.FormatUint(uint64(taskExit.Pid), 10),
	}

	if taskExit.ExitedAt != nil {
		extensions["exited_at"] = taskExit.ExitedAt.AsTime().Format(time.RFC3339Nano)
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-task-exit-%s-%d", taskExit.ContainerID, time.Now().UnixNano()),
		Type:      "TaskExit",
		Topic:     "TaskExit",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: extensions,
			},
		},
	}
}

// handleTaskDelete 处理 TaskDelete 事件
func (c *ContainerdDataSource) handleTaskDelete(decoded interface{}) *common.Event {
	taskDelete, ok := decoded.(*containerdEvents.TaskDelete)
	if !ok {
		return nil
	}

	extensions := map[string]string{
		"container_id": taskDelete.ContainerID,
		"pid":          strconv.FormatUint(uint64(taskDelete.Pid), 10),
		"exit_status":  strconv.FormatUint(uint64(taskDelete.ExitStatus), 10),
	}

	if taskDelete.ExitedAt != nil {
		extensions["exited_at"] = taskDelete.ExitedAt.AsTime().Format(time.RFC3339Nano)
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-task-delete-%s-%d", taskDelete.ContainerID, time.Now().UnixNano()),
		Type:      "TaskDelete",
		Topic:     "TaskDelete",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: extensions,
			},
		},
	}
}

// handleTaskPaused 处理 TaskPaused 事件
func (c *ContainerdDataSource) handleTaskPaused(decoded interface{}) *common.Event {
	taskPaused, ok := decoded.(*containerdEvents.TaskPaused)
	if !ok {
		return nil
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-task-paused-%s-%d", taskPaused.ContainerID, time.Now().UnixNano()),
		Type:      "TaskPause",
		Topic:     "TaskPause",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: map[string]string{
					"container_id": taskPaused.ContainerID,
				},
			},
		},
	}
}

// handleTaskResumed 处理 TaskResumed 事件
func (c *ContainerdDataSource) handleTaskResumed(decoded interface{}) *common.Event {
	taskResumed, ok := decoded.(*containerdEvents.TaskResumed)
	if !ok {
		return nil
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-task-resumed-%s-%d", taskResumed.ContainerID, time.Now().UnixNano()),
		Type:      "TaskResume",
		Topic:     "TaskResume",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: map[string]string{
					"container_id": taskResumed.ContainerID,
				},
			},
		},
	}
}

// handleContainerCreate 处理 ContainerCreate 事件
func (c *ContainerdDataSource) handleContainerCreate(decoded interface{}) *common.Event {
	containerCreate, ok := decoded.(*containerdEvents.ContainerCreate)
	if !ok {
		return nil
	}

	extensions := map[string]string{
		"container_id": containerCreate.ID,
		"image":        containerCreate.Image,
	}

	if containerCreate.Runtime != nil {
		extensions["runtime_name"] = containerCreate.Runtime.Name
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-container-create-%s-%d", containerCreate.ID, time.Now().UnixNano()),
		Type:      "ContainerCreate",
		Topic:     "ContainerCreate",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: extensions,
			},
		},
	}
}

// handleContainerDelete 处理 ContainerDelete 事件
func (c *ContainerdDataSource) handleContainerDelete(decoded interface{}) *common.Event {
	containerDelete, ok := decoded.(*containerdEvents.ContainerDelete)
	if !ok {
		return nil
	}

	return &common.Event{
		ID:        fmt.Sprintf("containerd-container-delete-%s-%d", containerDelete.ID, time.Now().UnixNano()),
		Type:      "ContainerDelete",
		Topic:     "ContainerDelete",
		Timestamp: time.Now(),
		Source:    "containerd",
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				Extensions: map[string]string{
					"container_id": containerDelete.ID,
				},
			},
		},
	}
}
