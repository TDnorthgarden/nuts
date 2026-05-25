package eventbus

import (
	"fmt"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// Factory EventBus全局工厂实例
var Factory = &EventBusFactory{
	parsers:    make(map[string]ConfigParser),
	creators:   make(map[string]CreatorFunc),
	validators: make(map[string]ValidatorFunc),
}

// ConfigParser 配置解析函数
type ConfigParser func(config interface{}) (interface{}, error)

// CreatorFunc EventBus创建函数
type CreatorFunc func(cfg interface{}) (EventBus, error)

// ValidatorFunc 配置验证函数
type ValidatorFunc func(cfg interface{}) error

// EventBusFactory EventBus工厂
type EventBusFactory struct {
	mu         sync.RWMutex
	parsers    map[string]ConfigParser
	creators   map[string]CreatorFunc
	validators map[string]ValidatorFunc
}

// Register 注册EventBus类型
func (f *EventBusFactory) Register(
	typeName string,
	parser ConfigParser,
	validator ValidatorFunc,
	creator CreatorFunc,
) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.parsers[typeName] = parser
	f.validators[typeName] = validator
	f.creators[typeName] = creator
}

// Create 根据配置创建EventBus
func (f *EventBusFactory) Create(configStr string) (EventBus, error) {
	// 解析配置获取类型
	var cfg struct {
		Type string `toml:"type"`
	}
	if err := toml.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, fmt.Errorf("parse config type: %w", err)
	}

	if cfg.Type == "" {
		return nil, fmt.Errorf("config type is required")
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	// 查找解析器
	parser, ok := f.parsers[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("unknown eventbus type: %s", cfg.Type)
	}

	// 解析完整配置
	parsedCfg, err := parser(configStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// 验证配置
	validator, ok := f.validators[cfg.Type]
	if ok {
		if err := validator(parsedCfg); err != nil {
			return nil, fmt.Errorf("validate config: %w", err)
		}
	}

	// 创建实例
	creator, ok := f.creators[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("no creator for type: %s", cfg.Type)
	}

	return creator(parsedCfg)
}

// CreateWithMap 从 map 配置创建EventBus
func (f *EventBusFactory) CreateWithMap(config map[string]interface{}) (EventBus, error) {
	ebType, ok := config["type"].(string)
	if !ok || ebType == "" {
		return nil, fmt.Errorf("config type is required")
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	// 查找解析器 - 对于 map 配置，直接使用 map 作为配置
	parser, ok := f.parsers[ebType]
	if !ok {
		return nil, fmt.Errorf("unknown eventbus type: %s", ebType)
	}

	// 将 map 转为 TOML 字符串后解析（复用现有解析器）
	configBytes, err := toml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	// 解析完整配置
	parsedConfig, err := parser(string(configBytes))
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// 验证配置
	validator, ok := f.validators[ebType]
	if ok {
		if err := validator(parsedConfig); err != nil {
			return nil, fmt.Errorf("validate config: %w", err)
		}
	}

	// 创建实例
	creator, ok := f.creators[ebType]
	if !ok {
		return nil, fmt.Errorf("no creator for type: %s", ebType)
	}

	return creator(parsedConfig)
}

// GetSupportedTypes 获取支持的类型列表
func (f *EventBusFactory) GetSupportedTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// GRPCConfig gRPC配置
type GRPCConfig struct {
	Type    string `toml:"type"`
	Address string `toml:"address"`
	// TODO: 添加TLS等配置
}

// ParseGRPCConfig 解析gRPC配置
func ParseGRPCConfig(config interface{}) (interface{}, error) {
	configStr, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("config must be string")
	}

	var cfg GRPCConfig
	if err := toml.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ValidateGRPCConfig 验证gRPC配置
func ValidateGRPCConfig(cfg interface{}) error {
	grpcCfg, ok := cfg.(*GRPCConfig)
	if !ok {
		return fmt.Errorf("invalid config type")
	}

	if grpcCfg.Address == "" {
		return fmt.Errorf("address is required")
	}

	return nil
}
