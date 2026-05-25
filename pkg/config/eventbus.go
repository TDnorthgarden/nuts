package config

import "time"

// EventBusConfig 事件总线模块配置
type EventBusConfig struct {
	// 订阅者通道缓冲区大小
	SubscriberChannelBufferSize int `toml:"subscriber_channel_buffer_size"`

	// 发布超时时间（秒）
	PublishTimeoutSeconds int `toml:"publish_timeout_seconds"`

	// 最大并发分发器数量
	MaxConcurrentDispatchers int `toml:"max_concurrent_dispatchers"`
}

// DefaultEventBusConfig 返回默认事件总线配置
func DefaultEventBusConfig() *EventBusConfig {
	return &EventBusConfig{
		SubscriberChannelBufferSize: 100,
		PublishTimeoutSeconds:       5,
		MaxConcurrentDispatchers:    10,
	}
}

// GetSubscriberChannelBufferSize 获取订阅者通道缓冲区大小
func (c *EventBusConfig) GetSubscriberChannelBufferSize() int {
	if c.SubscriberChannelBufferSize <= 0 {
		return 100 // 默认值
	}
	return c.SubscriberChannelBufferSize
}

// GetPublishTimeout 获取发布超时时间
func (c *EventBusConfig) GetPublishTimeout() time.Duration {
	if c.PublishTimeoutSeconds <= 0 {
		return 5 * time.Second // 默认值
	}
	return time.Duration(c.PublishTimeoutSeconds) * time.Second
}

// GetMaxConcurrentDispatchers 获取最大并发分发器数量
func (c *EventBusConfig) GetMaxConcurrentDispatchers() int {
	if c.MaxConcurrentDispatchers <= 0 {
		return 10 // 默认值
	}
	return c.MaxConcurrentDispatchers
}
