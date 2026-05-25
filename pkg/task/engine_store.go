package task

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/db"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// indexEntry 索引条目，缓存 CreatedAt 用于排序，避免 List 时反序列化
type indexEntry struct {
	id        string
	createdAt time.Time
}

// engineTaskStore implements TaskStore using a generic db.DB backend.
// It handles JSON serialization/deserialization and business-level filtering.
type engineTaskStore struct {
	db     db.DB
	prefix string
	logger log.Logger

	// 写锁：per-task 分段锁，保护 Get→modify→Set 原子性
	keyLocks sync.Map // map[string]*sync.Mutex

	// 状态历史最大条目数，超限时裁剪旧记录
	maxStateHistory int

	// stateIndex 二级索引：state → sorted []indexEntry（按 CreatedAt 降序）
	// 加速 List(State=) 查询，支持索引层分页，避免全量反序列化
	stateIndex   map[TaskState][]indexEntry
	stateIndexMu sync.RWMutex
}

// getKeyLock 获取任务的写锁（延迟创建）
func (s *engineTaskStore) getKeyLock(id string) *sync.Mutex {
	v, _ := s.keyLocks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// NewTaskStore creates a TaskStore backed by the given db.DB.
func NewTaskStore(database db.DB, maxStateHistory int) TaskStore {
	s := &engineTaskStore{
		db:              database,
		prefix:          "task:",
		logger:          log.GetDefault(),
		maxStateHistory: maxStateHistory,
		stateIndex:      make(map[TaskState][]indexEntry),
	}
	s.rebuildStateIndex()
	return s
}

// Get retrieves a task by ID.
func (s *engineTaskStore) Get(id string) (*Task, error) {
	data, err := s.db.Get(s.prefix + id)
	if err != nil {
		return nil, fmt.Errorf("get task %s: %w", id, err)
	}

	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("unmarshal task %s: %w", id, err)
	}
	return &task, nil
}

// Create adds a new task with default initialization.
func (s *engineTaskStore) Create(task *Task) error {
	if task.ID == "" {
		return fmt.Errorf("task id is required")
	}

	now := time.Now()
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	if task.UpdatedAt.IsZero() {
		task.UpdatedAt = now
	}
	if task.StateUpdatedAt.IsZero() {
		task.StateUpdatedAt = now
	}
	if task.State == "" {
		task.State = TaskStatePending
	}
	if task.StateHistory == nil {
		task.StateHistory = make([]StateTransitionRecord, 0)
	}

	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	if err := s.db.Set(s.prefix+task.ID, data); err != nil {
		return err
	}

	s.addToStateIndex(task.ID, task.State, task.CreatedAt)
	return nil
}

// addToStateIndex 将任务插入 stateIndex（按 CreatedAt 降序插入）
func (s *engineTaskStore) addToStateIndex(id string, state TaskState, createdAt time.Time) {
	s.stateIndexMu.Lock()
	defer s.stateIndexMu.Unlock()
	entry := indexEntry{id: id, createdAt: createdAt}
	entries := s.stateIndex[state]
	// sort.Search 找到第一个 createdAt <= entry.createdAt 的位置（降序）
	pos := sort.Search(len(entries), func(i int) bool {
		return !entries[i].createdAt.After(createdAt)
	})
	entries = append(entries, indexEntry{})
	copy(entries[pos+1:], entries[pos:])
	entries[pos] = entry
	s.stateIndex[state] = entries
}

// removeFromStateIndex 从 stateIndex 中移除任务 ID
func (s *engineTaskStore) removeFromStateIndex(id string, state TaskState) {
	s.stateIndexMu.Lock()
	defer s.stateIndexMu.Unlock()
	entries, ok := s.stateIndex[state]
	if !ok {
		return
	}
	for i, e := range entries {
		if e.id == id {
			s.stateIndex[state] = append(entries[:i], entries[i+1:]...)
			break
		}
	}
	if len(s.stateIndex[state]) == 0 {
		delete(s.stateIndex, state)
	}
}

// moveInStateIndex 将任务从 oldState 移到 newState（保持排序）
func (s *engineTaskStore) moveInStateIndex(id string, oldState, newState TaskState, createdAt time.Time) {
	if oldState == newState {
		return
	}
	s.removeFromStateIndex(id, oldState)
	s.addToStateIndex(id, newState, createdAt)
}

