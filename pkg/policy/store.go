package policy

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// MemoryPolicyStore 内存策略存储实现
type MemoryPolicyStore struct {
	policies map[string]*Policy
	mu       sync.RWMutex
}

// NewMemoryPolicyStore 创建内存策略存储
func NewMemoryPolicyStore() *MemoryPolicyStore {
	return &MemoryPolicyStore{
		policies: make(map[string]*Policy),
	}
}

// Get 获取策略
func (s *MemoryPolicyStore) Get(id string) (*Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policy, exists := s.policies[id]
	if !exists {
		return nil, fmt.Errorf("policy not found: %s", id)
	}

	// 返回副本
	return s.copyPolicy(policy), nil
}

// List 列出所有策略
func (s *MemoryPolicyStore) List() ([]*Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policies := make([]*Policy, 0, len(s.policies))
	for _, policy := range s.policies {
		policies = append(policies, s.copyPolicy(policy))
	}

	return policies, nil
}

// ListEnabled 列出启用的策略
func (s *MemoryPolicyStore) ListEnabled() ([]*Policy, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	policies := make([]*Policy, 0)
	for _, policy := range s.policies {
		if policy.Enabled {
			policies = append(policies, s.copyPolicy(policy))
		}
	}

	return policies, nil
}

// Create 创建策略
func (s *MemoryPolicyStore) Create(policy *Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if policy.ID == "" {
		return fmt.Errorf("policy id is required")
	}

	if _, exists := s.policies[policy.ID]; exists {
		return fmt.Errorf("policy already exists: %s", policy.ID)
	}

	// 初始化版本号
	policy.Version = 1

	// 存储副本
	s.policies[policy.ID] = s.copyPolicy(policy)

	return nil
}

// Update 更新策略（乐观锁）
func (s *MemoryPolicyStore) Update(policy *Policy) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.policies[policy.ID]
	if !exists {
		return fmt.Errorf("policy not found: %s", policy.ID)
	}

	// 检查版本号
	if policy.Version != existing.Version {
		return fmt.Errorf("version conflict: expected %d, got %d", existing.Version, policy.Version)
	}

	// 递增版本号
	policy.Version = existing.Version + 1

	// 存储副本
	s.policies[policy.ID] = s.copyPolicy(policy)

	return nil
}

// Delete 删除策略
func (s *MemoryPolicyStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.policies[id]; !exists {
		return fmt.Errorf("policy not found: %s", id)
	}

	delete(s.policies, id)
	return nil
}

// Count 获取策略数量
func (s *MemoryPolicyStore) Count() (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return len(s.policies), nil
}

// copyPolicy 复制策略
func (s *MemoryPolicyStore) copyPolicy(policy *Policy) *Policy {
	expCopy := make(map[string]interface{}, len(policy.Expansion))
	for k, v := range policy.Expansion {
		expCopy[k] = v
	}
	return &Policy{
		ID:          policy.ID,
		Description: policy.Description,
		Enabled:     policy.Enabled,
		DSL:         policy.DSL,
		DSLEngine:   policy.DSLEngine,
		Expansion:   expCopy,
		Timeout:     policy.Timeout,
		Version:     policy.Version,
	}
}

// CachedPolicyStore 带缓存的策略存储
type CachedPolicyStore struct {
	store     PolicyStore
	cache     map[string]*Policy
	cacheMu   sync.RWMutex
	hitCount  atomic.Int64
	missCount atomic.Int64
}

// NewCachedPolicyStore 创建带缓存的策略存储
func NewCachedPolicyStore(store PolicyStore) *CachedPolicyStore {
	return &CachedPolicyStore{
		store: store,
		cache: make(map[string]*Policy),
	}
}

// Get 获取策略（带缓存）
func (s *CachedPolicyStore) Get(id string) (*Policy, error) {
	s.cacheMu.RLock()
	if policy, ok := s.cache[id]; ok {
		s.cacheMu.RUnlock()
		s.hitCount.Add(1)
		return policy, nil
	}
	s.cacheMu.RUnlock()

	s.missCount.Add(1)

	// 从底层存储获取
	policy, err := s.store.Get(id)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	s.cacheMu.Lock()
	s.cache[id] = policy
	s.cacheMu.Unlock()

	return policy, nil
}

// List 列出所有策略
func (s *CachedPolicyStore) List() ([]*Policy, error) {
	return s.store.List()
}

// ListEnabled 列出启用的策略
func (s *CachedPolicyStore) ListEnabled() ([]*Policy, error) {
	return s.store.ListEnabled()
}

// Create 创建策略
func (s *CachedPolicyStore) Create(policy *Policy) error {
	if err := s.store.Create(policy); err != nil {
		return err
	}

	// 更新缓存
	s.cacheMu.Lock()
	s.cache[policy.ID] = policy
	s.cacheMu.Unlock()

	return nil
}

// Update 更新策略
func (s *CachedPolicyStore) Update(policy *Policy) error {
	if err := s.store.Update(policy); err != nil {
		return err
	}

	// 更新缓存
	s.cacheMu.Lock()
	s.cache[policy.ID] = policy
	s.cacheMu.Unlock()

	return nil
}

// Delete 删除策略
func (s *CachedPolicyStore) Delete(id string) error {
	if err := s.store.Delete(id); err != nil {
		return err
	}

	// 清除缓存
	s.cacheMu.Lock()
	delete(s.cache, id)
	s.cacheMu.Unlock()

	return nil
}

// Count 获取策略数量
func (s *CachedPolicyStore) Count() (int, error) {
	return s.store.Count()
}

// InvalidateCache 使缓存失效
func (s *CachedPolicyStore) InvalidateCache() {
	s.cacheMu.Lock()
	s.cache = make(map[string]*Policy)
	s.cacheMu.Unlock()
}

// GetCacheStats 获取缓存统计
func (s *CachedPolicyStore) GetCacheStats() CacheStats {
	return CacheStats{
		HitCount:  s.hitCount.Load(),
		MissCount: s.missCount.Load(),
		Size:      len(s.cache),
	}
}

// CacheStats 缓存统计
type CacheStats struct {
	HitCount  int64
	MissCount int64
	Size      int
}

// HitRate 命中率
func (s *CacheStats) HitRate() float64 {
	total := s.HitCount + s.MissCount
	if total == 0 {
		return 0
	}
	return float64(s.HitCount) / float64(total)
}
