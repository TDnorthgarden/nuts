package eventlog

import (
	"context"
	"sync"
	"time"
)

// ringBufferEventLog 基于环形缓冲区的 EventLog 实现
// 用于调试和测试场景，数据仅保存在内存中
type ringBufferEventLog struct {
	entries []EventLogEntry
	size    int
	head    int
	count   int
	mu      sync.RWMutex

	// 二级索引
	traceIndex map[string][]string // traceID → []entryID
	taskIndex  map[string][]string // taskID → []entryID
}

// NewRingBufferEventLog 创建环形缓冲区 EventLog
func NewRingBufferEventLog(size int) EventLog {
	if size <= 0 {
		size = 10000
	}
	return &ringBufferEventLog{
		entries:    make([]EventLogEntry, size),
		size:       size,
		traceIndex: make(map[string][]string),
		taskIndex:  make(map[string][]string),
	}
}

func (r *ringBufferEventLog) Append(_ context.Context, entry *EventLogEntry) error {
	if entry == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// 覆盖旧条目时清理索引
	if r.count == r.size {
		old := r.entries[r.head]
		r.removeFromIndex(old.TraceID, old.TaskID, old.ID)
	}

	r.entries[r.head] = *entry
	entryID := entry.ID
	r.head = (r.head + 1) % r.size
	if r.count < r.size {
		r.count++
	}

	// 更新索引
	if entry.TraceID != "" {
		r.traceIndex[entry.TraceID] = append(r.traceIndex[entry.TraceID], entryID)
	}
	if entry.TaskID != "" {
		r.taskIndex[entry.TaskID] = append(r.taskIndex[entry.TaskID], entryID)
	}

	return nil
}

func (r *ringBufferEventLog) removeFromIndex(traceID, taskID, entryID string) {
	if traceID != "" {
		ids := r.traceIndex[traceID]
		for i, id := range ids {
			if id == entryID {
				r.traceIndex[traceID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(r.traceIndex[traceID]) == 0 {
			delete(r.traceIndex, traceID)
		}
	}
	if taskID != "" {
		ids := r.taskIndex[taskID]
		for i, id := range ids {
			if id == entryID {
				r.taskIndex[taskID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(r.taskIndex[taskID]) == 0 {
			delete(r.taskIndex, taskID)
		}
	}
}

func (r *ringBufferEventLog) QueryByTraceID(traceID string) (*TraceTimeline, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entryIDs := r.traceIndex[traceID]
	if len(entryIDs) == 0 {
		return &TraceTimeline{TraceID: traceID}, nil
	}

	entries := r.findByIDs(entryIDs)
	if len(entries) == 0 {
		return &TraceTimeline{TraceID: traceID}, nil
	}

	// 计算时间跨度
	var minTime, maxTime time.Time
	taskMap := make(map[string]bool)
	for i, e := range entries {
		if i == 0 || e.Timestamp.Before(minTime) {
			minTime = e.Timestamp
		}
		if i == 0 || e.Timestamp.After(maxTime) {
			maxTime = e.Timestamp
		}
		if e.TaskID != "" {
			taskMap[e.TaskID] = true
		}
	}

	tasks := make([]TaskSummary, 0, len(taskMap))
	for taskID := range taskMap {
		tasks = append(tasks, TaskSummary{ID: taskID})
	}

	return &TraceTimeline{
		TraceID:  traceID,
		Entries:  entries,
		Tasks:    tasks,
		Duration: maxTime.Sub(minTime),
	}, nil
}

func (r *ringBufferEventLog) QueryByTaskID(taskID string) ([]EventLogEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entryIDs := r.taskIndex[taskID]
	if len(entryIDs) == 0 {
		return nil, nil
	}

	return r.findByIDs(entryIDs), nil
}

func (r *ringBufferEventLog) Query(filter LogFilter) ([]EventLogEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []EventLogEntry
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + r.size) % r.size
		e := r.entries[idx]

		if filter.TraceID != "" && e.TraceID != filter.TraceID {
			continue
		}
		if filter.TaskID != "" && e.TaskID != filter.TaskID {
			continue
		}
		if filter.Stage != "" && e.Stage != filter.Stage {
			continue
		}
		if filter.Source != "" && e.Source != filter.Source {
			continue
		}
		if filter.StartTime != nil && e.Timestamp.Before(*filter.StartTime) {
			continue
		}
		if filter.EndTime != nil && e.Timestamp.After(*filter.EndTime) {
			continue
		}

		result = append(result, e)

		if filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}

	return result, nil
}

func (r *ringBufferEventLog) Cleanup(before time.Time) (int, error) {
	// Ring buffer 自动覆盖旧数据，无需手动清理
	return 0, nil
}

func (r *ringBufferEventLog) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = nil
	r.count = 0
	r.traceIndex = nil
	r.taskIndex = nil
	return nil
}

// findByIDs 按 ID 列表查找条目（需持有读锁）
func (r *ringBufferEventLog) findByIDs(ids []string) []EventLogEntry {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	var result []EventLogEntry
	for i := 0; i < r.count; i++ {
		idx := (r.head - r.count + i + r.size) % r.size
		if idSet[r.entries[idx].ID] {
			result = append(result, r.entries[idx])
		}
	}
	return result
}
