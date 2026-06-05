package common

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type contextKey string

const traceIDKey contextKey = "trace_id"

// ContextWithTraceID 将 TraceID 存入 context
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, traceIDKey, traceID)
}

// TraceIDFromContext 从 context 提取 TraceID
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if id, ok := ctx.Value(traceIDKey).(string); ok {
		return id
	}
	return ""
}

// GenerateTraceID 生成 TraceID
// 优先从 OTel SpanContext 提取，降级为 UUID（去除连字符，统一 32-hex 格式）
func GenerateTraceID(ctx context.Context) string {
	if ctx != nil {
		span := oteltrace.SpanFromContext(ctx)
		if span.SpanContext().IsValid() {
			return span.SpanContext().TraceID().String()
		}
	}
	return strings.ReplaceAll(GenerateUUID(), "-", "")
}

// 事件主题常量
const (
	// TaskEventTopicPrefix 任务事件主题前缀
	TaskEventTopicPrefix = "task.state_changed_"

	// StateTransitionCommandTopic 状态转换命令主题
	StateTransitionCommandTopic = "state.transition.command"
)

// Event 统一事件结构
// 所有模块间传递的事件都使用此结构
type Event struct {
	// ID 事件唯一标识（UUID）
	ID string `json:"id"`

	// Type 事件类型，如"ContainerStart", "policy.matched", "task.state_changed"
	Type string `json:"type"`

	// Topic 事件主题，用于EventBus路由
	Topic string `json:"topic"`

	// Timestamp 事件发生时间
	Timestamp time.Time `json:"timestamp"`

	// TraceID 链路追踪ID，用于跨模块追踪
	TraceID string `json:"trace_id,omitempty"`

	// Source 事件来源，如"nri", "docker", "policy-engine"
	Source string `json:"source,omitempty"`

	// Version 事件版本（用于版本管理）
	Version string `json:"version,omitempty"`

	// TypedPayload 类型化 payload
	// 值为 *api.Event_Pod, *api.Event_Task, *api.Event_Policy, *api.Event_Component 之一
	TypedPayload any `json:"-"`

	// Payload 非类型化 payload（兜底用）
	// 当 TypedPayload 无法满足需求时使用
	Payload map[string]interface{} `json:"payload,omitempty"`

	// Ctx 事件上下文，用于跨处理器传递 context（不参与序列化）
	Ctx context.Context `json:"-"`
}

// WithContext 为事件绑定上下文，自动提取 TraceID
func (e *Event) WithContext(ctx context.Context) *Event {
	if ctx == nil {
		ctx = context.Background()
	}
	e.Ctx = ctx
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		e.TraceID = traceID
	}
	return e
}

// NewEvent 创建新事件
func NewEvent(eventType, topic, source string) *Event {
	return &Event{
		ID:        GenerateUUID(),
		Type:      eventType,
		Topic:     topic,
		Timestamp: time.Now(),
		Source:    source,
		Version:   "1.0",
	}
}

// GetField 从 TypedPayload 中获取字段值
func (e *Event) GetField(key string) (interface{}, bool) {
	if e.TypedPayload != nil {
		switch tp := e.TypedPayload.(type) {
		case *api.Event_Pod:
			if tp.Pod != nil {
				if v, found := getPodField(tp.Pod, key); found {
					return v, true
				}
			}
		case *api.Event_Policy:
			if tp.Policy != nil {
				if v, found := getPolicyField(tp.Policy, key); found {
					return v, true
				}
			}
		case *api.Event_Task:
			if tp.Task != nil {
				if v, found := getTaskField(tp.Task, key); found {
					return v, true
				}
			}
		case *api.Event_Component:
			if tp.Component != nil {
				if v, found := getComponentField(tp.Component, key); found {
					return v, true
				}
			}
		}
	}
	// 兜底：检查顶层 Payload 字段
	if e.Payload != nil {
		if v, ok := e.Payload[key]; ok {
			return v, true
		}
	}
	return nil, false
}

