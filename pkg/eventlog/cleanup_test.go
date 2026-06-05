package eventlog

import (
	"context"
	"testing"
	"time"
)

func TestCleaner_StartStop(t *testing.T) {
	inner := NewRingBufferEventLog(100)
	cleaner := NewCleaner(inner, 100*time.Millisecond, 1*time.Second)

	cleaner.Start()
	time.Sleep(50 * time.Millisecond)
	cleaner.Stop()
}

func TestCleaner_CleanupRingBuffer(t *testing.T) {
	// RingBuffer 的 Cleanup 是 no-op
	inner := NewRingBufferEventLog(100)
	cleaner := NewCleaner(inner, 50*time.Millisecond, 1*time.Second)

	cleaner.Start()
	time.Sleep(150 * time.Millisecond)
	cleaner.Stop()

	// RingBuffer 不会清理任何东西
	ctx := context.Background()
	inner.Append(ctx, &EventLogEntry{ID: "e1", TraceID: "t1", Timestamp: time.Now()})
	timeline, _ := inner.QueryByTraceID("t1")
	if len(timeline.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(timeline.Entries))
	}
}
