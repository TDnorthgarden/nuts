package task

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// PayloadBuilder 接口定义
type PayloadBuilder interface {
	// Build 从事件构建任务参数字符串 map
	Build(event *common.Event, state string) (map[string]string, error)

	// Name 返回构建器名称
	Name() string
}

// PayloadBuilderFactory PayloadBuilder 工厂
type PayloadBuilderFactory struct {
	builders map[string]PayloadBuilder
	mu       sync.RWMutex
}

// NewPayloadBuilderFactory 创建 PayloadBuilder 工厂
func NewPayloadBuilderFactory() *PayloadBuilderFactory {
	factory := &PayloadBuilderFactory{
		builders: make(map[string]PayloadBuilder),
	}
	if err := factory.Register(&DefaultPayloadBuilder{}); err != nil {
		panic("payload_builder: register default: " + err.Error())
	}
	if err := factory.Register(&ContainerPayloadBuilder{}); err != nil {
		panic("payload_builder: register container: " + err.Error())
	}
	if err := factory.Register(&NRIEventPayloadBuilder{}); err != nil {
		panic("payload_builder: register nri: " + err.Error())
	}
	if err := factory.Register(&TimestampPayloadBuilder{}); err != nil {
		panic("payload_builder: register timestamp: " + err.Error())
	}
	return factory
}

// Register 注册 PayloadBuilder
func (f *PayloadBuilderFactory) Register(builder PayloadBuilder) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	name := builder.Name()
	if _, exists := f.builders[name]; exists {
		return fmt.Errorf("payload builder %s already registered", name)
	}

	f.builders[name] = builder
	return nil
}

// Get 获取指定名称的 PayloadBuilder
func (f *PayloadBuilderFactory) Get(name string) (PayloadBuilder, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()

	builder, exists := f.builders[name]
	if !exists {
		return nil, fmt.Errorf("payload builder %s not found", name)
	}

	return builder, nil
}

// List 获取所有已注册的构建器名称
func (f *PayloadBuilderFactory) List() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	names := make([]string, 0, len(f.builders))
	for name := range f.builders {
		names = append(names, name)
	}
	return names
}

// toStringMap 将 map[string]interface{} 转为 map[string]string
func toStringMap(m map[string]interface{}) map[string]string {
	result := make(map[string]string, len(m))
	for k, v := range m {
		switch val := v.(type) {
		case string:
			result[k] = val
		case int64:
			result[k] = strconv.FormatInt(val, 10)
		case float64:
			result[k] = strconv.FormatFloat(val, 'f', -1, 64)
		case bool:
			result[k] = strconv.FormatBool(val)
		default:
			result[k] = fmt.Sprintf("%v", val)
		}
	}
	return result
}

// DefaultPayloadBuilder 默认构建器，直接使用事件 payload
type DefaultPayloadBuilder struct{}

func (b *DefaultPayloadBuilder) Build(event *common.Event, state string) (map[string]string, error) {
	if event == nil {
		return make(map[string]string), nil
	}
	return toStringMap(event.ToPayloadMap()), nil
}

func (b *DefaultPayloadBuilder) Name() string {
	return "default"
}

// ContainerPayloadBuilder 容器相关构建器
type ContainerPayloadBuilder struct{}

func (b *ContainerPayloadBuilder) Build(event *common.Event, state string) (map[string]string, error) {
	if event == nil {
		return make(map[string]string), nil
	}

	params := make(map[string]string)

	if podName, ok := event.GetField("pod_name"); ok {
		if s, ok := podName.(string); ok {
			params["container_name"] = s
		}
	}
	if podNamespace, ok := event.GetField("pod_namespace"); ok {
		if s, ok := podNamespace.(string); ok {
			params["namespace"] = s
		}
	}
	if podUID, ok := event.GetField("pod_uid"); ok {
		if s, ok := podUID.(string); ok {
			params["pod_uid"] = s
		}
	}
	if podID, ok := event.GetField("pod_id"); ok {
		if s, ok := podID.(string); ok {
			params["pod_id"] = s
		}
	}

	return params, nil
}

func (b *ContainerPayloadBuilder) Name() string {
	return "container"
}

// NRIEventPayloadBuilder NRI事件构建器
type NRIEventPayloadBuilder struct{}

func (b *NRIEventPayloadBuilder) Build(event *common.Event, state string) (map[string]string, error) {
	if event == nil {
		return make(map[string]string), nil
	}

	params := toStringMap(event.ToPayloadMap())

	params["event_type"] = event.Type
	params["event_source"] = event.Source
	params["event_id"] = event.ID

	return params, nil
}

func (b *NRIEventPayloadBuilder) Name() string {
	return "nri"
}

// TimestampPayloadBuilder 时间戳填充构建器
type TimestampPayloadBuilder struct{}

func (b *TimestampPayloadBuilder) Build(event *common.Event, state string) (map[string]string, error) {
	now := time.Now()

	if event == nil {
		return map[string]string{
			"timestamp":       strconv.FormatInt(now.Unix(), 10),
			"timestamp_human": now.Format(time.RFC3339),
		}, nil
	}

	params := toStringMap(event.ToPayloadMap())

	params["timestamp"] = strconv.FormatInt(now.Unix(), 10)
	params["timestamp_human"] = now.Format(time.RFC3339)
	params["event_timestamp"] = strconv.FormatInt(event.Timestamp.Unix(), 10)
	params["event_timestamp_human"] = event.Timestamp.Format(time.RFC3339)

	return params, nil
}

func (b *TimestampPayloadBuilder) Name() string {
	return "timestamp"
}