// rebuildStateIndex 从持久化存储重建 stateIndex（进程重启后使用）
func (s *engineTaskStore) rebuildStateIndex() {
	keys, err := s.db.List(s.prefix)
	if err != nil {
		s.logger.Warn("rebuildStateIndex: list failed", log.Error(err))
		return
	}
	for _, key := range keys {
		data, err := s.db.Get(key)
		if err != nil {
			continue
		}
		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			continue
		}
		if task.ArchivedAt == nil {
			s.addToStateIndex(task.ID, task.State, task.CreatedAt)
		}
	}
	s.logger.Info("stateIndex rebuilt", log.Int("total_tasks", len(keys)))
}

// Update replaces the stored task with optimistic locking.
func (s *engineTaskStore) Update(task *Task) error {
	lock := s.getKeyLock(task.ID)
	lock.Lock()
	defer lock.Unlock()

	existing, err := s.getLocked(task.ID)
	if err != nil {
		return fmt.Errorf("get existing: %w", err)
	}

	if task.Version != existing.Version {
		return common.NewAppError(common.CodeTaskVersionConflict,
			fmt.Sprintf("task %s: expected %d, got %d", task.ID, existing.Version, task.Version))
	}

	task.UpdatedAt = time.Now()

	if err := s.putLocked(task); err != nil {
		return err
	}

	// 归档后从 stateIndex 移除
	if task.ArchivedAt != nil && existing.ArchivedAt == nil {
		s.removeFromStateIndex(task.ID, existing.State)
	}

	return nil
}

// Delete removes a task.
func (s *engineTaskStore) Delete(id string) error {
	lock := s.getKeyLock(id)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getLocked(id)
	if err != nil {
		// 任务不存在时仍然删除索引和锁
		s.keyLocks.Delete(id)
		return s.db.Delete(s.prefix + id)
	}

	if err := s.db.Delete(s.prefix + id); err != nil {
		return err
	}

	s.keyLocks.Delete(id)
	s.removeFromStateIndex(id, task.State)
	return nil
}

// List returns tasks matching the filter.
func (s *engineTaskStore) List(filter TaskFilter) ([]*Task, error) {
	// 有 State 过滤时尝试从二级索引读取，避免全表扫描
	if filter.State != "" {
		return s.listByState(filter)
	}

	keys, err := s.db.List(s.prefix)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}

	tasks := make([]*Task, 0, len(keys))
	var readErrors int
	for _, key := range keys {
		data, err := s.db.Get(key)
		if err != nil {
			s.logger.Error("engineTaskStore.List: failed to read task",
				log.String("key", key), log.Error(err))
			readErrors++
			continue
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			s.logger.Error("engineTaskStore.List: failed to deserialize task",
				log.String("key", key), log.Error(err))
			readErrors++
			continue
		}

		if !s.matchFilter(&task, filter) {
			continue
		}

		tasks = append(tasks, &task)
	}

	if readErrors > 0 {
		s.logger.Warn("engineTaskStore.List: skipped tasks due to errors",
			log.Int("skipped", readErrors),
			log.Int("total_keys", len(keys)),
			log.Int("returned", len(tasks)))
	}

	// 稳定排序：按创建时间降序
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].CreatedAt.After(tasks[j].CreatedAt)
	})

	// Apply pagination
	if filter.Offset > 0 {
		if filter.Offset >= len(tasks) {
			return []*Task{}, nil
		}
		tasks = tasks[filter.Offset:]
	}
	if filter.Limit > 0 && filter.Limit < len(tasks) {
		tasks = tasks[:filter.Limit]
	}

	return tasks, nil
}