// GetPayloadString 从事件 payload 获取字符串值（优先从 TypedPayload，fallback 到 Payload）
func (e *Event) GetPayloadString(key string) string {
	val, ok := e.GetField(key)
	if !ok {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// GetPayloadMap 从事件 payload 获取 map 值
// 优先返回 map[string]interface{} 类型字段（如 labels）
// 对于 extensions 中的字符串值，尝试解析为 JSON map
func (e *Event) GetPayloadMap(key string) map[string]interface{} {
	val, ok := e.GetField(key)
	if !ok {
		return nil
	}
	switch v := val.(type) {
	case map[string]interface{}:
		return v
	case map[string]string:
		result := make(map[string]interface{}, len(v))
		for k, sv := range v {
			result[k] = sv
		}
		return result
	case string:
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(v), &result); err == nil {
			return result
		}
	}
	return nil
}

// ToPayloadMap 将 TypedPayload 转换为 map[string]interface{}
// 用于策略引擎、任务构建器等需要 map 格式的场景
func (e *Event) ToPayloadMap() map[string]interface{} {
	result := make(map[string]interface{})
	if e.TypedPayload == nil {
		return result
	}

	switch tp := e.TypedPayload.(type) {
	case *api.Event_Pod:
		if tp.Pod != nil {
			if tp.Pod.PodName != "" {
				result["pod_name"] = tp.Pod.PodName
			}
			if tp.Pod.PodNamespace != "" {
				result["pod_namespace"] = tp.Pod.PodNamespace
				result["namespace"] = tp.Pod.PodNamespace
			}
			if tp.Pod.PodUid != "" {
				result["pod_uid"] = tp.Pod.PodUid
			}
			if tp.Pod.PodId != "" {
				result["pod_id"] = tp.Pod.PodId
			}
			if tp.Pod.Labels != nil {
				result["labels"] = tp.Pod.Labels
			}
			if tp.Pod.Extensions != nil {
				for k, v := range tp.Pod.Extensions {
					if _, exists := result[k]; !exists {
						result[k] = v
					}
				}
			}
		}

	case *api.Event_Policy:
		if tp.Policy != nil {
			if tp.Policy.PolicyId != "" {
				result["policy_id"] = tp.Policy.PolicyId
			}
			if tp.Policy.TriggerEventType != "" {
				result["trigger_event_type"] = tp.Policy.TriggerEventType
			}
			if tp.Policy.TriggerEventSource != "" {
				result["trigger_event_source"] = tp.Policy.TriggerEventSource
			}
			if tp.Policy.TriggerEventId != "" {
				result["trigger_event_id"] = tp.Policy.TriggerEventId
			}
			if tp.Policy.Timestamp != 0 {
				result["timestamp"] = tp.Policy.Timestamp
			}
			if tp.Policy.Extensions != nil {
				for k, v := range tp.Policy.Extensions {
					if _, exists := result[k]; !exists {
						result[k] = v
					}
				}
			}
		}

	case *api.Event_Task:
		if tp.Task != nil {
			if tp.Task.TaskId != "" {
				result["task_id"] = tp.Task.TaskId
			}
			if tp.Task.OldState != "" {
				result["old_state"] = tp.Task.OldState
			}
			if tp.Task.NewState != "" {
				result["new_state"] = tp.Task.NewState
			}
			if tp.Task.CurrentState != "" {
				result["current_state"] = tp.Task.CurrentState
			}
			if tp.Task.TargetState != "" {
				result["target_state"] = tp.Task.TargetState
			}
			if tp.Task.Timestamp != 0 {
				result["timestamp"] = tp.Task.Timestamp
			}
			if tp.Task.PolicyId != "" {
				result["policy_id"] = tp.Task.PolicyId
			}
			if tp.Task.TriggerEventType != "" {
				result["trigger_event_type"] = tp.Task.TriggerEventType
			}
			if tp.Task.TriggerEventId != "" {
				result["trigger_event_id"] = tp.Task.TriggerEventId
			}
			if tp.Task.Extensions != nil {
				for k, v := range tp.Task.Extensions {
					if _, exists := result[k]; !exists {
						result[k] = v
					}
				}
			}
		}

	case *api.Event_Component:
		if tp.Component != nil {
			if tp.Component.TaskId != "" {
				result["task_id"] = tp.Component.TaskId
			}
			if tp.Component.CurrentState != "" {
				result["current_state"] = tp.Component.CurrentState
			}
			if tp.Component.TargetState != "" {
				result["target_state"] = tp.Component.TargetState
			}
			if tp.Component.ComponentName != "" {
				result["component"] = tp.Component.ComponentName
			}
			if tp.Component.Success {
				result["success"] = true
			}
			if tp.Component.Message != "" {
				result["message"] = tp.Component.Message
			}
		}
	}

	// Merge untyped Payload (e.g., "command" from rules.json, "action" from policy match)
	// TypedPayload fields take precedence; untyped Payload fills in missing keys.
	if e.Payload != nil {
		for k, v := range e.Payload {
			if _, exists := result[k]; !exists {
				result[k] = v
			}
		}
	}

	return result
}

// getPodField 从 PodEventPayload 中提取字段值
func getPodField(pod *api.PodEventPayload, key string) (interface{}, bool) {
	switch key {
	case "pod_uid":
		return pod.PodUid, pod.PodUid != ""
	case "pod_name":
		return pod.PodName, pod.PodName != ""
	case "pod_namespace", "namespace":
		return pod.PodNamespace, pod.PodNamespace != ""
	case "pod_id":
		return pod.PodId, pod.PodId != ""
	case "labels":
		if pod.Labels != nil {
			return pod.Labels, true
		}
		return nil, false
	default:
		if pod.Extensions != nil {
			if v, exists := pod.Extensions[key]; exists {
				return v, true
			}
		}
		return nil, false
	}
}

// getPolicyField 从 PolicyEventPayload 中提取字段值
func getPolicyField(p *api.PolicyEventPayload, key string) (interface{}, bool) {
	switch key {
	case "policy_id":
		return p.PolicyId, p.PolicyId != ""
	case "trigger_event_type":
		return p.TriggerEventType, p.TriggerEventType != ""
	case "trigger_event_source":
		return p.TriggerEventSource, p.TriggerEventSource != ""
	case "trigger_event_id":
		return p.TriggerEventId, p.TriggerEventId != ""
	case "timestamp":
		return p.Timestamp, p.Timestamp != 0
	default:
		if p.Extensions != nil {
			if v, exists := p.Extensions[key]; exists {
				return v, true
			}
		}
		return nil, false
	}
}

// getTaskField 从 TaskEventPayload 中提取字段值
func getTaskField(t *api.TaskEventPayload, key string) (interface{}, bool) {
	switch key {
	case "task_id":
		return t.TaskId, t.TaskId != ""
	case "old_state":
		return t.OldState, t.OldState != ""
	case "new_state":
		return t.NewState, t.NewState != ""
	case "current_state":
		return t.CurrentState, t.CurrentState != ""
	case "target_state":
		return t.TargetState, t.TargetState != ""
	case "timestamp":
		return t.Timestamp, t.Timestamp != 0
	case "policy_id":
		return t.PolicyId, t.PolicyId != ""
	case "trigger_event_type":
		return t.TriggerEventType, t.TriggerEventType != ""
	case "trigger_event_id":
		return t.TriggerEventId, t.TriggerEventId != ""
	default:
		if t.Extensions != nil {
			if v, exists := t.Extensions[key]; exists {
				return v, true
			}
		}
		return nil, false
	}
}

// getComponentField 从 ComponentEventPayload 中提取字段值
func getComponentField(c *api.ComponentEventPayload, key string) (interface{}, bool) {
	switch key {
	case "task_id":
		return c.TaskId, c.TaskId != ""
	case "current_state":
		return c.CurrentState, c.CurrentState != ""
	case "target_state":
		return c.TargetState, c.TargetState != ""
	case "component_name":
		return c.ComponentName, c.ComponentName != ""
	case "success":
		return c.Success, true // bool 类型，始终返回 true
	case "message":
		return c.Message, c.Message != ""
	default:
		return nil, false
	}
}
