package datasource

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

func TestNewBufferedDataSource(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)
	if buf == nil {
		t.Fatal("NewBufferedDataSource returned nil")
	}
	if buf.source != mock {
		t.Error("source not set correctly")
	}
	if buf.bufferSize != 10 {
		t.Errorf("expected bufferSize 10, got %d", buf.bufferSize)
	}
	if buf.dropPolicy != DropOldest {
		t.Errorf("expected DropOldest, got %v", buf.dropPolicy)
	}
	if buf.started.Load() {
		t.Error("started should be false")
	}
}

func TestBufferedDataSourceStartStop(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
		EventIntervalMs: 50,
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)
	err = buf.Start(ctx, eventCh)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	if !buf.started.Load() {
		t.Error("started should be true after Start")
	}

	select {
	case <-buf.Ready():
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Ready")
	}

	cancel()

	time.Sleep(100 * time.Millisecond)

	err = buf.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if buf.started.Load() {
		t.Error("started should be false after Stop")
	}
}

func TestBufferedDataSourceDoubleStart(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)

	ctx := context.Background()
	eventCh := make(chan *common.Event, 100)

	err = buf.Start(ctx, eventCh)
	if err != nil {
		t.Fatalf("first Start failed: %v", err)
	}

	err = buf.Start(ctx, eventCh)
	if err != nil {
		t.Fatalf("second Start should not error: %v", err)
	}

	buf.Stop()
}

func TestBufferedDataSourceStopWithoutStart(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)

	err = buf.Stop()
	if err != nil {
		t.Errorf("Stop without Start should not error: %v", err)
	}
}

func TestBufferedDataSourceInterface(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)
	var _ DataSource = buf
	_ = buf
}

func TestBufferedDataSourceBufferEvents(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
		EventIntervalMs: 10,
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 5, DropOldest)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	eventCh := make(chan *common.Event, 1000)
	err = buf.Start(ctx, eventCh)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	received := int64(0)
	done := make(chan struct{})
	go func() {
		for range eventCh {
			atomic.AddInt64(&received, 1)
		}
		close(done)
	}()

	<-ctx.Done()
	buf.Stop()
	close(eventCh)
	<-done

	stats := buf.GetBufferStats()
	t.Logf("Buffered=%d Dropped=%d Processed=%d FullCount=%d",
		stats.BufferedEvents, stats.DroppedEvents, stats.ProcessedEvents, stats.FullCount)
}

func TestBufferedDataSourceSetLogger(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)
	buf.SetLogger(buf.logger)
}

func TestBufferedDataSourceParseConfig(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)
	err = buf.ParseConfig(map[string]interface{}{
		"event_interval_ms": int64(100),
	})
	if err != nil {
		t.Errorf("ParseConfig failed: %v", err)
	}
}

func TestBufferedDataSourceHealth(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)
	err = buf.Health()
	// delegates to source; mock returns error when not running
	if err == nil {
		t.Error("Health should return error when source is not started")
	}
}

func TestBufferedDataSourceGetStats(t *testing.T) {
	mock, err := NewMockDataSource(&MockDataSourceConfig{
		BaseDataSourceConfig: BaseDataSourceConfig{
			Type: "mock", Name: "test", Enabled: true, BufferSize: 100,
		},
	})
	if err != nil {
		t.Fatalf("NewMockDataSource failed: %v", err)
	}

	buf := NewBufferedDataSource(mock, 10, DropOldest)
	stats := buf.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}
}
