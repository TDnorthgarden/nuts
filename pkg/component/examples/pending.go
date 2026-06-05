package examples

import (
	"context"
	"fmt"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/component"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
)

// PendingComponent 待处理组件
// 订阅 pending 状态的任务，自动转换到 validating 状态
type PendingComponent struct {
	*component.BaseComponent
}

// NewPendingComponent 创建待处理组件
func NewPendingComponent(bus eventbus.EventBus) *PendingComponent {
	info := component.ComponentInfo{
		Name:         "pending",
		Version:      "1.0.0",
		HandlesState: "pending",
		NextState:    "validating",
		FailureState: "failed",
	}

	comp := &PendingComponent{}

	config := component.ComponentConfig{
		MaxConcurrent: 10,
		Timeout:       30, // 30 seconds
		RetryAttempts: 3,
	}

	comp.BaseComponent = component.NewBaseComponent(info, config, bus, comp.handleEvent)
	return comp
}

// handleEvent 处理事件
func (c *PendingComponent) handleEvent(event *common.Event) error {
	taskID := event.GetPayloadString("task_id")
	if taskID == "" {
		return fmt.Errorf("task_id not found in event")
	}

	fmt.Printf("[PendingComponent] Processing task %s\n", taskID)

	// 自动转换到 validating 状态
	ctx := event.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return c.PublishStateTransition(ctx, taskID, "pending", "validating", true, "auto transition to validating")
}
