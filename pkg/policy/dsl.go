package policy

import (
	"context"
	"fmt"
)

// DSLEngine DSL引擎接口
// 抽象不同DSL语言的实现（CEL、libdslgo、Rego）
type DSLEngine interface {
	// Compile 编译DSL表达式
	// dsl: DSL表达式字符串
	// 返回编译后的程序对象
	Compile(dsl string) (DSLProgram, error)

	// Evaluate 评估表达式
	// program: 编译后的程序
	// data: 输入数据（通常是Event的payload）
	// 返回评估结果
	Evaluate(program DSLProgram, data map[string]interface{}) (bool, error)

	// EvaluateWithCtx 带 context 的评估表达式（支持超时和取消）
	EvaluateWithCtx(ctx context.Context, program DSLProgram, data map[string]interface{}) (bool, error)

	// GetType 获取引擎类型
	GetType() string

	// Validate 验证DSL语法
	Validate(dsl string) error
}

// DSLProgram 编译后的程序对象
// 具体内容由各DSL引擎实现定义
type DSLProgram interface {
	// Close 释放程序资源
	Close() error
}

// EvaluationResult 评估结果
type EvaluationResult struct {
	// Matched 是否匹配
	Matched bool

	// Error 评估错误
	Error error

	// EvaluationTime 评估耗时（纳秒）
	EvaluationTime int64

	// Variables 评估过程中的变量（用于调试）
	Variables map[string]interface{}
}

// DSLEngineConfig DSL引擎配置
type DSLEngineConfig struct {
	// Type 引擎类型
	Type string `json:"type"` // "cel", "libdslgo", "rego"

	// EnableDebug 是否启用调试模式
	EnableDebug bool `json:"enable_debug"`
}

// Validate 验证配置
func (c *DSLEngineConfig) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("type is required")
	}
	return nil
}

// DSLExpression DSL表达式封装
type DSLExpression struct {
	// Raw 原始表达式
	Raw string

	// Compiled 编译后的程序
	Compiled DSLProgram

	// Engine 使用的引擎
	Engine DSLEngine
}

// Evaluate 评估表达式
func (e *DSLExpression) Evaluate(data map[string]interface{}) (bool, error) {
	if e.Compiled == nil {
		return false, fmt.Errorf("expression not compiled")
	}
	return e.Engine.Evaluate(e.Compiled, data)
}

// EvaluateWithCtx 带 context 的评估表达式
func (e *DSLExpression) EvaluateWithCtx(ctx context.Context, data map[string]interface{}) (bool, error) {
	if e.Compiled == nil {
		return false, fmt.Errorf("expression not compiled")
	}
	return e.Engine.EvaluateWithCtx(ctx, e.Compiled, data)
}

// Close 释放资源
func (e *DSLExpression) Close() error {
	if e.Compiled != nil {
		return e.Compiled.Close()
	}
	return nil
}
