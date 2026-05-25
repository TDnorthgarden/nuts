package datasource

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// DataSourceValidator 数据源接口验证器
// 用于验证DataSource实现是否符合接口规范
type DataSourceValidator struct {
	source DataSource
}

// NewDataSourceValidator 创建验证器
func NewDataSourceValidator(source DataSource) *DataSourceValidator {
	return &DataSourceValidator{source: source}
}

// Validate 验证数据源实现
func (v *DataSourceValidator) Validate() error {
	// 1. 验证Start/Stop生命周期
	if err := v.validateLifecycle(); err != nil {
		return fmt.Errorf("lifecycle validation failed: %w", err)
	}

	// 2. 验证Health接口
	if err := v.validateHealth(); err != nil {
		return fmt.Errorf("health validation failed: %w", err)
	}

	// 3. 验证GetStats接口
	if err := v.validateStats(); err != nil {
		return fmt.Errorf("stats validation failed: %w", err)
	}

	return nil
}

// validateLifecycle 验证生命周期
func (v *DataSourceValidator) validateLifecycle() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)

	// 启动
	if err := v.source.Start(ctx, eventCh); err != nil {
		return fmt.Errorf("start failed: %w", err)
	}

	// 等待数据源就绪
	select {
	case <-v.source.Ready():
		// 数据源已就绪
	case <-time.After(5 * time.Second):
		return fmt.Errorf("data source not ready within timeout")
	}

	// 停止
	if err := v.source.Stop(); err != nil {
		return fmt.Errorf("stop failed: %w", err)
	}

	return nil
}

// validateHealth 验证健康检查
func (v *DataSourceValidator) validateHealth() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)

	// 启动
	if err := v.source.Start(ctx, eventCh); err != nil {
		return err
	}
	defer v.source.Stop()

	// 执行健康检查
	if err := v.source.Health(); err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}

	return nil
}

// validateStats 验证统计信息
func (v *DataSourceValidator) validateStats() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)

	// 启动
	if err := v.source.Start(ctx, eventCh); err != nil {
		return err
	}
	defer v.source.Stop()

	// 获取统计信息
	stats := v.source.GetStats()
	if stats == nil {
		return fmt.Errorf("GetStats returned nil")
	}

	// 验证统计字段
	if stats.EventsReceived < 0 {
		return fmt.Errorf("invalid EventsReceived: %d", stats.EventsReceived)
	}
	if stats.EventsSent < 0 {
		return fmt.Errorf("invalid EventsSent: %d", stats.EventsSent)
	}
	if stats.EventsDropped < 0 {
		return fmt.Errorf("invalid EventsDropped: %d", stats.EventsDropped)
	}

	return nil
}

// TestDataSourceInterface 测试函数（用于单元测试）
func TestDataSourceInterface(t *testing.T, source DataSource) {
	validator := NewDataSourceValidator(source)

	if err := validator.Validate(); err != nil {
		t.Fatalf("DataSource validation failed: %v", err)
	}
}

// ValidateCrossPlatform 验证跨平台兼容性
func ValidateCrossPlatform(source DataSource) error {
	// 验证数据源不依赖平台特定功能
	// Mock数据源应该跨平台兼容

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)

	// 启动
	if err := source.Start(ctx, eventCh); err != nil {
		return fmt.Errorf("cross-platform start failed: %w", err)
	}
	defer source.Stop()

	// 验证健康检查
	if err := source.Health(); err != nil {
		return fmt.Errorf("cross-platform health check failed: %w", err)
	}

	return nil
}

// IntegrationTestConfig 集成测试配置
type IntegrationTestConfig struct {
	// TestDuration 测试持续时间
	TestDuration time.Duration

	// ExpectedEvents 期望的事件数量
	ExpectedEvents int

	// ValidatePayload 是否验证payload
	ValidatePayload bool

	// RequiredPayloadFields 必需的payload字段
	RequiredPayloadFields []string
}

// RunIntegrationTest 运行集成测试
func RunIntegrationTest(t *testing.T, source DataSource, config *IntegrationTestConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), config.TestDuration)
	defer cancel()

	eventCh := make(chan *common.Event, 1000)

	// 启动数据源
	if err := source.Start(ctx, eventCh); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// 收集事件
	events := make([]*common.Event, 0)
	done := make(chan struct{})

	go func() {
		for event := range eventCh {
			events = append(events, event)
		}
		close(done)
	}()

	// 等待测试结束
	<-ctx.Done()
	source.Stop()
	close(eventCh)
	<-done

	// 验证事件数量
	if len(events) < config.ExpectedEvents {
		t.Errorf("Expected at least %d events, got %d", config.ExpectedEvents, len(events))
	}

	// 验证payload（优先从 TypedPayload，fallback 到 Payload）
	if config.ValidatePayload {
		for _, event := range events {
			for _, field := range config.RequiredPayloadFields {
				if _, exists := event.GetField(field); !exists {
					t.Errorf("Event missing required field: %s", field)
				}
			}
		}
	}

	// 验证统计信息
	stats := source.GetStats()
	t.Logf("Stats: Received=%d, Sent=%d, Dropped=%d",
		stats.EventsReceived, stats.EventsSent, stats.EventsDropped)
}
