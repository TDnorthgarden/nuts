package eventbus

import (
	"context"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// EventBus 事件总线接口
// 提供发布/订阅机制，支持多种实现（gRPC/Redis/Kafka）
type EventBus interface {
	// Start 启动EventBus
	Start(ctx context.Context) error

	// Stop 停止EventBus
	Stop() error

	// Publish 发布事件到指定主题
	// topic: 事件主题，如"policy.matched", "task.state_changed"
	// event: 事件对象
	// 返回错误如果发布失败
	Publish(topic string, event *common.Event) error

	// Subscribe 订阅指定主题的事件
	// topic: 订阅的主题，支持通配符如"task.*"
	// 返回只读channel用于接收事件
	Subscribe(topic string) <-chan *common.Event

	// Unsubscribe 取消订阅
	// topic: 要取消订阅的主题
	// 返回错误如果取消失败
	Unsubscribe(topic string) error

	// Close 关闭事件总线，释放资源
	Close() error

	// Health 检查事件总线健康状态
	Health() error
}

// EventSerializer 事件序列化接口
// 用于不同传输协议的序列化/反序列化
type EventSerializer interface {
	// Serialize 将Event序列化为字节数组
	Serialize(event *common.Event) ([]byte, error)

	// Deserialize 将字节数组反序列化为Event
	Deserialize(data []byte) (*common.Event, error)

	// ContentType 返回序列化格式标识
	ContentType() string
}
