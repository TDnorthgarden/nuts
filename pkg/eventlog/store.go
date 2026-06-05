package eventlog

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/db"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

const (
	eventLogPrefix     = "eventlog:"
	eventLogTraceIdx   = "eventlog:idx:trace:"
	eventLogTaskIdx    = "eventlog:idx:task:"
)

// dbEventLog 基于 db.DB 的 EventLog 实现
// 使用主键存储 + 内存二级索引实现按 TraceID/TaskID 查询
type dbEventLog struct {
	db     db.DB
	logger log.Logger

	// 内存二级索引（启动时从 DB 重建）
	traceIndex   map[string][]string // traceID → []entryID
	traceIndexMu sync.RWMutex
	taskIndex    map[string][]string // taskID → []entryID
	taskIndexMu  sync.RWMutex
}

// NewDBEventLog 创建 DB-backed EventLog
func NewDBEventLog(database db.DB) EventLog {
	d := &dbEventLog{
		db:         database,
		logger:     log.GetDefault(),
		traceIndex: make(map[string][]string),
		taskIndex:  make(map[string][]string),
	}
	d.rebuildIndex()
	return d
}

func (d *dbEventLog) Append(_ context.Context, entry *EventLogEntry) error {
	if entry == nil {
		return nil
	}
	if entry.ID == "" {
		entry.ID = common.GenerateUUID()
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal event log entry: %w", err)
	}

	key := eventLogPrefix + entry.ID
	if err := d.db.Set(key, data); err != nil {
		return fmt.Errorf("store event log entry: %w", err)
	}

	// 更新内存索引
	if entry.TraceID != "" {
		d.traceIndexMu.Lock()
		d.traceIndex[entry.TraceID] = append(d.traceIndex[entry.TraceID], entry.ID)
		d.traceIndexMu.Unlock()
	}
	if entry.TaskID != "" {
		d.taskIndexMu.Lock()
		d.taskIndex[entry.TaskID] = append(d.taskIndex[entry.TaskID], entry.ID)
		d.taskIndexMu.Unlock()
	}

	return nil
}

func (d *dbEventLog) QueryByTraceID(traceID string) (*TraceTimeline, error) {
	d.traceIndexMu.RLock()
	entryIDs := d.traceIndex[traceID]
	d.traceIndexMu.RUnlock()

	if len(entryIDs) == 0 {
		return &TraceTimeline{TraceID: traceID}, nil
	}

	entries, err := d.getByIDs(entryIDs)
	if err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return &TraceTimeline{TraceID: traceID}, nil
	}

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

func (d *dbEventLog) QueryByTaskID(taskID string) ([]EventLogEntry, error) {
	d.taskIndexMu.RLock()
	entryIDs := d.taskIndex[taskID]
	d.taskIndexMu.RUnlock()

	if len(entryIDs) == 0 {
		return nil, nil
	}

	return d.getByIDs(entryIDs)
}

func (d *dbEventLog) Query(filter LogFilter) ([]EventLogEntry, error) {
	// 全量扫描（适合数据量不大的场景）
	allEntries, err := d.getAllEntries()
	if err != nil {
		return nil, err
	}

	var result []EventLogEntry
	for _, e := range allEntries {
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

func (d *dbEventLog) Cleanup(before time.Time) (int, error) {
	allEntries, err := d.getAllEntries()
	if err != nil {
		return 0, err
	}

	deleted := 0
	for _, e := range allEntries {
		if e.Timestamp.Before(before) {
			key := eventLogPrefix + e.ID
			if err := d.db.Delete(key); err != nil {
				d.logger.Warn("Failed to delete event log entry",
					log.String("entry_id", e.ID), log.Error(err))
				continue
			}
			d.removeFromIndex(e)
			deleted++
		}
	}

	return deleted, nil
}

func (d *dbEventLog) Close() error {
	d.traceIndex = nil
	d.taskIndex = nil
	return nil
}

func (d *dbEventLog) getByIDs(ids []string) ([]EventLogEntry, error) {
	entries := make([]EventLogEntry, 0, len(ids))
	for _, id := range ids {
		key := eventLogPrefix + id
		data, err := d.db.Get(key)
		if err != nil {
			continue // 条目可能已被清理
		}
		var entry EventLogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (d *dbEventLog) getAllEntries() ([]EventLogEntry, error) {
	keys, err := d.db.List(eventLogPrefix)
	if err != nil {
		return nil, fmt.Errorf("list event log entries: %w", err)
	}

	entries := make([]EventLogEntry, 0, len(keys))
	for _, key := range keys {
		data, err := d.db.Get(key)
		if err != nil {
			continue
		}
		var entry EventLogEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (d *dbEventLog) removeFromIndex(entry EventLogEntry) {
	if entry.TraceID != "" {
		d.traceIndexMu.Lock()
		ids := d.traceIndex[entry.TraceID]
		for i, id := range ids {
			if id == entry.ID {
				d.traceIndex[entry.TraceID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(d.traceIndex[entry.TraceID]) == 0 {
			delete(d.traceIndex, entry.TraceID)
		}
		d.traceIndexMu.Unlock()
	}
	if entry.TaskID != "" {
		d.taskIndexMu.Lock()
		ids := d.taskIndex[entry.TaskID]
		for i, id := range ids {
			if id == entry.ID {
				d.taskIndex[entry.TaskID] = append(ids[:i], ids[i+1:]...)
				break
			}
		}
		if len(d.taskIndex[entry.TaskID]) == 0 {
			delete(d.taskIndex, entry.TaskID)
		}
		d.taskIndexMu.Unlock()
	}
}

func (d *dbEventLog) rebuildIndex() {
	entries, err := d.getAllEntries()
	if err != nil {
		d.logger.Warn("Failed to rebuild event log index", log.Error(err))
		return
	}

	for _, e := range entries {
		if e.TraceID != "" {
			d.traceIndex[e.TraceID] = append(d.traceIndex[e.TraceID], e.ID)
		}
		if e.TaskID != "" {
			d.taskIndex[e.TaskID] = append(d.taskIndex[e.TaskID], e.ID)
		}
	}

	d.logger.Info("Event log index rebuilt",
		log.Int("entries", len(entries)),
		log.Int("trace_index", len(d.traceIndex)),
		log.Int("task_index", len(d.taskIndex)))
}
