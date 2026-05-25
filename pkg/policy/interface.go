package policy

import (
	"context"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/config"
)

// PolicyEngine 策略引擎接口
// 负责事件与策略的匹配，触发任务创建
type PolicyEngine interface {
	// Match 匹配事件与策略
	// ctx: 上下文（支持超时和取消）
	// event: 输入事件
	// 返回匹配的策略列表
	Match(ctx context.Context, event *common.Event) ([]*PolicyMatch, error)

	// Evaluate 评估单个策略
	// policyID: 策略ID
	// event: 输入事件
	// 返回是否匹配
	Evaluate(policyID string, event *common.Event) (bool, error)

	// ListPolicies 列出所有策略
	ListPolicies() ([]*Policy, error)

	// GetPolicy 获取单个策略详情
	GetPolicy(policyID string) (*Policy, error)

	// GetManager 获取策略管理器（用于HTTP API）
	GetManager() PolicyManager

	// Init 初始化策略引擎，从配置加载策略和 DSL 引擎
	Init(cfg config.ConfigManager) error

	// Start 启动策略引擎
	Start(ctx context.Context) error

	// Stop 停止策略引擎
	Stop() error

	// Health 健康检查
	Health() error

	// GetStats 获取引擎统计信息
	GetStats() EngineStats
}

// Policy 策略定义
type Policy struct {
	// ID 策略唯一标识
	ID string `json:"id" yaml:"id" validate:"required,min=1,max=100"`

	// Description 策略描述
	Description string `json:"description" yaml:"description" validate:"max=1000"`

	// Enabled 是否启用
	Enabled bool `json:"enabled" yaml:"enabled"`

	// DSL 策略表达式（DSL语言）
	DSL string `json:"dsl" yaml:"dsl" validate:"required"`

	// DSLEngine 使用的DSL引擎类型
	DSLEngine string `json:"dsl_engine" yaml:"dsl_engine" validate:"required,oneof=cel libdslgo rego"`

	// Command 动作命令（与具体 action 实现配套使用）
	Command string `json:"command" yaml:"command,omitempty"`

	// Expansion 策略自定义内容（不做策略匹配使用，仅用于事件传递）
	Expansion map[string]interface{} `json:"expansion" yaml:"expansion,omitempty"`

	// Version 乐观锁版本号
	Version int64 `json:"version" yaml:"version,omitempty"`
}

// Validate 验证策略
func (p *Policy) Validate() error {
	if err := common.ValidateStruct(p); err != nil {
		return err
	}

	// 自定义验证逻辑
	if p.DSLEngine == "" {
		p.DSLEngine = "cel" // 默认使用 CEL
	}

	return nil
}

// PolicyMatch 策略匹配结果
type PolicyMatch struct {
	// PolicyID 匹配的策略ID
	PolicyID string

	// Matched 是否匹配
	Matched bool

	// EvaluationTime 评估耗时（毫秒）
	EvaluationTime int64

	// Error 评估错误（如果有）
	Error error

	// Command 动作命令（从匹配的策略中复制，与具体 action 实现配套使用）
	Command string

	// Expansion 策略自定义内容（从匹配的策略中复制）
	Expansion map[string]interface{}
}

// PolicyStore 策略存储接口
type PolicyStore interface {
	// Get 获取策略
	Get(id string) (*Policy, error)

	// List 列出所有策略
	List() ([]*Policy, error)

	// ListEnabled 列出启用的策略
	ListEnabled() ([]*Policy, error)

	// Create 创建策略
	Create(policy *Policy) error

	// Update 更新策略
	Update(policy *Policy) error

	// Delete 删除策略
	Delete(id string) error

	// Count 获取策略数量
	Count() (int, error)
}

// PolicyManager 策略管理器接口
type PolicyManager interface {
	// AddPolicy 添加策略
	AddPolicy(policy *Policy) error

	// RemovePolicy 移除策略
	RemovePolicy(policyID string) error

	// UpdatePolicy 更新策略
	UpdatePolicy(policy *Policy) error

	// GetPolicy 获取策略
	GetPolicy(policyID string) (*Policy, error)

	// ListPolicies 列出策略
	ListPolicies() ([]*Policy, error)

	// EnablePolicy 启用策略
	EnablePolicy(policyID string) error

	// DisablePolicy 禁用策略
	DisablePolicy(policyID string) error

	// ReloadPolicies 重新加载策略
	ReloadPolicies() error

	// ValidatePolicy 校验策略 DSL 语法
	ValidatePolicy(policyID string) error

	// ValidatePolicyTemp 临时校验策略 DSL 语法（不保存）
	ValidatePolicyTemp(policy *Policy) error

	// GetCompiledProgram 获取编译后的策略程序
	GetCompiledProgram(policyID string) (*DSLExpression, error)

	// RegisterEngine 注册 DSL 引擎
	RegisterEngine(engine DSLEngine)

	// GetStats 获取管理器统计信息
	GetStats() ManagerStats
}
