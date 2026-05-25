package task

import (
	"context"
	"fmt"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// StateMachineEngine 状态机引擎接口
// 负责任务的创建、状态流转管理、命令处理
type StateMachineEngine interface {
	// CreateTask 创建任务并启动状态机
	CreateTask(ctx context.Context, spec TaskSpec) (*Task, error)

	// HandleTransitionCommand 处理状态切换命令（来自外部组件）
	HandleTransitionCommand(ctx context.Context, cmd TransitionCommand) error

	// GetTaskState 获取任务当前状态
	GetTaskState(taskID string) (TaskState, error)

	// GetStateMachineConfig 获取状态机配置（供组件查询）
	GetStateMachineConfig() *StateMachineConfig

	// GetTaskHistory 获取任务历史（追踪用）
	GetTaskHistory(taskID string) ([]StateTransitionRecord, error)

	// SetMetrics 设置度量收集器
	SetMetrics(m common.MetricsRecorder)
}

// TaskSpec 任务创建规范
type TaskSpec struct {
	ID          string
	Name        string
	Description string
	Priority    int
	Metadata    map[string]string
	Event       *common.Event // 可选的事件源，用于PayloadBuilder
}

// TransitionCommand 状态切换命令
type TransitionCommand struct {
	TaskID        string
	CurrentState  TaskState
	TargetState   TaskState
	Result        *CommandResult
	ComponentInfo ComponentInfo
	SetRetryCount *int // 可选：设置任务重试次数（原子化，与状态转换一起提交）
}

// CommandResult 命令执行结果
type CommandResult struct {
	Success bool
	Message string
	Data    map[string]interface{}
}

// ComponentInfo 组件信息
type ComponentInfo struct {
	Name    string
	Version string
	State   string
}

// DefaultStateMachineEngine 默认状态机引擎实现
type DefaultStateMachineEngine struct {
	store                 TaskStore
	config                *StateMachineConfig
	eventBus              eventbus.EventBus
	logger                log.Logger
	payloadBuilderFactory *PayloadBuilderFactory
	metrics               common.MetricsRecorder
}

// NewDefaultStateMachineEngine 创建默认状态机引擎
func NewDefaultStateMachineEngine(store TaskStore, config *StateMachineConfig, eventBus eventbus.EventBus) *DefaultStateMachineEngine {
	return &DefaultStateMachineEngine{
		store:                 store,
		config:                config,
		eventBus:              eventBus,
		logger:                log.GetDefault(),
		payloadBuilderFactory: NewPayloadBuilderFactory(),
	}
}

// SetPayloadBuilderFactory 设置PayloadBuilder工厂
func (e *DefaultStateMachineEngine) SetPayloadBuilderFactory(factory *PayloadBuilderFactory) {
	e.payloadBuilderFactory = factory
}

// SetLogger 设置日志记录器
func (e *DefaultStateMachineEngine) SetLogger(logger log.Logger) {
	e.logger = logger
}

// SetMetrics 设置度量收集器
func (e *DefaultStateMachineEngine) SetMetrics(m common.MetricsRecorder) {
	e.metrics = m
}

// CreateTask 创建任务并启动状态机
func (e *DefaultStateMachineEngine) CreateTask(ctx context.Context, spec TaskSpec) (*Task, error) {
	// 基础 metadata 从 spec 获取
	metadata := spec.Metadata
	if metadata == nil {
		metadata = make(map[string]string)
	}

	// 如果提供了事件，使用 PayloadBuilder 构建额外字段合并到 metadata
	if spec.Event != nil {
		builderName := e.config.PayloadBuilder
		if builderName == "" {
			builderName = "default"
		}
		builder, err := e.payloadBuilderFactory.Get(builderName)
		if err != nil {
			e.logger.Warn("Payload builder not found, using default",
				log.String("builder", builderName), log.Error(err))
			builder = &DefaultPayloadBuilder{}
		}

		builtParams, err := builder.Build(spec.Event, e.config.InitialState)
		if err != nil {
			return nil, fmt.Errorf("build payload: %w", err)
		}

		for k, v := range builtParams {
			metadata[k] = v
		}
	}

	// 创建任务
	task := &Task{
		ID:          spec.ID,
		Name:        spec.Name,
		Description: spec.Description,
		Priority:    spec.Priority,
		Metadata:    metadata,
		// 初始化重试计数
		RetryCount: 0, // 初始重试次数为0
	}

	// 创建任务到存储
	if err := e.store.Create(task); err != nil {
		return nil, fmt.Errorf("store task: %w", err)
	}

	// 设置初始状态
	initialState := TaskState(e.config.InitialState)
	if err := e.store.UpdateStateWithRecord(task.ID, initialState, "system", "task created"); err != nil {
		return nil, fmt.Errorf("set initial state: %w", err)
	}

	// 获取更新后的任务
	task, err := e.store.Get(task.ID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}

	if e.metrics != nil {
		e.metrics.TaskCreated()
	}

	return task, nil
}

// HandleTransitionCommand 处理状态切换命令
func (e *DefaultStateMachineEngine) HandleTransitionCommand(ctx context.Context, cmd TransitionCommand) error {
	// 1. 获取任务（全量副本，用于状态校验）
	task, err := e.store.Get(cmd.TaskID)
	if err != nil {
		return common.WrapError(common.CodeTaskNotFound, err, "get task")
	}

	// 2. 如果任务已在目标状态，仅处理 SetRetryCount 后返回（幂等）
	if task.State == cmd.TargetState {
		if cmd.SetRetryCount != nil {
			_, err := e.store.TransitionState(cmd.TaskID, task.State, "system", "retry count update", nil, false, cmd.SetRetryCount, nil)
			if err != nil {
				return fmt.Errorf("update retry count: %w", err)
			}
		}
		return nil
	}

	// 3. 校验当前状态
	if task.State != cmd.CurrentState {
		return common.NewAppError(common.CodeTaskStateMismatch,
			fmt.Sprintf("expected %s, got %s", cmd.CurrentState, task.State))
	}

	// 4. 校验转换是否允许
	if !e.config.IsTransitionAllowed(string(cmd.CurrentState), string(cmd.TargetState)) {
		return common.NewAppError(common.CodeTaskTransitionDenied,
			fmt.Sprintf("%s -> %s", cmd.CurrentState, cmd.TargetState))
	}

	// 5. 构建触发信息
	triggeredBy := "system"
	if cmd.ComponentInfo.Name != "" {
		triggeredBy = cmd.ComponentInfo.Name
	}
	reason := "state transition command"
	if cmd.Result != nil && cmd.Result.Message != "" {
		reason = cmd.Result.Message
	}

	// 6. 构建发布事件（在 TransitionState postCommit 中发布，状态持久化后通知）
	topic := common.TaskEventTopicPrefix + string(cmd.TargetState)
	event := common.NewEvent("task.state_changed", topic, "state-machine-engine").WithContext(ctx)
	extensions := make(map[string]string)
	for k, v := range task.Metadata {
		if k != "policy_id" && k != "trigger_event_type" && k != "trigger_event_id" {
			extensions[k] = v
		}
	}
	event.TypedPayload = &api.Event_Task{
		Task: &api.TaskEventPayload{
			TaskId:           cmd.TaskID,
			OldState:         string(cmd.CurrentState),
			NewState:         string(cmd.TargetState),
			PolicyId:         task.Metadata["policy_id"],
			TriggerEventType: task.Metadata["trigger_event_type"],
			TriggerEventId:   task.Metadata["trigger_event_id"],
			Extensions:       extensions,
		},
	}

	// 7. 原子化状态转换 + 先发布事件（同一锁周期内）
	now := time.Now()
	var setArchivedAt *time.Time
	clearArchived := false
	if e.config.IsTerminalState(string(cmd.TargetState)) {
		setArchivedAt = &now
	} else if task.ArchivedAt != nil {
		clearArchived = true
	}

	postCommit := func() error {
		if e.eventBus != nil {
			return e.eventBus.Publish(topic, event)
		}
		return nil
	}

	t, err := e.store.TransitionState(cmd.TaskID, cmd.TargetState, triggeredBy, reason, setArchivedAt, clearArchived, cmd.SetRetryCount, postCommit)
	if err != nil {
		if e.metrics != nil {
			e.metrics.TaskError()
		}
		return fmt.Errorf("transition state: %w", err)
	}

	// 8. 指标
	if setArchivedAt != nil {
		if e.metrics != nil {
			e.metrics.TaskArchived()
		}
	} else if clearArchived {
		if e.metrics != nil {
			e.metrics.TaskRetry()
		}
	}

	if e.metrics != nil {
		e.metrics.TaskStateTransition(string(cmd.CurrentState), string(cmd.TargetState))
	}

	_ = t // 事件发布已在 postCommit 中完成
	return nil
}

// GetTaskState 获取任务当前状态
func (e *DefaultStateMachineEngine) GetTaskState(taskID string) (TaskState, error) {
	task, err := e.store.Get(taskID)
	if err != nil {
		return "", fmt.Errorf("get task: %w", err)
	}
	return task.State, nil
}

// GetStateMachineConfig 获取状态机配置
func (e *DefaultStateMachineEngine) GetStateMachineConfig() *StateMachineConfig {
	return e.config
}

// GetTaskHistory 获取任务历史
func (e *DefaultStateMachineEngine) GetTaskHistory(taskID string) ([]StateTransitionRecord, error) {
	return e.store.GetStateHistory(taskID)
}
