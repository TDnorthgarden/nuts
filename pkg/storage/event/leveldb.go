package event

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/nuts-project/nuts/pkg/storage"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/syndtr/goleveldb/leveldb/opt"
	"github.com/syndtr/goleveldb/leveldb/util"
)

// LevelDBEventStore LevelDB事件存储实现
type LevelDBEventStore struct {
	db *leveldb.DB
}

// NewLevelDBEventStore 创建LevelDB事件存储
func NewLevelDBEventStore(dbPath string) (storage.EventStore, error) {
	opts := &opt.Options{
		// 优化写入性能
		WriteBuffer: 64 * 1024 * 1024, // 64MB
	}

	db, err := leveldb.OpenFile(dbPath, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &LevelDBEventStore{db: db}, nil
}

// Write 写入单个事件
func (s *LevelDBEventStore) Write(event *storage.Event) error {
	key := s.buildEventKey(event)
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	return s.db.Put([]byte(key), data, nil)
}

// WriteBatch 批量写入事件
func (s *LevelDBEventStore) WriteBatch(events []*storage.Event) error {
	batch := new(leveldb.Batch)
	for _, event := range events {
		key := s.buildEventKey(event)
		data, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("failed to marshal event: %w", err)
		}
		batch.Put([]byte(key), data)
	}
	return s.db.Write(batch, nil)
}

// Query 查询事件
func (s *LevelDBEventStore) Query(query *storage.EventQuery) ([]*storage.Event, error) {
	var events []*storage.Event

	// 构建查询范围
	prefix := s.buildEventPrefix(query.CgroupID, query.PolicyID)
	iter := s.db.NewIterator(&util.Range{Start: []byte(prefix)}, nil)
	defer iter.Release()

	for iter.Next() {
		var event storage.Event
		if err := json.Unmarshal(iter.Value(), &event); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event: %w", err)
		}

		// 应用过滤条件
		if query.EventType != "" && event.Type != query.EventType {
			continue
		}
		if !query.StartTime.IsZero() && event.Timestamp.Before(query.StartTime) {
			continue
		}
		if !query.EndTime.IsZero() && event.Timestamp.After(query.EndTime) {
			continue
		}

		events = append(events, &event)

		if query.Limit > 0 && len(events) >= query.Limit {
			break
		}
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return events, nil
}

// QueryByTimeRange 按时间范围查询事件
func (s *LevelDBEventStore) QueryByTimeRange(start, end time.Time, filters map[string]string) ([]*storage.Event, error) {
	var events []*storage.Event

	cgroupID, _ := filters["cgroup_id"]
	policyID, _ := filters["policy_id"]
	eventType, _ := filters["event_type"]

	prefix := s.buildEventPrefix(cgroupID, policyID)
	iter := s.db.NewIterator(&util.Range{Start: []byte(prefix)}, nil)
	defer iter.Release()

	for iter.Next() {
		var event storage.Event
		if err := json.Unmarshal(iter.Value(), &event); err != nil {
			return nil, fmt.Errorf("failed to unmarshal event: %w", err)
		}

		// 应用过滤条件
		if event.Timestamp.Before(start) || event.Timestamp.After(end) {
			continue
		}
		if eventType != "" && event.Type != eventType {
			continue
		}

		events = append(events, &event)
	}

	if err := iter.Error(); err != nil {
		return nil, err
	}

	return events, nil
}

// Delete 删除事件
func (s *LevelDBEventStore) Delete(cgroupID string, policyID string) error {
	prefix := s.buildEventPrefix(cgroupID, policyID)
	iter := s.db.NewIterator(&util.Range{Start: []byte(prefix)}, nil)
	defer iter.Release()

	batch := new(leveldb.Batch)
	for iter.Next() {
		batch.Delete(iter.Key())
	}

	return s.db.Write(batch, nil)
}

// Close 关闭数据库
func (s *LevelDBEventStore) Close() error {
	return s.db.Close()
}

// buildEventKey 构建事件键
func (s *LevelDBEventStore) buildEventKey(event *storage.Event) string {
	return fmt.Sprintf("%s:%s:%d:%s",
		event.CgroupID,
		event.PolicyID,
		event.Timestamp.UnixNano(),
		event.ID,
	)
}

// buildEventPrefix 构建事件前缀
func (s *LevelDBEventStore) buildEventPrefix(cgroupID, policyID string) string {
	if cgroupID != "" && policyID != "" {
		return fmt.Sprintf("%s:%s:", cgroupID, policyID)
	}
	if cgroupID != "" {
		return fmt.Sprintf("%s:", cgroupID)
	}
	return ""
}
