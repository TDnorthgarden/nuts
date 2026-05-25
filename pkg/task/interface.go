package task

import (
	"time"
)

// Task 任务定义
type Task struct {
	// ID 任务唯一标识
	ID string `json:"id"`

	// Name 任务名称
	Name string `json:"name"`

	// Description 任务描述
	Description string `json:"description"`

	// Priority 任务优先级（0-100，数字越大优先级越高）
	Priority int `json:"priority"`

	// Version 数据版本号（乐观锁，每次写入递增）
	Version int `json:"version"`

	// RetryCount 重试次数
	RetryCount int `json:"retry_count"`

	// State 任务状态
	State TaskState `json:"state"`

	// Result 任务执行结果
	Result *TaskResult `json:"result,omitempty"`

	// Metadata 元数据
	Metadata map[string]string `json:"metadata"`

	// CreatedAt 创建时间
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt 更新时间
	UpdatedAt time.Time `json:"updated_at"`

	// StartedAt 开始时间
	StartedAt *time.Time `json:"started_at,omitempty"`

	// CompletedAt 完成时间
	CompletedAt *time.Time `json:"completed_at,omitempty"`

	// StateHistory 状态历史记录
	StateHistory []StateTransitionRecord `json:"state_history,omitempty"`

	// StateUpdatedAt 状态更新时间（用于超时检测）
	StateUpdatedAt time.Time `json:"state_updated_at"`

	// TimeoutAt 预计超时时间
	TimeoutAt *time.Time `json:"timeout_at,omitempty"`

	// ArchiveEligible 是否可以归档
	ArchiveEligible bool `json:"archive_eligible"`

	// ArchivedAt 归档时间。终端状态的 Task 设为此值，重试时清空为 nil。
	// engineTaskStore.List 默认过滤 ArchiveAt != nil 的记录，
	// 当 TaskFilter.IncludeArchived=true 时返回全部。
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// StateTransitionRecord 状态转换历史记录
type StateTransitionRecord struct {
	// From 源状态
	From TaskState `json:"from"`

	// To 目标状态
	To TaskState `json:"to"`

	// Timestamp 转换时间戳
	Timestamp time.Time `json:"timestamp"`

	// TriggeredBy 触发来源（组件名称或系统）
	TriggeredBy string `json:"triggered_by"`

	// Reason 转换原因/说明
	Reason string `json:"reason,omitempty"`

	// Attempt 当前状态的尝试次数
	Attempt int `json:"attempt"`
}

// TaskState 任务状态
type TaskState string

const (
	// TaskStatePending 待执行
	TaskStatePending TaskState = "pending"
	// TaskStateProcessing 处理中（状态机中的执行阶段）
	TaskStateProcessing TaskState = "processing"
	// TaskStatePaused 已暂停
	TaskStatePaused TaskState = "paused"
	// TaskStateCompleted 已完成
	TaskStateCompleted TaskState = "completed"
	// TaskStateFailed 执行失败
	TaskStateFailed TaskState = "failed"
	// TaskStateCancelled 已取消
	TaskStateCancelled TaskState = "cancelled"
	// TaskStateTimeout 超时
	TaskStateTimeout TaskState = "timeout"
)

// TaskResult 任务执行结果
type TaskResult struct {
	// Success 是否成功
	Success bool `json:"success"`

	// Output 输出内容
	Output string `json:"output,omitempty"`

	// Error 错误信息
	Error string `json:"error,omitempty"`

	// ExitCode 退出码
	ExitCode int `json:"exit_code,omitempty"`

	// Duration 执行时长（纳秒）
	Duration int64 `json:"duration,omitempty"`
}

// TaskFilter 任务过滤器
type TaskFilter struct {
	// State 状态过滤
	State TaskState

	// Priority 优先级过滤
	Priority *int

	// CreatedAfter 创建时间过滤
	CreatedAfter *time.Time

	// CreatedBefore 创建时间过滤
	CreatedBefore *time.Time

	// Limit 限制数量
	Limit int

	// Offset 偏移量
	Offset int

	// IncludeArchived 是否同时检索已归档的任务。默认 false（只返回活跃任务）。
	IncludeArchived bool
}

// TaskStore 任务存储接口
type TaskStore interface {
	// Get 获取任务
	Get(id string) (*Task, error)

	// Create 创建任务
	Create(task *Task) error

	// Update 更新任务
	Update(task *Task) error

	// Delete 删除任务
	Delete(id string) error

	// List 列出任务
	List(filter TaskFilter) ([]*Task, error)

	// Count 获取任务数量
	Count(filter TaskFilter) (int, error)

	// UpdateState 更新任务状态
	UpdateState(id string, state TaskState) error

	// UpdateStateWithRecord 更新任务状态并记录详细信息
	UpdateStateWithRecord(id string, state TaskState, triggeredBy, reason string) error

	// TransitionState 原子化状态转换：在同一锁周期内完成状态、历史、ArchivedAt、RetryCount 更新。
	// setArchivedAt: 非 nil 则设为此值（用于归档终态任务）；clearArchived: true 则清空；两者都不则不变。
	// setRetryCount: 非 nil 则更新 RetryCount。
	// postCommit: 非 nil 时在状态持久化成功后调用（仍持有锁），用于发布事件等通知操作。失败不影响状态转换结果。
	TransitionState(id string, newState TaskState, triggeredBy, reason string, setArchivedAt *time.Time, clearArchived bool, setRetryCount *int, postCommit func() error) (*Task, error)

	// GetStateHistory 获取任务状态历史
	GetStateHistory(id string) ([]StateTransitionRecord, error)

	// UpdateResult 更新任务结果
	UpdateResult(id string, result *TaskResult) error
}
