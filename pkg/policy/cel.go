package policy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// defaultCELTimeout 默认 CEL 评估超时
const defaultCELTimeout = 5 * time.Second

// CEngine CEL引擎实现
type CEngine struct {
	env *cel.Env
	mu  sync.RWMutex
}

// NewCEngine 创建CEL引擎
func NewCEngine() (*CEngine, error) {
	// 创建CEL环境 - 使用map类型而非自定义类型
	env, err := cel.NewEnv(
		cel.Variable("event", cel.MapType(cel.StringType, cel.DynType)),
	)
	if err != nil {
		return nil, fmt.Errorf("create CEL env: %w", err)
	}

	return &CEngine{env: env}, nil
}

// Compile 编译CEL表达式
func (e *CEngine) Compile(dsl string) (DSLProgram, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// 解析表达式
	ast, issues := e.env.Parse(dsl)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("parse error: %w", issues.Err())
	}

	// 检查类型
	_, issues = e.env.Check(ast)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("type check error: %w", issues.Err())
	}

	// 生成程序
	prog, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program creation error: %w", err)
	}

	return &CELProgram{program: prog}, nil
}

// Evaluate 评估表达式
func (e *CEngine) Evaluate(program DSLProgram, data map[string]interface{}) (bool, error) {
	celProg, ok := program.(*CELProgram)
	if !ok {
		return false, fmt.Errorf("invalid program type")
	}

	// 直接使用map作为输入变量
	vars := map[string]interface{}{
		"event": data,
	}

	// 执行评估
	out, _, err := celProg.program.Eval(vars)
	if err != nil {
		return false, fmt.Errorf("evaluation error: %w", err)
	}

	// 转换结果为bool
	result, ok := out.(types.Bool)
	if !ok {
		return false, fmt.Errorf("result is not bool: %v", out)
	}

	return bool(result), nil
}

// EvaluateWithCtx 带 context 的评估表达式
func (e *CEngine) EvaluateWithCtx(ctx context.Context, program DSLProgram, data map[string]interface{}) (bool, error) {
	celProg, ok := program.(*CELProgram)
	if !ok {
		return false, fmt.Errorf("invalid program type")
	}

	// 从 context 获取超时，默认 5s
	timeout := defaultCELTimeout
	if deadline, ok := ctx.Deadline(); ok {
		if t := time.Until(deadline); t > 0 {
			timeout = t
		}
	}

	resultCh := make(chan struct {
		matched bool
		err     error
	}, 1)

	go func() {
		vars := map[string]interface{}{
			"event": data,
		}
		out, _, err := celProg.program.Eval(vars)
		if err != nil {
			resultCh <- struct {
				matched bool
				err     error
			}{false, fmt.Errorf("evaluation error: %w", err)}
			return
		}
		result, ok := out.(types.Bool)
		if !ok {
			resultCh <- struct {
				matched bool
				err     error
			}{false, fmt.Errorf("result is not bool: %v", out)}
			return
		}
		resultCh <- struct {
			matched bool
			err     error
		}{bool(result), nil}
	}()

	select {
	case res := <-resultCh:
		return res.matched, res.err
	case <-ctx.Done():
		return false, fmt.Errorf("CEL evaluation cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		return false, fmt.Errorf("CEL evaluation timed out after %v", timeout)
	}
}

// GetType 获取引擎类型
func (e *CEngine) GetType() string {
	return "cel"
}

// Validate 验证DSL语法
func (e *CEngine) Validate(dsl string) error {
	_, err := e.Compile(dsl)
	return err
}

// CELProgram CEL程序实现
type CELProgram struct {
	program cel.Program
}

// Close 释放资源
func (p *CELProgram) Close() error {
	return nil
}
// CEL表达式示例
const (
	// ExamplePolicy1 监控nginx pod启动
	ExamplePolicy1 = `event.type == "ContainerStart" && event.payload.namespace == "production" && event.payload.image.startsWith("nginx")`

	// ExamplePolicy2 清理旧pod
	ExamplePolicy2 = `event.type == "PodDeleted" && event.payload.age > 86400`

	// ExamplePolicy3 崩溃循环检测
	ExamplePolicy3 = `event.type == "ContainerStop" && event.payload.exit_code != 0 && event.payload.restart_count > 5`
)

// CELExpressionBuilder CEL表达式构建器
// 用于简化CEL表达式的构造
type CELExpressionBuilder struct {
	conditions []string
	logic      string // "AND" or "OR"
}

// NewCELExpressionBuilder 创建CEL表达式构建器
func NewCELExpressionBuilder() *CELExpressionBuilder {
	return &CELExpressionBuilder{
		conditions: make([]string, 0),
		logic:      "AND",
	}
}

// WithLogic 设置逻辑运算符
func (b *CELExpressionBuilder) WithLogic(logic string) *CELExpressionBuilder {
	b.logic = logic
	return b
}

// AddCondition 添加条件
func (b *CELExpressionBuilder) AddCondition(condition string) *CELExpressionBuilder {
	b.conditions = append(b.conditions, condition)
	return b
}

// AddEqual 添加等于条件
func (b *CELExpressionBuilder) AddEqual(field, value string) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s == "%s"`, field, value))
}

// AddNotEqual 添加不等于条件
func (b *CELExpressionBuilder) AddNotEqual(field, value string) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s != "%s"`, field, value))
}

// AddContains 添加包含条件
func (b *CELExpressionBuilder) AddContains(field, value string) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s.contains("%s")`, field, value))
}

// AddStartsWith 添加前缀条件
func (b *CELExpressionBuilder) AddStartsWith(field, value string) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s.startsWith("%s")`, field, value))
}

// AddEndsWith 添加后缀条件
func (b *CELExpressionBuilder) AddEndsWith(field, value string) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s.endsWith("%s")`, field, value))
}

// AddGreaterThan 添加大于条件
func (b *CELExpressionBuilder) AddGreaterThan(field string, value int) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s > %d`, field, value))
}

// AddLessThan 添加小于条件
func (b *CELExpressionBuilder) AddLessThan(field string, value int) *CELExpressionBuilder {
	return b.AddCondition(fmt.Sprintf(`%s < %d`, field, value))
}

// AddIn 添加包含在列表中条件
func (b *CELExpressionBuilder) AddIn(field string, values []string) *CELExpressionBuilder {
	valuesStr := fmt.Sprintf(`["%s"]`, strings.Join(values, `","`))
	return b.AddCondition(fmt.Sprintf(`%s in %s`, field, valuesStr))
}

// Build 构建CEL表达式
func (b *CELExpressionBuilder) Build() string {
	if len(b.conditions) == 0 {
		return "true"
	}

	if len(b.conditions) == 1 {
		return b.conditions[0]
	}

	joiner := " && "
	if b.logic == "OR" {
		joiner = " || "
	}

	return "(" + strings.Join(b.conditions, joiner) + ")"
}

// predefinedPolicies 预定义策略模板
var predefinedPolicies = map[string]string{
	"monitor-nginx":     ExamplePolicy1,
	"cleanup-old-pods":  ExamplePolicy2,
	"crash-loop-detect": ExamplePolicy3,
}

// GetPredefinedPolicy 获取预定义策略
func GetPredefinedPolicy(name string) (string, bool) {
	policy, ok := predefinedPolicies[name]
	return policy, ok
}

// ListPredefinedPolicies 列出预定义策略
func ListPredefinedPolicies() []string {
	names := make([]string, 0, len(predefinedPolicies))
	for name := range predefinedPolicies {
		names = append(names, name)
	}
	return names
}
