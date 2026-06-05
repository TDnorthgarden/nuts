package eventlog

import (
	"context"
	"time"
)

// Stage 事件流转阶段
type Stage string

const (
	StageDataSource  Stage = "datasource"    // 数据源采集
	StagePolicyMatch Stage = "policy_match"  // 策略匹配
	StageTaskCreate  Stage = "task_create"   // 任务创建
	StageTaskState   Stage = "task_state"    // 任务状态变更
	StageCommand     Stage = "command"       // 状态转换命令
	StageTimeout     Stage = "timeout"       // 超时处理
	StageArchive     Stage = "archive"       // 归档
)

// EventLogEntry 事件日志条目
type EventLogEntry struct {
	ID            string                 `json:"id"`
	TraceID       string                 `json:"trace_id"`
	EventID       string                 `json:"event_id"`
	Stage         Stage                  `json:"stage"`
	EventType     string                 `json:"event_type"`
	Topic         string                 `json:"topic,omitempty"`
	Source        string                 `json:"source"`
	Timestamp     time.Time              `json:"timestamp"`
	TaskID        string                 `json:"task_id,omitempty"`
	OldState      string                 `json:"old_state,omitempty"`
	NewState      string                 `json:"new_state,omitempty"`
	ComponentName string                 `json:"component_name,omitempty"`
	Success       *bool                  `json:"success,omitempty"`
	Message       string                 `json:"message,omitempty"`
	Payload       map[string]interface{} `json:"payload,omitempty"`
}

// TraceTimeline 追踪时间线
type TraceTimeline struct {
	TraceID  string          `json:"trace_id"`
	Entries  []EventLogEntry `json:"entries"`
	Tasks    []TaskSummary   `json:"tasks"`
	Duration time.Duration   `json:"duration"`
}

// TaskSummary 任务摘要
type TaskSummary struct {
	ID        string    `json:"id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// LogFilter 日志查询过滤器
type LogFilter struct {
	TraceID   string
	TaskID    string
	Stage     Stage
	Source    string
	StartTime *time.Time
	EndTime   *time.Time
	Limit     int
	Offset    int
}

// EventLog 事件日志接口
type EventLog interface {
	// Append 追加事件日志
	Append(ctx context.Context, entry *EventLogEntry) error

	// QueryByTraceID 按 TraceID 查询完整事件链路
	QueryByTraceID(traceID string) (*TraceTimeline, error)

	// QueryByTaskID 按 TaskID 查询关联事件
	QueryByTaskID(taskID string) ([]EventLogEntry, error)

	// Query 按条件查询事件日志
	Query(filter LogFilter) ([]EventLogEntry, error)

	// Cleanup 清理指定时间之前的日志
	Cleanup(before time.Time) (int, error)

	// Close 关闭日志存储
	Close() error
}
