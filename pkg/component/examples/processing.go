package examples

import (
	"fmt"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/component"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
)

// ProcessingComponent 处理组件
// 订阅 processing 状态的任务，执行业务处理
type ProcessingComponent struct {
	*component.BaseComponent
	processor TaskProcessor
}

// TaskProcessor 任务处理器接口
type TaskProcessor interface {
	Process(taskID string, params map[string]interface{}) (map[string]interface{}, error)
}

// DefaultProcessor 默认处理器
type DefaultProcessor struct{}

func (p *DefaultProcessor) Process(taskID string, params map[string]interface{}) (map[string]interface{}, error) {
	// 模拟处理
	time.Sleep(100 * time.Millisecond)

	result := map[string]interface{}{
		"task_id":   taskID,
		"processed": true,
		"timestamp": time.Now().Unix(),
	}

	// 复制输入参数
	for k, v := range params {
		result["input_"+k] = v
	}

	return result, nil
}

// NewProcessingComponent 创建处理组件
func NewProcessingComponent(bus eventbus.EventBus, processor TaskProcessor) *ProcessingComponent {
	info := component.ComponentInfo{
		Name:         "processing",
		Version:      "1.0.0",
		HandlesState: "processing",
		NextState:    "failover",
		FailureState: "failed",
	}

	comp := &ProcessingComponent{
		processor: processor,
	}

	if comp.processor == nil {
		comp.processor = &DefaultProcessor{}
	}

	config := component.ComponentConfig{
		MaxConcurrent: 20,
		Timeout:       5 * time.Minute,
		RetryAttempts: 3,
	}

	comp.BaseComponent = component.NewBaseComponent(info, config, bus, comp.handleEvent)
	return comp
}

// handleEvent 处理事件
// ProcessingComponent is now a pass-through: action execution is handled by core.go
// when it receives the task.state_changed_processing event.
// This component simply transitions to failover to continue the pipeline.
func (c *ProcessingComponent) handleEvent(event *common.Event) error {
	taskID := event.GetPayloadString("task_id")
	if taskID == "" {
		return fmt.Errorf("task_id not found in event")
	}

	fmt.Printf("[ProcessingComponent] Passing through task %s (action execution delegated to core)\n", taskID)

	// Pass-through: transition directly to failover
	// The actual action execution happens in core.go's processing handler
	return c.PublishStateTransition(taskID, "processing", "failover", true, "processing pass-through")
}
