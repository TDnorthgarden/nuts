package policy

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// EventAccessor Event字段访问器
// 提供对Event payload中字段的便捷访问
type EventAccessor struct {
	event *common.Event
}

// NewEventAccessor 创建Event访问器
func NewEventAccessor(event *common.Event) *EventAccessor {
	return &EventAccessor{event: event}
}

// Get 获取字段值（优先从 TypedPayload，fallback 到 flat map 查找）
func (a *EventAccessor) Get(path string) (interface{}, error) {
	// 尝试从 GetField 直接获取（TypedPayload 强类型路径）
	if val, ok := a.event.GetField(path); ok {
		return val, nil
	}
	// 尝试从扁平 map 中查找
	// 注意: ToPayloadMap() 返回扁平结构 (key="namespace", value="production")，
	// 而传入路径可能包含 "payload." 前缀 (如 "payload.namespace")，
	// 因此需要去除该前缀后再查找。
	payloadMap := a.event.ToPayloadMap()
	cleanPath := strings.TrimPrefix(path, "payload.")
	return a.getFromMap(payloadMap, cleanPath)
}

// GetString 获取字符串值
func (a *EventAccessor) GetString(path string) string {
	val, err := a.Get(path)
	if err != nil {
		return ""
	}
	if s, ok := val.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

// GetInt 获取整数值
func (a *EventAccessor) GetInt(path string) int {
	val, err := a.Get(path)
	if err != nil {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// GetBool 获取布尔值
func (a *EventAccessor) GetBool(path string) bool {
	val, err := a.Get(path)
	if err != nil {
		return false
	}
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// getFromMap 从map中获取嵌套字段
func (a *EventAccessor) getFromMap(data map[string]interface{}, path string) (interface{}, error) {
	if path == "" {
		return data, nil
	}

	parts := strings.Split(path, ".")
	current := interface{}(data)

	for _, part := range parts {
		switch v := current.(type) {
		case map[string]interface{}:
			if val, ok := v[part]; ok {
				current = val
			} else {
				return nil, fmt.Errorf("field not found: %s", part)
			}
		case map[interface{}]interface{}:
			if val, ok := v[part]; ok {
				current = val
			} else {
				return nil, fmt.Errorf("field not found: %s", part)
			}
		default:
			// 尝试通过反射访问字段
			current = a.getFieldByReflection(current, part)
			if current == nil {
				return nil, fmt.Errorf("field not found: %s", part)
			}
		}
	}

	return current, nil
}

// getFieldByReflection 通过反射获取字段
func (a *EventAccessor) getFieldByReflection(obj interface{}, field string) interface{} {
	val := reflect.ValueOf(obj)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	if val.Kind() == reflect.Struct {
		fieldVal := val.FieldByName(field)
		if fieldVal.IsValid() {
			return fieldVal.Interface()
		}
	}

	return nil
}

// ToMap 将Event转换为map供DSL引擎使用
// CEL 引擎需要嵌套结构（如 event.payload.namespace），
// 因此 payload 字段作为嵌套 map 放入，而非展平为 "payload.namespace" 键。
func (a *EventAccessor) ToMap() map[string]interface{} {
	result := make(map[string]interface{})

	// 复制基础字段
	result["id"] = a.event.ID
	result["type"] = a.event.Type
	result["topic"] = a.event.Topic
	result["timestamp"] = a.event.Timestamp.Unix()
	result["source"] = a.event.Source
	result["trace_id"] = a.event.TraceID

	// 将 TypedPayload 作为嵌套的 "payload" map 放入（CEL 需要的结构）
	payloadMap := a.event.ToPayloadMap()
	if len(payloadMap) > 0 {
		result["payload"] = payloadMap
	}

	return result
}

// expandMap 递归展开嵌套map
func (a *EventAccessor) expandMap(data map[string]interface{}, prefix string, result map[string]interface{}) {
	for key, value := range data {
		fullKey := key
		if prefix != "" {
			fullKey = prefix + "." + key
		}

		switch v := value.(type) {
		case map[string]interface{}:
			a.expandMap(v, fullKey, result)
		case map[interface{}]interface{}:
			// 转换为string key的map
			converted := make(map[string]interface{})
			for k, val := range v {
				if strKey, ok := k.(string); ok {
					converted[strKey] = val
				}
			}
			a.expandMap(converted, fullKey, result)
		default:
			result[fullKey] = value
		}
	}
}

// Flatten 扁平化Event payload
// 将嵌套结构转换为单层map，key用点号连接
func (a *EventAccessor) Flatten() map[string]interface{} {
	result := make(map[string]interface{})
	payloadMap := a.event.ToPayloadMap()
	a.expandMap(payloadMap, "", result)
	return result
}

// GetFieldPath 获取字段路径的完整值
// 支持点号分隔的路径，如 "pod.namespace"
func (a *EventAccessor) GetFieldPath(path string) (interface{}, error) {
	return a.Get(path)
}

// HasField 检查字段是否存在
func (a *EventAccessor) HasField(path string) bool {
	_, err := a.Get(path)
	return err == nil
}

// GetAllFields 获取所有字段路径
func (a *EventAccessor) GetAllFields() []string {
	flattened := a.Flatten()
	fields := make([]string, 0, len(flattened))
	for field := range flattened {
		fields = append(fields, field)
	}
	return fields
}

// EventDataBuilder Event数据构建器
// 用于从Event构建DSL引擎可用的数据结构
type EventDataBuilder struct {
	event *common.Event
}

// NewEventDataBuilder 创建数据构建器
func NewEventDataBuilder(event *common.Event) *EventDataBuilder {
	return &EventDataBuilder{event: event}
}

// Build 构建数据map
func (b *EventDataBuilder) Build() map[string]interface{} {
	accessor := NewEventAccessor(b.event)
	return accessor.ToMap()
}

// BuildWithCustom 添加自定义字段
func (b *EventDataBuilder) BuildWithCustom(custom map[string]interface{}) map[string]interface{} {
	data := b.Build()
	for k, v := range custom {
		data[k] = v
	}
	return data
}

// BuildFlattened 构建扁平化数据
func (b *EventDataBuilder) BuildFlattened() map[string]interface{} {
	accessor := NewEventAccessor(b.event)
	flattened := accessor.Flatten()

	// 添加基础字段
	flattened["id"] = b.event.ID
	flattened["type"] = b.event.Type
	flattened["topic"] = b.event.Topic
	flattened["timestamp"] = b.event.Timestamp.Unix()
	flattened["source"] = b.event.Source

	return flattened
}