// listByState 使用有序 stateIndex 加速按状态查询，支持索引层分页
func (s *engineTaskStore) listByState(filter TaskFilter) ([]*Task, error) {
	s.stateIndexMu.RLock()
	entries := s.stateIndex[filter.State]
	// 索引层分页：只取需要的区间，避免全量反序列化
	total := len(entries)
	start := filter.Offset
	if start > total {
		s.stateIndexMu.RUnlock()
		return []*Task{}, nil
	}
	end := total
	if filter.Limit > 0 && start+filter.Limit < end {
		end = start + filter.Limit
	}
	// 复制需要的 ID 区间
	pageIDs := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		pageIDs = append(pageIDs, entries[i].id)
	}
	s.stateIndexMu.RUnlock()

	tasks := make([]*Task, 0, len(pageIDs))
	var readErrors int
	for _, id := range pageIDs {
		data, err := s.db.Get(s.prefix + id)
		if err != nil {
			s.logger.Error("engineTaskStore.List: failed to read task",
				log.String("id", id), log.Error(err))
			readErrors++
			continue
		}

		var task Task
		if err := json.Unmarshal(data, &task); err != nil {
			s.logger.Error("engineTaskStore.List: failed to deserialize task",
				log.String("id", id), log.Error(err))
			readErrors++
			continue
		}

		if !s.matchFilter(&task, filter) {
			continue
		}

		tasks = append(tasks, &task)
	}

	if readErrors > 0 {
		s.logger.Warn("engineTaskStore.List: skipped tasks due to errors",
			log.Int("skipped", readErrors),
			log.Int("returned", len(tasks)))
	}

	return tasks, nil
}

// Count returns the number of tasks matching the filter.
// 当仅有 State 过滤（无归档、无其他条件）时直接读取 stateIndex 长度，避免全量反序列化。
func (s *engineTaskStore) Count(filter TaskFilter) (int, error) {
	if filter.State != "" && !filter.IncludeArchived && filter.Priority == nil && filter.CreatedAfter == nil && filter.CreatedBefore == nil {
		s.stateIndexMu.RLock()
		n := len(s.stateIndex[filter.State])
		s.stateIndexMu.RUnlock()
		return n, nil
	}
	tasks, err := s.List(filter)
	return len(tasks), err
}

// UpdateState changes the task state and records a simple transition.
func (s *engineTaskStore) UpdateState(id string, state TaskState) error {
	lock := s.getKeyLock(id)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getLocked(id)
	if err != nil {
		return err
	}

	oldState := task.State
	now := time.Now()

	transition := StateTransitionRecord{
		From:        oldState,
		To:          state,
		Timestamp:   now,
		TriggeredBy: "system",
		Attempt:     1,
	}
	task.StateHistory = append(task.StateHistory, transition)
	if s.maxStateHistory > 0 && len(task.StateHistory) > s.maxStateHistory {
		task.StateHistory = task.StateHistory[len(task.StateHistory)-s.maxStateHistory:]
	}
	task.State = state
	task.StateUpdatedAt = now
	task.UpdatedAt = now

	if err := s.putLocked(task); err != nil {
		return err
	}

	s.moveInStateIndex(id, oldState, state, task.CreatedAt)
	return nil
}

// UpdateStateWithRecord changes the task state and records a detailed transition.
func (s *engineTaskStore) UpdateStateWithRecord(id string, state TaskState, triggeredBy, reason string) error {
	lock := s.getKeyLock(id)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getLocked(id)
	if err != nil {
		return err
	}

	oldState := task.State
	now := time.Now()

	attempt := 1
	if len(task.StateHistory) > 0 {
		last := task.StateHistory[len(task.StateHistory)-1]
		if last.To == state {
			attempt = last.Attempt + 1
		}
	}

	record := StateTransitionRecord{
		From:        oldState,
		To:          state,
		Timestamp:   now,
		TriggeredBy: triggeredBy,
		Reason:      reason,
		Attempt:     attempt,
	}
	task.StateHistory = append(task.StateHistory, record)
	if s.maxStateHistory > 0 && len(task.StateHistory) > s.maxStateHistory {
		task.StateHistory = task.StateHistory[len(task.StateHistory)-s.maxStateHistory:]
	}
	task.State = state
	task.StateUpdatedAt = now
	task.UpdatedAt = now

	if err := s.putLocked(task); err != nil {
		return err
	}

	s.moveInStateIndex(id, oldState, state, task.CreatedAt)
	return nil
}

