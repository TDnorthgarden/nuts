package datasource

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

func TestNewMockDataSource(t *testing.T) {
	cfg := &MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
		EventIntervalMs: 100,
	}
	source, err := NewMockDataSource(cfg)
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}
	if source == nil {
		t.Fatal("NewMockDataSource returned nil")
	}
	if source.eventInterval != 100*time.Millisecond {
		t.Errorf("expected 100ms interval, got %v", source.eventInterval)
	}
}

func TestNewMockDataSource_InvalidConfig(t *testing.T) {
	_, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "", Name: "", Enabled: true, BufferSize: 100,
		},
	})
	if err == nil {
		t.Error("expected error for invalid config")
	}
}

func TestMockDataSourceStartStop(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
		EventIntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)
	if err := mock.Start(ctx, eventCh); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !mock.started.Load() {
		t.Error("started should be true after Start")
	}

	select {
	case <-mock.Ready():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Ready")
	}

	if err := mock.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if mock.started.Load() {
		t.Error("started should be false after Stop")
	}
}

func TestMockDataSourceDoubleStart(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	ctx := context.Background()
	eventCh := make(chan *common.Event, 100)

	if err := mock.Start(ctx, eventCh); err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	if err := mock.Start(ctx, eventCh); err == nil {
		t.Error("second Start should return error")
	}

	mock.Stop()
}

func TestMockDataSourceStopWithoutStart(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	if err := mock.Stop(); err != nil {
		t.Errorf("Stop without Start should not error: %v", err)
	}
}

func TestMockDataSourceHealth(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	if err := mock.Health(); err == nil {
		t.Error("Health should return error when not started")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	eventCh := make(chan *common.Event, 100)
	mock.Start(ctx, eventCh)

	if err := mock.Health(); err != nil {
		t.Errorf("Health should pass when started: %v", err)
	}

	mock.Stop()

	if err := mock.Health(); err == nil {
		t.Error("Health should return error after Stop")
	}
}

func TestMockDataSourceGetStats(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	stats := mock.GetStats()
	if stats.Connected {
		t.Error("should not be connected before Start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	eventCh := make(chan *common.Event, 100)
	mock.Start(ctx, eventCh)

	stats = mock.GetStats()
	if !stats.Connected {
		t.Error("should be connected after Start")
	}

	mock.Stop()
}

func TestMockDataSourceEvents(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
		EventIntervalMs: 10,
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	eventCh := make(chan *common.Event, 1000)
	if err := mock.Start(ctx, eventCh); err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	var received atomic.Int64
	done := make(chan struct{})
	go func() {
		for range eventCh {
			received.Add(1)
		}
		close(done)
	}()

	<-ctx.Done()
	mock.Stop()
	close(eventCh)
	<-done

	if received.Load() == 0 {
		t.Error("expected at least one event")
	}

	stats := mock.GetStats()
	t.Logf("Events: received=%d, sent=%d, dropped=%d",
		stats.EventsReceived, stats.EventsSent, stats.EventsDropped)
}

func TestMockDataSourceReady(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	eventCh := make(chan *common.Event, 100)

	readyCh := mock.Ready()
	select {
	case <-readyCh:
		t.Error("Ready should not be closed before Start")
	case <-time.After(10 * time.Millisecond):
	}

	mock.Start(ctx, eventCh)
	select {
	case <-mock.Ready():
	case <-time.After(time.Second):
		t.Fatal("Ready should be closed after Start")
	}

	mock.Stop()
}

func TestMockDataSourceParseConfig(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	err = mock.ParseConfig(map[string]interface{}{
		"event_interval_ms": int64(200),
		"name":              "new-name",
		"type":              "mock-v2",
	})
	if err != nil {
		t.Errorf("ParseConfig failed: %v", err)
	}

	if mock.eventInterval != 200*time.Millisecond {
		t.Errorf("expected 200ms interval, got %v", mock.eventInterval)
	}
	if mock.config.Name != "new-name" {
		t.Errorf("expected name=new-name, got %s", mock.config.Name)
	}
}
