package policy

import (
	"fmt"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// DSLEngineFactory DSL引擎工厂
// 支持动态创建DSL引擎实例
type DSLEngineFactory struct {
	creators map[string]CreatorFunc
	mu       sync.RWMutex
}

// CreatorFunc DSL引擎创建函数
type CreatorFunc func(config *DSLEngineConfig) (DSLEngine, error)

// Factory 全局工厂实例
var Factory = &DSLEngineFactory{
	creators: make(map[string]CreatorFunc),
}

// Register 注册DSL引擎类型
func (f *DSLEngineFactory) Register(
	typeName string,
	creator CreatorFunc,
) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.creators[typeName] = creator
}

// Create 根据配置创建DSL引擎
func (f *DSLEngineFactory) Create(config *DSLEngineConfig) (DSLEngine, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	creator, ok := f.creators[config.Type]
	if !ok {
		return nil, fmt.Errorf("unknown DSL engine type: %s", config.Type)
	}

	return creator(config)
}

// GetSupportedTypes 获取支持的类型列表
func (f *DSLEngineFactory) GetSupportedTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// CreateWithMap 从 map 配置创建DSL引擎
func (f *DSLEngineFactory) CreateWithMap(config map[string]interface{}) (DSLEngine, error) {
	// 获取类型
	dsleType, ok := config["type"].(string)
	if !ok || dsleType == "" {
		return nil, fmt.Errorf("config type is required")
	}

	// 将 map 转为 TOML 字符串后解析（复用现有解析逻辑）
	configBytes, err := toml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	// 解析为 DSLEngineConfig
	var cfg DSLEngineConfig
	if err := toml.Unmarshal(configBytes, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return f.Create(&cfg)
}

// init 注册内置类型
func init() {
	// 注册CEL引擎
	Factory.Register("cel", CreateCEngine)
}

// CreateCEngine 创建CEL引擎
func CreateCEngine(config *DSLEngineConfig) (DSLEngine, error) {
	return NewCEngine()
}

// EnginePool DSL引擎池
// 用于复用引擎实例，减少创建开销
type EnginePool struct {
	pool map[string]DSLEngine
	mu   sync.RWMutex
}

// NewEnginePool 创建引擎池
func NewEnginePool() *EnginePool {
	return &EnginePool{
		pool: make(map[string]DSLEngine),
	}
}

// Get 获取引擎
func (p *EnginePool) Get(engineType string) (DSLEngine, error) {
	p.mu.RLock()
	engine, ok := p.pool[engineType]
	p.mu.RUnlock()

	if ok {
		return engine, nil
	}

	// 创建新引擎
	config := &DSLEngineConfig{Type: engineType}
	newEngine, err := Factory.Create(config)
	if err != nil {
		return nil, err
	}

	// 双检锁：创建完成后再次检查，避免重复创建
	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.pool[engineType]; ok {
		return existing, nil
	}
	p.pool[engineType] = newEngine
	return newEngine, nil
}

// Close 关闭引擎池
func (p *EnginePool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 清空池
	p.pool = make(map[string]DSLEngine)
	return nil
}

// GetStats 获取池统计
func (p *EnginePool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PoolStats{
		TotalEngines: len(p.pool),
	}
}

// PoolStats 池统计
type PoolStats struct {
	TotalEngines int
}
