package examples

import (
	"fmt"
	"sync"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/component"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
)

// FailoverComponent 故障转移组件
// 监听失败的任务，尝试恢复或转移
type FailoverComponent struct {
	*component.BaseComponent
	retryPolicy RetryPolicy
	retryCounts map[string]int // taskID -> retry count
	mu          sync.RWMutex
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries  int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	BackoffRate float64
}

// DefaultRetryPolicy 默认重试策略
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries:  3,
		BaseDelay:   5 * time.Second,
		MaxDelay:    5 * time.Minute,
		BackoffRate: 2.0,
	}
}

// NewFailoverComponent 创建故障转移组件
func NewFailoverComponent(bus eventbus.EventBus, policy RetryPolicy) *FailoverComponent {
	info := component.ComponentInfo{
		Name:         "failover",
		Version:      "1.0.0",
		HandlesState: "failover",
		NextState:    "completed", // 重试回初始状态
		FailureState: "abandoned",
	}

	comp := &FailoverComponent{
		retryPolicy: policy,
		retryCounts: make(map[string]int),
	}

	if comp.retryPolicy.MaxRetries == 0 {
		comp.retryPolicy = DefaultRetryPolicy()
	}

	config := component.ComponentConfig{
		MaxConcurrent: 1, // 串行处理避免状态冲突
		Timeout:       1 * time.Minute,
		RetryAttempts: 1,
	}

	comp.BaseComponent = component.NewBaseComponent(info, config, bus, comp.handleEvent)
	return comp
}

// handleEvent 处理失败事件
func (c *FailoverComponent) handleEvent(event *common.Event) error {
	taskID := event.GetPayloadString("task_id")
	if taskID == "" {
		return fmt.Errorf("task_id not found in event")
	}

	policyID := event.GetPayloadString("policy_id")
	if policyID == "" {
		return fmt.Errorf("policy_id not found in event")
	}

	ext := event.GetPayloadString("notification_channel")
	fmt.Println("notification_channel:", ext)

	ts := event.GetPayloadString("timestamp_human")
	fmt.Println("ts:", ts)
	// 获取当前重试次数
	retryCount := c.getRetryCount(taskID)

	// 检查是否超过最大重试次数
	if retryCount >= c.retryPolicy.MaxRetries {
		fmt.Printf("[FailoverComponent] Task %s exceeded max retries (%d), marking as abandoned\n",
			taskID, c.retryPolicy.MaxRetries)
		c.resetRetryCount(taskID)
		return c.PublishStateTransition(taskID, "failover", "abandoned", false, "max retries exceeded")
	}

	// 增加重试次数
	newRetryCount := c.incrementRetryCount(taskID)

	// 计算退避延迟
	delay := c.calculateBackoff(retryCount)

	fmt.Printf("[FailoverComponent] Task %s will retry in %v (attempt %d/%d)\n",
		taskID, delay, newRetryCount, c.retryPolicy.MaxRetries)

	// 延迟后发布状态转换命令（failover -> completed）
	time.AfterFunc(delay, func() {
		fmt.Printf("[FailoverComponent] Completing task %s (attempt %d)\n", taskID, newRetryCount)
		if err := c.PublishStateTransition(taskID, "failover", "completed", true,
			fmt.Sprintf("failover handled, attempt %d", newRetryCount)); err != nil {
			fmt.Printf("[FailoverComponent] Failed to publish transition: %v\n", err)
		}
	})

	return nil
}

// getRetryCount 获取任务重试次数
func (c *FailoverComponent) getRetryCount(taskID string) int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.retryCounts[taskID]
}

// incrementRetryCount 增加重试次数
func (c *FailoverComponent) incrementRetryCount(taskID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retryCounts[taskID]++
	return c.retryCounts[taskID]
}

// resetRetryCount 重置重试次数（任务成功时调用）
func (c *FailoverComponent) resetRetryCount(taskID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.retryCounts, taskID)
}

// calculateBackoff 计算退避延迟
func (c *FailoverComponent) calculateBackoff(attempt int) time.Duration {
	delay := c.retryPolicy.BaseDelay

	for i := 0; i < attempt; i++ {
		delay = time.Duration(float64(delay) * c.retryPolicy.BackoffRate)
	}

	if delay > c.retryPolicy.MaxDelay {
		delay = c.retryPolicy.MaxDelay
	}

	return delay
}
