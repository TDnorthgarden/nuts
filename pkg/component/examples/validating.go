package examples

import (
	"context"
	"fmt"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/component"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
)

// ValidatingComponent 验证组件
// 订阅 validating 状态的任务，进行业务验证
type ValidatingComponent struct {
	*component.BaseComponent
	validationRules []ValidationRule
}

// ValidationRule 验证规则
type ValidationRule struct {
	Name     string
	Check    func(params map[string]interface{}) (bool, string)
	Critical bool
}

// NewValidatingComponent 创建验证组件
func NewValidatingComponent(bus eventbus.EventBus, rules []ValidationRule) *ValidatingComponent {
	info := component.ComponentInfo{
		Name:         "validating",
		Version:      "1.0.0",
		HandlesState: "validating",
		NextState:    "processing",
		FailureState: "failed",
	}

	comp := &ValidatingComponent{
		validationRules: rules,
	}

	config := component.ComponentConfig{
		MaxConcurrent: 10,
		Timeout:       30 * time.Second,
		RetryAttempts: 3,
	}

	comp.BaseComponent = component.NewBaseComponent(info, config, bus, comp.handleEvent)
	return comp
}

// handleEvent 处理验证事件
func (c *ValidatingComponent) handleEvent(event *common.Event) error {
	taskID := event.GetPayloadString("task_id")
	if taskID == "" {
		return fmt.Errorf("task_id not found in event")
	}

	fmt.Printf("[ValidatingComponent] Processing task %s\n", taskID)

	// 获取验证参数（从 TypedPayload extensions 中读取 JSON 字符串并解析）
	params := event.GetPayloadMap("parameters")

	// 执行验证
	passed, reason := c.validate(params)

	// 发布状态转换命令
	ctx := event.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if passed {
		return c.PublishStateTransition(ctx, taskID, "validating", "processing", true, reason)
	}

	return c.PublishStateTransition(ctx, taskID, "validating", "processing", false, reason)
}

// validate 执行所有验证规则
func (c *ValidatingComponent) validate(params map[string]interface{}) (bool, string) {
	for _, rule := range c.validationRules {
		passed, reason := rule.Check(params)
		if !passed {
			if rule.Critical {
				return false, fmt.Sprintf("validation failed: %s - %s", rule.Name, reason)
			}
			fmt.Printf("[ValidatingComponent] Warning: %s - %s\n", rule.Name, reason)
		}
	}
	return true, "all validations passed"
}

// DefaultValidationRules 默认验证规则
func DefaultValidationRules() []ValidationRule {
	return []ValidationRule{
		{
			Name: "task_id_present",
			Check: func(params map[string]interface{}) (bool, string) {
				if params == nil {
					return true, "" // params 可能不存在，不算失败
				}
				return true, ""
			},
			Critical: false,
		},
	}
}
