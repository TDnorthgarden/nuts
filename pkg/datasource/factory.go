package datasource

import (
	"fmt"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// init 注册内置类型
func init() {
	// 注册Mock数据源
	Factory.Register(
		"mock",
		ParseMockConfig,
		CreateMockDataSource,
	)

	// 注册NRI数据源
	Factory.Register(
		"nri",
		ParseNRIConfig,
		CreateNRIDataSource,
	)

	// 注册Containerd数据源
	Factory.Register(
		"containerd",
		ParseContainerdConfig,
		CreateContainerdDataSource,
	)
}

// DataSourceFactory 数据源工厂
// 支持通过配置动态创建数据源实例
type DataSourceFactory struct {
	parsers  map[string]ConfigParser
	creators map[string]CreatorFunc
	mu       sync.RWMutex
}

// ConfigParser 配置解析函数
type ConfigParser func(config interface{}) (DataSourceConfig, error)

// CreatorFunc 数据源创建函数
type CreatorFunc func(config DataSourceConfig) (DataSource, error)

// DataSourceFactoryFunc 数据源工厂函数类型
type DataSourceFactoryFunc func(name string, config map[string]interface{}) (DataSource, error)

// Factory 全局工厂实例
var Factory = &DataSourceFactory{
	parsers:  make(map[string]ConfigParser),
	creators: make(map[string]CreatorFunc),
}

// Register 注册数据源类型
func (f *DataSourceFactory) Register(
	typeName string,
	parser ConfigParser,
	creator CreatorFunc,
) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.parsers[typeName] = parser
	f.creators[typeName] = creator
}

// Create 根据配置创建数据源
func (f *DataSourceFactory) Create(configStr string) (DataSource, error) {
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
		return nil, fmt.Errorf("unknown datasource type: %s", cfg.Type)
	}

	// 解析完整配置
	config, err := parser(configStr)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// 验证配置
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	// 创建实例
	creator, ok := f.creators[cfg.Type]
	if !ok {
		return nil, fmt.Errorf("no creator for type: %s", cfg.Type)
	}

	return creator(config)
}

// CreateWithMap 从 map 配置创建数据源
func (f *DataSourceFactory) CreateWithMap(config map[string]interface{}) (DataSource, error) {
	dsType, ok := config["type"].(string)
	if !ok || dsType == "" {
		return nil, fmt.Errorf("config type is required")
	}

	f.mu.RLock()
	defer f.mu.RUnlock()

	// 查找解析器 - 对于 map 配置，直接使用 map 作为配置
	parser, ok := f.parsers[dsType]
	if !ok {
		return nil, fmt.Errorf("unknown datasource type: %s", dsType)
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
	if err := parsedConfig.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	// 创建实例
	creator, ok := f.creators[dsType]
	if !ok {
		return nil, fmt.Errorf("no creator for type: %s", dsType)
	}

	return creator(parsedConfig)
}

// GetSupportedTypes 获取支持的类型列表
func (f *DataSourceFactory) GetSupportedTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// ParseMockConfig 解析Mock配置
func ParseMockConfig(config interface{}) (DataSourceConfig, error) {
	configStr, ok := config.(string)
	if !ok {
		return nil, fmt.Errorf("config must be string")
	}

	var cfg MockDataSourceConfig
	if err := toml.Unmarshal([]byte(configStr), &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// CreateMockDataSource 创建Mock数据源
func CreateMockDataSource(config DataSourceConfig) (DataSource, error) {
	mockCfg, ok := config.(*MockDataSourceConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type")
	}

	if err := mockCfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	// 设置默认值
	if mockCfg.EventIntervalMs <= 0 {
		mockCfg.EventIntervalMs = 5000
	}
	if len(mockCfg.EventTypes) == 0 {
		mockCfg.EventTypes = []string{
			"ContainerStart",
			"ContainerStop",
			"ContainerUpdate",
			"PodCreated",
			"PodDeleted",
		}
	}

	return NewMockDataSource(mockCfg)
}
