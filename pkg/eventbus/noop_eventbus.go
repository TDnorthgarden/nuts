package eventbus

import (
	"context"
	"fmt"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// NoopEventBus 空操作 EventBus 实现
// 所有方法均不执行实际操作，适用于独立模式（无远程组件）
type NoopEventBus struct{}

// NewNoopEventBus 创建空操作 EventBus
func NewNoopEventBus() *NoopEventBus {
	return &NoopEventBus{}
}

func (b *NoopEventBus) Start(ctx context.Context) error { return nil }

func (b *NoopEventBus) Stop() error { return nil }

func (b *NoopEventBus) Publish(topic string, event *common.Event) error { return nil }

func (b *NoopEventBus) Subscribe(topic string) <-chan *common.Event {
	ch := make(chan *common.Event)
	close(ch)
	return ch
}

func (b *NoopEventBus) Unsubscribe(topic string) error { return nil }

func (b *NoopEventBus) Close() error { return nil }

func (b *NoopEventBus) Health() error { return nil }

// NoopEventBusConfig NoopEventBus 配置（无需配置）
type NoopEventBusConfig struct{}

// ParseNoopConfig 解析 NoopEventBus 配置
func ParseNoopConfig(config interface{}) (interface{}, error) {
	return &NoopEventBusConfig{}, nil
}

// ValidateNoopConfig 验证 NoopEventBus 配置
func ValidateNoopConfig(cfg interface{}) error {
	_, ok := cfg.(*NoopEventBusConfig)
	if !ok {
		return fmt.Errorf("invalid config type")
	}
	return nil
}

func init() {
	Factory.Register(
		"noop",
		ParseNoopConfig,
		ValidateNoopConfig,
		func(cfg interface{}) (EventBus, error) {
			return NewNoopEventBus(), nil
		},
	)
}