// TransitionState 原子化状态转换，在同一个锁周期内完成状态、历史、ArchivedAt、RetryCount 的更新。
// postCommit 在状态持久化成功后调用（仍持有锁），用于发布事件等通知操作。
// postCommit 失败不影响状态转换结果（store 是 source of truth，事件是 best-effort 通知）。
func (s *engineTaskStore) TransitionState(id string, newState TaskState, triggeredBy, reason string, setArchivedAt *time.Time, clearArchived bool, setRetryCount *int, postCommit func() error) (*Task, error) {
	lock := s.getKeyLock(id)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}

	oldState := task.State
	now := time.Now()

	attempt := 1
	if len(task.StateHistory) > 0 {
		last := task.StateHistory[len(task.StateHistory)-1]
		if last.To == newState {
			attempt = last.Attempt + 1
		}
	}
	record := StateTransitionRecord{
		From:        oldState,
		To:          newState,
		Timestamp:   now,
		TriggeredBy: triggeredBy,
		Reason:      reason,
		Attempt:     attempt,
	}
	task.StateHistory = append(task.StateHistory, record)
	if s.maxStateHistory > 0 && len(task.StateHistory) > s.maxStateHistory {
		task.StateHistory = task.StateHistory[len(task.StateHistory)-s.maxStateHistory:]
	}

	task.State = newState
	task.StateUpdatedAt = now
	task.UpdatedAt = now

	if setArchivedAt != nil {
		task.ArchivedAt = setArchivedAt
	} else if clearArchived {
		task.ArchivedAt = nil
	}

	if setRetryCount != nil {
		task.RetryCount = *setRetryCount
	}

	// 先持久化状态（store 是 source of truth）
	if err := s.putLocked(task); err != nil {
		return nil, err
	}

	// 持久化成功后发布事件（best-effort，失败不影响状态转换）
	if postCommit != nil {
		if err := postCommit(); err != nil {
			s.logger.Warn("TransitionState: postCommit failed (state already persisted)",
				log.String("task_id", id),
				log.String("from", string(oldState)),
				log.String("to", string(newState)),
				log.Error(err))
		}
	}

	s.moveInStateIndex(id, oldState, newState, task.CreatedAt)
	if setArchivedAt != nil {
		s.removeFromStateIndex(id, newState)
	}

	return task, nil
}

// GetStateHistory returns the state transition history for a task.
func (s *engineTaskStore) GetStateHistory(id string) ([]StateTransitionRecord, error) {
	lock := s.getKeyLock(id)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getLocked(id)
	if err != nil {
		return nil, err
	}
	history := make([]StateTransitionRecord, len(task.StateHistory))
	copy(history, task.StateHistory)
	return history, nil
}

// UpdateResult updates the task execution result.
func (s *engineTaskStore) UpdateResult(id string, result *TaskResult) error {
	lock := s.getKeyLock(id)
	lock.Lock()
	defer lock.Unlock()

	task, err := s.getLocked(id)
	if err != nil {
		return err
	}

	task.Result = result
	task.UpdatedAt = time.Now()

	return s.putLocked(task)
}

// getLocked 获取任务（调用方需持有 getKeyLock）
func (s *engineTaskStore) getLocked(id string) (*Task, error) {
	data, err := s.db.Get(s.prefix + id)
	if err != nil {
		return nil, fmt.Errorf("get task %s: %w", id, err)
	}

	var task Task
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, fmt.Errorf("unmarshal task %s: %w", id, err)
	}
	return &task, nil
}

// putLocked 持久化任务（调用方需持有 getKeyLock）
func (s *engineTaskStore) putLocked(task *Task) error {
	task.Version++
	data, err := json.Marshal(task)
	if err != nil {
		return fmt.Errorf("marshal task: %w", err)
	}

	return s.db.Set(s.prefix+task.ID, data)
}

func (s *engineTaskStore) matchFilter(task *Task, filter TaskFilter) bool {
	if !filter.IncludeArchived && task.ArchivedAt != nil {
		return false
	}
	if filter.State != "" && task.State != filter.State {
		return false
	}
	if filter.Priority != nil && task.Priority != *filter.Priority {
		return false
	}
	if filter.CreatedAfter != nil && task.CreatedAt.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && task.CreatedAt.After(*filter.CreatedBefore) {
		return false
	}
	return true
}
