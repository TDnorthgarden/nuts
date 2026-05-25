package task

import (
	"container/heap"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// heapEntry 最小堆条目，按 deadline 排序
type heapEntry struct {
	deadline time.Time
	taskID   string
}

// timeoutHeap 任务超时最小堆
type timeoutHeap []heapEntry

func (h timeoutHeap) Len() int            { return len(h) }
func (h timeoutHeap) Less(i, j int) bool  { return h[i].deadline.Before(h[j].deadline) }
func (h timeoutHeap) Swap(i, j int)       { h[i], h[j] = h[j], h[i] }
func (h *timeoutHeap) Push(x interface{}) { *h = append(*h, x.(heapEntry)) }
func (h *timeoutHeap) Pop() interface{} {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// TimeoutCallback 超时回调，在超时检查器发现任务超时时调用
type TimeoutCallback func(taskID string, currentState TaskState)

// TimeoutChecker 超时检查器
// 定期扫描超时任务，通过回调通知超时事件
type TimeoutChecker struct {
	store         TaskStore
	config        *StateMachineConfig
	checkInterval time.Duration
	logger        log.Logger
	metrics       common.MetricsRecorder
	onTimeout     TimeoutCallback

	// heap 按 deadline 排序的任务超时堆，避免每次扫描全量任务
	heap         timeoutHeap
	heapMu       sync.Mutex
	heapStale    bool // 堆数据可能不准确，需要重新构建

	// 强制重建堆的节拍计数器（每 N 次 check 后重建一次）
	ticksSinceRebuild int
	rebuildInterval   int
}

// NewTimeoutChecker 创建超时检查器
// rebuildInterval 为重建堆的节拍数（每 N 次 check 重建一次），<=0 时使用默认值 5
func NewTimeoutChecker(store TaskStore, config *StateMachineConfig, interval time.Duration, rebuildInterval int) *TimeoutChecker {
	if interval == 0 {
		interval = 30 * time.Second // 默认 30 秒
	}
	if rebuildInterval <= 0 {
		rebuildInterval = 5
	}
	return &TimeoutChecker{
		store:         store,
		config:        config,
		checkInterval: interval,
		logger:        log.GetDefault(),
		heapStale:     true,
		rebuildInterval: rebuildInterval,
	}
}

// SetOnTimeout 设置超时回调
func (c *TimeoutChecker) SetOnTimeout(cb TimeoutCallback) {
	c.onTimeout = cb
}

// SetLogger 设置日志记录器
func (c *TimeoutChecker) SetLogger(logger log.Logger) {
	c.logger = logger
}

// SetMetrics 设置度量收集器
func (c *TimeoutChecker) SetMetrics(m common.MetricsRecorder) {
	c.metrics = m
}

// Start 启动超时检查器（动态间隔：每次扫描后计算最早超时时间，精确 sleep 到那一刻）
func (c *TimeoutChecker) Start(ctx context.Context) error {
	c.logger.Info("TimeoutChecker started",
		log.String("max_interval", c.checkInterval.String()))

	for {
		// 执行扫描并获取下次唤醒间隔
		nextWake := c.checkTimeoutsAndGetNextWake()

		timer := time.NewTimer(nextWake)
		select {
		case <-timer.C:
			// 到期，继续下一轮扫描
		case <-ctx.Done():
			timer.Stop()
			c.logger.Info("TimeoutChecker stopped")
			return nil
		}
	}
}

// checkTimeoutsAndGetNextWake 扫描超时任务并返回下次唤醒间隔
// 返回值为距离下次扫描应等待的时间：
//   - 有活跃任务时：最早超时任务的剩余时间（不超过 maxInterval）
//   - 无活跃任务时：maxInterval（默认 30s）
func (c *TimeoutChecker) checkTimeoutsAndGetNextWake() time.Duration {
	c.ticksSinceRebuild++

	// 堆为空或需要重建时，从 store 重建超时堆
	if c.heapStale || len(c.heap) == 0 || c.ticksSinceRebuild >= c.rebuildInterval {
		c.rebuildHeap()
		c.ticksSinceRebuild = 0
	}

	now := time.Now()
	timeoutCount := 0
	totalChecked := 0

	// 从堆中弹出所有已超时的任务
	for {
		c.heapMu.Lock()
		if len(c.heap) == 0 {
			c.heapMu.Unlock()
			break
		}

		entry := c.heap[0]
		if entry.deadline.After(now) {
			// 堆顶未超时，无需继续处理
			c.heapMu.Unlock()
			break
		}

		// 弹出堆顶
		heap.Pop(&c.heap)
		c.heapMu.Unlock()

		totalChecked++

		// 从 store 验证任务状态（可能已被删除或状态已变更）
		task, err := c.store.Get(entry.taskID)
		if err != nil {
			// 任务已删除，跳过
			continue
		}

		if !c.isTaskTimeout(task, now) {
			// 任务尚未超时（状态已变更），忽略
			continue
		}

		timeoutCount++
		c.logger.Warn("Task timed out",
			log.String("task_id", task.ID),
			log.String("state", string(task.State)),
			log.Any("state_updated_at", task.StateUpdatedAt))
		if c.metrics != nil {
			c.metrics.TaskTimeout()
		}

		if c.onTimeout != nil {
			c.onTimeout(task.ID, task.State)
		}
	}

	// 计算下次唤醒间隔
	var nextWake time.Duration
	c.heapMu.Lock()
	if len(c.heap) > 0 {
		nextWake = c.heap[0].deadline.Sub(now)
		if nextWake < 0 {
			nextWake = 0
		}
	} else {
		nextWake = c.checkInterval
	}
	c.heapMu.Unlock()

	if timeoutCount > 0 {
		c.logger.Info("Timeout check completed",
			log.Int("total_checked", totalChecked),
			log.Int("timeout_count", timeoutCount),
			log.String("next_wake", nextWake.String()))
	}

	if nextWake < 100*time.Millisecond {
		nextWake = 100 * time.Millisecond
	}

	return nextWake
}

// rebuildHeap 重建超时堆：扫描所有活跃任务并计算超时时间
func (c *TimeoutChecker) rebuildHeap() {
	now := time.Now()

	// 收集所有配置了 timeout 的状态
	stateTimeouts := make(map[TaskState]time.Duration)
	for name, stateCfg := range c.config.States {
		if stateCfg.StateTimeout == "" {
			continue
		}
		d := parseStateTimeout(stateCfg.StateTimeout)
		if d > 0 {
			stateTimeouts[TaskState(name)] = d
		}
	}

	newHeap := make(timeoutHeap, 0)

	for state, timeout := range stateTimeouts {
		tasks, err := c.store.List(TaskFilter{State: state})
		if err != nil {
			c.logger.Error("rebuildHeap: failed to list tasks", log.String("state", string(state)), log.Error(err))
			continue
		}

		for _, task := range tasks {
			deadline := c.taskDeadline(task, now, timeout)
			if deadline == nil {
				continue
			}
			newHeap = append(newHeap, heapEntry{deadline: *deadline, taskID: task.ID})
		}
	}

	heap.Init(&newHeap)

	c.heapMu.Lock()
	defer c.heapMu.Unlock()
	c.heap = newHeap
	c.heapStale = false
}

// taskDeadline 计算任务的超时截止时间
func (c *TimeoutChecker) taskDeadline(task *Task, now time.Time, stateTimeout time.Duration) *time.Time {
	if task.ArchivedAt != nil {
		return nil
	}
	if task.TimeoutAt != nil {
		return task.TimeoutAt
	}
	deadline := task.StateUpdatedAt.Add(stateTimeout)
	if deadline.Before(now) {
		// 已经超时，deadline = now，确保本次 check 能立即处理
		return &now
	}
	return &deadline
}

// isTaskTimeout 检查任务是否超时
func (c *TimeoutChecker) isTaskTimeout(task *Task, now time.Time) bool {
	// 如果设置了 TimeoutAt，直接比较
	if task.TimeoutAt != nil && now.After(*task.TimeoutAt) {
		return true
	}

	// 如果没有设置 TimeoutAt，使用状态配置的 timeout
	stateCfg, ok := c.config.States[string(task.State)]
	if !ok || stateCfg.StateTimeout == "" {
		return false
	}

	// 解析 state_timeout 字符串，"0s" 回退到默认值
	timeout := parseStateTimeout(stateCfg.StateTimeout)
	if timeout <= 0 {
		return false
	}

	// 检查是否超时
	elapsed := now.Sub(task.StateUpdatedAt)
	return elapsed > timeout
}

// GetTaskTimeout 获取任务的超时时间
func (c *TimeoutChecker) GetTaskTimeout(taskID string) (*time.Time, error) {
	task, err := c.store.Get(taskID)
	if err != nil {
		return nil, err
	}

	if task.TimeoutAt != nil {
		return task.TimeoutAt, nil
	}

	// 根据状态配置计算超时时间
	stateCfg, ok := c.config.States[string(task.State)]
	if !ok || stateCfg.StateTimeout == "" {
		return nil, fmt.Errorf("no state_timeout configured for state %s", task.State)
	}

	timeout := parseStateTimeout(stateCfg.StateTimeout)
	if timeout <= 0 {
		return nil, fmt.Errorf("invalid state_timeout for state %s: %s", task.State, stateCfg.StateTimeout)
	}

	timeoutAt := task.StateUpdatedAt.Add(timeout)
	return &timeoutAt, nil
}

// parseStateTimeout 解析 state_timeout 字符串，空字符串或 "0s" 回退到 DefaultStateTimeout
func parseStateTimeout(raw string) time.Duration {
	if raw == "" || raw == "0s" {
		return DefaultStateTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		log.GetDefault().Warn("parseStateTimeout: invalid state_timeout value, using default",
			log.String("raw", raw),
			log.String("default", DefaultStateTimeout.String()))
		return DefaultStateTimeout
	}
	return d
}
