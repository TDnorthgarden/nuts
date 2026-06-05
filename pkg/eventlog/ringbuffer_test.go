package eventlog

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestRingBuffer_AppendAndQuery(t *testing.T) {
	rbl := NewRingBufferEventLog(100)
	ctx := context.Background()

	// 追加条目
	for i := 0; i < 5; i++ {
		entry := &EventLogEntry{
			ID:        fmt.Sprintf("entry-%d", i),
			TraceID:   "trace-abc",
			EventID:   fmt.Sprintf("event-%d", i),
			Stage:     StageDataSource,
			EventType: "TestEvent",
			Source:    "test",
			Timestamp: time.Now().Add(time.Duration(i) * time.Second),
			TaskID:    "task-001",
		}
		if err := rbl.Append(ctx, entry); err != nil {
			t.Fatalf("Append failed: %v", err)
		}
	}

	// 按 TraceID 查询
	timeline, err := rbl.QueryByTraceID("trace-abc")
	if err != nil {
		t.Fatalf("QueryByTraceID failed: %v", err)
	}
	if len(timeline.Entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(timeline.Entries))
	}
	if timeline.TraceID != "trace-abc" {
		t.Errorf("expected trace_id trace-abc, got %s", timeline.TraceID)
	}

	// 按 TaskID 查询
	entries, err := rbl.QueryByTaskID("task-001")
	if err != nil {
		t.Fatalf("QueryByTaskID failed: %v", err)
	}
	if len(entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(entries))
	}
}

func TestRingBuffer_Overwrite(t *testing.T) {
	rbl := NewRingBufferEventLog(3) // 只能存 3 条
	ctx := context.Background()

	// 追加 5 条，应覆盖前 2 条
	for i := 0; i < 5; i++ {
		entry := &EventLogEntry{
			ID:        fmt.Sprintf("entry-%d", i),
			TraceID:   "trace-abc",
			EventID:   fmt.Sprintf("event-%d", i),
			Stage:     StageDataSource,
			EventType: "TestEvent",
			Source:    "test",
			Timestamp: time.Now(),
			TaskID:    "task-001",
		}
		rbl.Append(ctx, entry)
	}

	// 应只剩 3 条（entry-2, entry-3, entry-4）
	entries, _ := rbl.QueryByTaskID("task-001")
	if len(entries) != 3 {
		t.Errorf("expected 3 entries after overwrite, got %d", len(entries))
	}
}

func TestRingBuffer_QueryFilter(t *testing.T) {
	rbl := NewRingBufferEventLog(100)
	ctx := context.Background()

	now := time.Now()
	rbl.Append(ctx, &EventLogEntry{
		ID: "e1", TraceID: "t1", Stage: StageDataSource, Source: "src-a",
		Timestamp: now, TaskID: "task-1",
	})
	rbl.Append(ctx, &EventLogEntry{
		ID: "e2", TraceID: "t1", Stage: StageTaskCreate, Source: "src-b",
		Timestamp: now.Add(time.Second), TaskID: "task-1",
	})
	rbl.Append(ctx, &EventLogEntry{
		ID: "e3", TraceID: "t2", Stage: StageDataSource, Source: "src-a",
		Timestamp: now.Add(2 * time.Second), TaskID: "task-2",
	})

	// 按 Stage 过滤
	entries, _ := rbl.Query(LogFilter{Stage: StageDataSource})
	if len(entries) != 2 {
		t.Errorf("expected 2 datasource entries, got %d", len(entries))
	}

	// 按 Source 过滤
	entries, _ = rbl.Query(LogFilter{Source: "src-b"})
	if len(entries) != 1 {
		t.Errorf("expected 1 src-b entry, got %d", len(entries))
	}

	// 按 TraceID 过滤
	entries, _ = rbl.Query(LogFilter{TraceID: "t1"})
	if len(entries) != 2 {
		t.Errorf("expected 2 t1 entries, got %d", len(entries))
	}

	// Limit
	entries, _ = rbl.Query(LogFilter{Limit: 1})
	if len(entries) != 1 {
		t.Errorf("expected 1 entry with limit, got %d", len(entries))
	}

	// 时间范围过滤
	start := now.Add(500 * time.Millisecond)
	end := now.Add(1500 * time.Millisecond)
	entries, _ = rbl.Query(LogFilter{StartTime: &start, EndTime: &end})
	if len(entries) != 1 {
		t.Errorf("expected 1 entry in time range, got %d", len(entries))
	}
}

func TestRingBuffer_QueryByTraceID_Empty(t *testing.T) {
	rbl := NewRingBufferEventLog(100)
	timeline, err := rbl.QueryByTraceID("nonexistent")
	if err != nil {
		t.Fatalf("QueryByTraceID failed: %v", err)
	}
	if len(timeline.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(timeline.Entries))
	}
}

func TestRingBuffer_NilEntry(t *testing.T) {
	rbl := NewRingBufferEventLog(100)
	if err := rbl.Append(context.Background(), nil); err != nil {
		t.Errorf("Append(nil) should not error, got %v", err)
	}
}

func TestRingBuffer_Close(t *testing.T) {
	rbl := NewRingBufferEventLog(100)
	ctx := context.Background()
	rbl.Append(ctx, &EventLogEntry{ID: "e1", TraceID: "t1"})

	if err := rbl.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Close 后查询应返回空
	timeline, _ := rbl.QueryByTraceID("t1")
	if timeline != nil && len(timeline.Entries) != 0 {
		t.Errorf("expected 0 entries after close, got %d", len(timeline.Entries))
	}
}

func TestRingBuffer_ConcurrentAppend(t *testing.T) {
	rbl := NewRingBufferEventLog(10000)
	ctx := context.Background()

	done := make(chan struct{})
	for g := 0; g < 10; g++ {
		go func(id int) {
			for i := 0; i < 100; i++ {
				rbl.Append(ctx, &EventLogEntry{
					ID:     fmt.Sprintf("g%d-e%d", id, i),
					TraceID: "shared-trace",
					Stage:  StageDataSource,
				})
			}
			done <- struct{}{}
		}(g)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	timeline, _ := rbl.QueryByTraceID("shared-trace")
	if len(timeline.Entries) != 1000 {
		t.Errorf("expected 1000 entries, got %d", len(timeline.Entries))
	}
}
