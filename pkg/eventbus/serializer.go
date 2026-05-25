package eventbus

import (
	"fmt"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ProtobufSerializer Protocol Buffers序列化器
//
// 采用核心强类型设计，使用 Protobuf oneof 承载 4 种类型化 payload：
//   - Pod（PodEventPayload）
//   - Task（TaskEventPayload）
//   - Policy（PolicyEventPayload）
//   - Component（ComponentEventPayload）
//
// 预期收益：
//   - 序列化速度提升 6-7x（代码生成 vs encoding/json 反射）
//   - 消息体积减少 ~70%（二进制+字段编号 vs 文本+字段名）
//   - GC 压力显著降低（预分配结构体 vs map[string]interface{} 大量小对象）
type ProtobufSerializer struct{}

// NewProtobufSerializer 创建Protobuf序列化器
func NewProtobufSerializer() *ProtobufSerializer {
	return &ProtobufSerializer{}
}

// Serialize 将 common.Event 序列化为 protobuf 二进制
func (s *ProtobufSerializer) Serialize(event *common.Event) ([]byte, error) {
	pbEvent := &api.Event{
		Id:        event.ID,
		Type:      event.Type,
		Topic:     event.Topic,
		Timestamp: timestamppb.New(event.Timestamp),
		TraceId:   event.TraceID,
		Source:    event.Source,
		Version:   event.Version,
	}

	if event.TypedPayload != nil {
		switch tp := event.TypedPayload.(type) {
		case *api.Event_Pod:
			pbEvent.Payload = tp
		case *api.Event_Task:
			pbEvent.Payload = tp
		case *api.Event_Policy:
			pbEvent.Payload = tp
		case *api.Event_Component:
			pbEvent.Payload = tp
		default:
			return nil, fmt.Errorf("unsupported TypedPayload type: %T", event.TypedPayload)
		}
	}

	if pbEvent.Payload == nil {
		return nil, fmt.Errorf("event has no TypedPayload set")
	}

	return proto.Marshal(pbEvent)
}

// Deserialize 将 protobuf 二进制反序列化为 common.Event
func (s *ProtobufSerializer) Deserialize(data []byte) (*common.Event, error) {
	var pbEvent api.Event
	if err := proto.Unmarshal(data, &pbEvent); err != nil {
		return nil, fmt.Errorf("unmarshal protobuf: %w", err)
	}

	event := &common.Event{
		ID:      pbEvent.Id,
		Type:    pbEvent.Type,
		Topic:   pbEvent.Topic,
		TraceID: pbEvent.TraceId,
		Source:  pbEvent.Source,
		Version: pbEvent.Version,
	}

	if pbEvent.Timestamp != nil {
		event.Timestamp = pbEvent.Timestamp.AsTime()
	}

	// 根据 oneof payload 类型还原 TypedPayload
	switch p := pbEvent.Payload.(type) {
	case *api.Event_Pod:
		event.TypedPayload = p
	case *api.Event_Task:
		event.TypedPayload = p
	case *api.Event_Policy:
		event.TypedPayload = p
	case *api.Event_Component:
		event.TypedPayload = p
	}

	return event, nil
}

// ContentType 返回Content-Type
func (s *ProtobufSerializer) ContentType() string {
	return "application/protobuf"
}
