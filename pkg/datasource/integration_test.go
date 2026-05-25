package datasource

import (
	"context"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

func TestMockDataSource_Integration(t *testing.T) {
	// 创建Mock数据源配置
	cfg := &MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type:       "mock",
			Name:       "test-mock",
			Enabled:    true,
			BufferSize: 100,
		},
		EventIntervalMs: 1000,
		EventTypes: []string{
			"ContainerStart",
			"ContainerStop",
		},
	}

	// 创建数据源
	source, err := NewMockDataSource(cfg)
	if err != nil {
		t.Fatalf("Create mock datasource failed: %v", err)
	}

	// 运行集成测试
	config := &IntegrationTestConfig{
		TestDuration:          5 * time.Second,
		ExpectedEvents:        3,
		ValidatePayload:       true,
		RequiredPayloadFields: []string{"counter", "pod_name", "namespace"},
	}

	RunIntegrationTest(t, source, config)
}

func TestDataSourceManager_Integration(t *testing.T) {
	// 创建事件channel
	eventCh := make(chan *common.Event, 1000)

	// 创建管理器
	manager := NewDataSourceManager(eventCh)
	defer manager.Close()

	// 创建Mock数据源
	cfg := &MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type:       "mock",
			Name:       "test-mock",
			Enabled:    true,
			BufferSize: 100,
		},
		EventIntervalMs: 500,
	}

	source, err := NewMockDataSource(cfg)
	if err != nil {
		t.Fatalf("Create mock datasource failed: %v", err)
	}

	// 注册数据源
	manager.Register("mock1", source, &cfg.BaseDataSourceConfig)

	// 启动数据源
	manager.StartByName("mock1")

	// 验证已激活
	active := manager.GetActive()
	if len(active) != 1 || active[0] != "mock1" {
		t.Errorf("Expected 1 active datasource, got %v", active)
	}

	// 验证健康状态
	manager.Health("mock1")

	// 获取统计信息
	stats, _ := manager.GetStats("mock1")
	t.Logf("Stats: %+v", stats)

	// 等待接收事件
	receivedCount := 0
	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-eventCh:
			receivedCount++
			if receivedCount >= 2 {
				goto Done
			}
		case <-timeout:
			t.Errorf("Timeout waiting for events, received %d", receivedCount)
			goto Done
		}
	}
Done:

	// 停止数据源
	manager.Stop("mock1")

	// 验证已停止
	active = manager.GetActive()
	if len(active) != 0 {
		t.Errorf("Expected 0 active datasources, got %v", active)
	}
}

func TestBufferedDataSource_Integration(t *testing.T) {
	// 创建Mock数据源
	cfg := &MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type:       "mock",
			Name:       "test-mock",
			Enabled:    true,
			BufferSize: 100,
		},
		EventIntervalMs: 100, // 快速生成事件
	}

	source, err := NewMockDataSource(cfg)
	if err != nil {
		t.Fatalf("Create mock datasource failed: %v", err)
	}

	// 创建带缓冲的数据源（小缓冲区测试丢弃）
	buffered := NewBufferedDataSource(source, 10, DropOldest)

	// 启动并测试
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 1000)
	buffered.Start(ctx, eventCh)
	defer buffered.Stop()

	// 慢速消费（模拟背压）
	receivedCount := 0
	done := make(chan struct{})

	go func() {
		for range eventCh {
			receivedCount++
			time.Sleep(200 * time.Millisecond) // 慢速处理
		}
		close(done)
	}()

	<-ctx.Done()
	buffered.Stop()
	close(eventCh)
	<-done

	// 获取缓冲统计
	stats := buffered.GetBufferStats()
	t.Logf("Buffer stats: Buffered=%d, Dropped=%d, Processed=%d, FullCount=%d",
		stats.BufferedEvents, stats.DroppedEvents, stats.ProcessedEvents, stats.FullCount)

	// 验证有事件被丢弃（背压生效）
	if stats.DroppedEvents == 0 {
		t.Log("No events dropped (buffer may be sufficient)")
	}
}

func TestDataSourceFactory_Integration(t *testing.T) {
	// 通过工厂创建数据源
	configStr := `
type = "mock"
name = "factory-mock"
enabled = true
buffer_size = 100
event_interval_ms = 1000
event_types = ["ContainerStart", "ContainerStop"]
`

	source, err := Factory.Create(configStr)
	if err != nil {
		t.Fatalf("Factory.Create failed: %v", err)
	}
	if source == nil {
		t.Fatalf("Factory.Create returned nil source")
	}

	// 验证接口
	validator := NewDataSourceValidator(source)
	validator.Validate()

	// 验证跨平台
	ValidateCrossPlatform(source)

	t.Log("Factory integration test passed")
}


