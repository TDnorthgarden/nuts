package policy

import (
	"fmt"
	"sync"
)

// DefaultPolicyManager 默认策略管理器实现
type DefaultPolicyManager struct {
	store    PolicyStore
	engines  map[string]DSLEngine
	programs map[string]*DSLExpression // policyID -> compiled program
	mu       sync.RWMutex
}

// NewDefaultPolicyManager 创建默认策略管理器
func NewDefaultPolicyManager(store PolicyStore) *DefaultPolicyManager {
	return &DefaultPolicyManager{
		store:    store,
		engines:  make(map[string]DSLEngine),
		programs: make(map[string]*DSLExpression),
	}
}

// RegisterEngine 注册DSL引擎
func (m *DefaultPolicyManager) RegisterEngine(engine DSLEngine) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.engines[engine.GetType()] = engine
}

// AddPolicy 添加策略
func (m *DefaultPolicyManager) AddPolicy(policy *Policy) error {
	// 验证策略
	if err := m.validatePolicy(policy); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// 存储策略
	if err := m.store.Create(policy); err != nil {
		return fmt.Errorf("store policy: %w", err)
	}

	// 编译策略表达式（持写锁防止与 ReloadPolicies 竞争）
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.compilePolicy(policy); err != nil {
		// 编译失败，删除策略
		m.store.Delete(policy.ID)
		return fmt.Errorf("compile policy: %w", err)
	}

	return nil
}

// RemovePolicy 移除策略
func (m *DefaultPolicyManager) RemovePolicy(policyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 删除存储
	if err := m.store.Delete(policyID); err != nil {
		return err
	}

	// 清除编译缓存
	if program, ok := m.programs[policyID]; ok {
		program.Close()
		delete(m.programs, policyID)
	}

	return nil
}

// UpdatePolicy 更新策略
func (m *DefaultPolicyManager) UpdatePolicy(policy *Policy) error {
	// 验证策略
	if err := m.validatePolicy(policy); err != nil {
		return fmt.Errorf("policy validation failed: %w", err)
	}

	// 更新存储
	if err := m.store.Update(policy); err != nil {
		return fmt.Errorf("update policy: %w", err)
	}

	// 重新编译（持写锁防止与 ReloadPolicies 竞争）
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.compilePolicy(policy); err != nil {
		return fmt.Errorf("recompile policy: %w", err)
	}

	return nil
}

// GetPolicy 获取策略
func (m *DefaultPolicyManager) GetPolicy(policyID string) (*Policy, error) {
	return m.store.Get(policyID)
}

// ListPolicies 列出策略
func (m *DefaultPolicyManager) ListPolicies() ([]*Policy, error) {
	return m.store.List()
}

// EnablePolicy 启用策略
func (m *DefaultPolicyManager) EnablePolicy(policyID string) error {
	policy, err := m.store.Get(policyID)
	if err != nil {
		return err
	}

	policy.Enabled = true
	return m.UpdatePolicy(policy)
}

// DisablePolicy 禁用策略
func (m *DefaultPolicyManager) DisablePolicy(policyID string) error {
	policy, err := m.store.Get(policyID)
	if err != nil {
		return err
	}

	policy.Enabled = false
	return m.UpdatePolicy(policy)
}

// ReloadPolicies 重新加载策略
func (m *DefaultPolicyManager) ReloadPolicies() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 清除所有编译缓存
	for _, program := range m.programs {
		program.Close()
	}
	m.programs = make(map[string]*DSLExpression)

	// 重新编译所有策略
	policies, err := m.store.List()
	if err != nil {
		return err
	}

	for _, policy := range policies {
		if err := m.compilePolicy(policy); err != nil {
			return fmt.Errorf("recompile policy %s: %w", policy.ID, err)
		}
	}

	return nil
}

