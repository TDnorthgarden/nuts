package eventlog

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAsyncEventLog_Append(t *testing.T) {
	inner := NewRingBufferEventLog(1000)
	async := NewAsyncEventLog(inner, 100)

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		async.Append(ctx, &EventLogEntry{
			ID:     fmt.Sprintf("entry-%d", i),
			TraceID: "trace-async",
			Stage:  StageDataSource,
		})
	}

	// 等待异步处理完成
	time.Sleep(100 * time.Millisecond)

	timeline, _ := async.QueryByTraceID("trace-async")
	if len(timeline.Entries) != 10 {
		t.Errorf("expected 10 entries, got %d", len(timeline.Entries))
	}

	async.Close()
}

func TestAsyncEventLog_BufferFull(t *testing.T) {
	inner := NewRingBufferEventLog(10000)
	async := NewAsyncEventLog(inner, 5) // 很小的 buffer

	ctx := context.Background()
	// 追加超过 buffer 大小的条目，不应阻塞
	for i := 0; i < 20; i++ {
		async.Append(ctx, &EventLogEntry{
			ID:     fmt.Sprintf("entry-%d", i),
			TraceID: "trace-overflow",
			Stage:  StageDataSource,
		})
	}

	time.Sleep(100 * time.Millisecond)

	// 部分条目可能因 buffer 满被丢弃，但不应 panic
	async.Close()
}

func TestAsyncEventLog_NilEntry(t *testing.T) {
	inner := NewRingBufferEventLog(100)
	async := NewAsyncEventLog(inner, 100)
	defer async.Close()

	if err := async.Append(context.Background(), nil); err != nil {
		t.Errorf("Append(nil) should not error, got %v", err)
	}
}

func TestAsyncEventLog_Close(t *testing.T) {
	inner := NewRingBufferEventLog(100)
	async := NewAsyncEventLog(inner, 100)

	ctx := context.Background()
	async.Append(ctx, &EventLogEntry{ID: "e1", TraceID: "t1"})

	if err := async.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
}