// GetCompiledProgram 获取编译后的策略程序
func (m *DefaultPolicyManager) GetCompiledProgram(policyID string) (*DSLExpression, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	program, ok := m.programs[policyID]
	if !ok {
		return nil, fmt.Errorf("policy not compiled: %s", policyID)
	}

	return program, nil
}

// validatePolicy 验证策略
func (m *DefaultPolicyManager) validatePolicy(policy *Policy) error {
	if policy.ID == "" {
		return fmt.Errorf("policy id is required")
	}
	if policy.DSL == "" {
		return fmt.Errorf("policy DSL is required")
	}
	if policy.DSLEngine == "" {
		return fmt.Errorf("policy DSL engine is required")
	}

	// 验证DSL引擎存在
	m.mu.RLock()
	defer m.mu.RUnlock()
	engine, ok := m.engines[policy.DSLEngine]

	if !ok {
		return fmt.Errorf("DSL engine not found: %s", policy.DSLEngine)
	}

	// 验证DSL语法
	if err := engine.Validate(policy.DSL); err != nil {
		return fmt.Errorf("DSL validation failed: %w", err)
	}

	return nil
}

// ValidatePolicyTemp 临时校验策略 DSL 语法（不保存到存储）
func (m *DefaultPolicyManager) ValidatePolicyTemp(policy *Policy) error {
	// 验证必需字段
	if policy.DSL == "" {
		return fmt.Errorf("policy DSL is required")
	}
	if policy.DSLEngine == "" {
		return fmt.Errorf("policy DSL engine is required")
	}

	// 验证 DSL 引擎存在
	m.mu.RLock()
	defer m.mu.RUnlock()
	engine, ok := m.engines[policy.DSLEngine]

	if !ok {
		return fmt.Errorf("DSL engine not found: %s", policy.DSLEngine)
	}

	// 验证 DSL 语法
	if err := engine.Validate(policy.DSL); err != nil {
		return fmt.Errorf("DSL validation failed: %w", err)
	}

	return nil
}

// compilePolicy 编译策略
// 注意：调用者必须已经持有 m.mu 锁
func (m *DefaultPolicyManager) compilePolicy(policy *Policy) error {
	// 获取引擎
	engine, ok := m.engines[policy.DSLEngine]
	if !ok {
		return fmt.Errorf("DSL engine not found: %s", policy.DSLEngine)
	}

	// 编译表达式
	program, err := engine.Compile(policy.DSL)
	if err != nil {
		return err
	}

	// 存储编译结果
	expression := &DSLExpression{
		Raw:      policy.DSL,
		Compiled: program,
		Engine:   engine,
	}

	// 清除旧程序
	if oldProgram, ok := m.programs[policy.ID]; ok {
		oldProgram.Close()
	}

	m.programs[policy.ID] = expression

	return nil
}

// GetStats 获取管理器统计信息
func (m *DefaultPolicyManager) GetStats() ManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	total, _ := m.store.Count()
	enabled, _ := m.store.ListEnabled()

	return ManagerStats{
		TotalPolicies:     total,
		EnabledPolicies:   len(enabled),
		CompiledPolicies:  len(m.programs),
		RegisteredEngines: len(m.engines),
	}
}

// ManagerStats 管理器统计信息
type ManagerStats struct {
	TotalPolicies     int
	EnabledPolicies   int
	CompiledPolicies  int
	RegisteredEngines int
}

// ValidatePolicy 校验策略 DSL 语法
func (m *DefaultPolicyManager) ValidatePolicy(policyID string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 获取策略
	policy, err := m.store.Get(policyID)
	if err != nil {
		return fmt.Errorf("get policy: %w", err)
	}

	// 验证 DSL 语法
	engine, ok := m.engines[policy.DSLEngine]
	if !ok {
		return fmt.Errorf("DSL engine not found: %s", policy.DSLEngine)
	}

	if err := engine.Validate(policy.DSL); err != nil {
		return fmt.Errorf("DSL validation failed: %w", err)
	}

	return nil
}
