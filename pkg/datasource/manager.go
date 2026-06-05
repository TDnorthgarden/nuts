package datasource

import (
	"context"
	"fmt"
	"sync"

	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/config"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

// DataSourceManager 数据源管理器
// 管理多个数据源的生命周期、事件聚合和动态切换
// 设计约定：同一时刻仅生效一个数据源
type DataSourceManager struct {
	sources map[string]DataSourceWrapper
	mu      sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc

	// 事件聚合channel
	eventCh chan<- *common.Event

	// 当前活跃数据源名称（设计约定：同一时刻仅一个活跃）
	activeName string

	// 数据源工厂注册表
	factories map[string]DataSourceFactoryFunc

	// 配置管理器（用于惰性创建数据源实例）
	cfg config.ConfigManager

	// 日志记录器
	logger log.Logger
}

// DataSourceWrapper 数据源包装器
type DataSourceWrapper struct {
	DataSource DataSource
	Config     DataSourceConfig
	Active     bool
}

// DatasourceInfo 数据源信息（用于API返回）
type DatasourceInfo struct {
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// NewDataSourceManager 创建数据源管理器
func NewDataSourceManager(eventCh chan<- *common.Event) *DataSourceManager {
	return &DataSourceManager{
		sources:   make(map[string]DataSourceWrapper),
		factories: make(map[string]DataSourceFactoryFunc),
		eventCh:   eventCh,
		logger:    log.GetDefault(), // 使用默认 logger
	}
}

// SetLogger 设置日志记录器
func (m *DataSourceManager) SetLogger(logger log.Logger) {
	m.logger = logger
}

// Register 注册数据源
func (m *DataSourceManager) Register(name string, source DataSource, config DataSourceConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sources[name]; exists {
		return fmt.Errorf("datasource %s already registered", name)
	}

	m.sources[name] = DataSourceWrapper{
		DataSource: source,
		Config:     config,
		Active:     false,
	}

	return nil
}

// Unregister 注销数据源
func (m *DataSourceManager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return fmt.Errorf("datasource %s not found", name)
	}

	// 如果激活，先停止
	if wrapper.Active {
		if err := wrapper.DataSource.Stop(); err != nil {
			return fmt.Errorf("stop datasource: %w", err)
		}
	}

	if m.activeName == name {
		m.activeName = ""
	}

	delete(m.sources, name)
	return nil
}

// Start 启动所有数据源
func (m *DataSourceManager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 从传入 context 派生，确保 core 的取消传播到数据源
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(ctx)

	startedCount := 0
	for name, wrapper := range m.sources {
		if wrapper.Active {
			continue
		}

		// 启动数据源
		// 配置已经在工厂创建时解析并传递给数据源实例
		// 这里不需要再次传递配置
		if err := wrapper.DataSource.Start(m.ctx, m.eventCh); err != nil {
			return fmt.Errorf("start datasource %s: %w", name, err)
		}

		wrapper.Active = true
		m.sources[name] = wrapper
		m.activeName = name
		startedCount++
		m.logger.Info("Datasource started", log.String("name", name))
	}

	if startedCount == 0 {
		m.logger.Info("No datasources to start")
	}

	return nil
}

// StartByName 启动指定数据源
func (m *DataSourceManager) StartByName(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return fmt.Errorf("datasource %s not found, available: %v", name, m.getAvailableSources())
	}

	if wrapper.Active {
		return fmt.Errorf("datasource %s already active", name)
	}

	// 确保 context 已初始化（兼容未调用 Start 的场景）
	if m.ctx == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}

	// 启动数据源
	if err := wrapper.DataSource.Start(m.ctx, m.eventCh); err != nil {
		return fmt.Errorf("failed to start datasource %s: %w", name, err)
	}

	wrapper.Active = true
	m.sources[name] = wrapper
	m.activeName = name
	m.logger.Info("Datasource started", log.String("datasource", name))
	return nil
}

// getAvailableSources 获取可用数据源列表
func (m *DataSourceManager) getAvailableSources() []string {
	sources := make([]string, 0, len(m.sources))
	for id := range m.sources {
		sources = append(sources, id)
	}
	return sources
}

// Stop 停止指定数据源
func (m *DataSourceManager) Stop(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return fmt.Errorf("datasource %s not found", name)
	}

	if !wrapper.Active {
		return nil
	}

	if err := wrapper.DataSource.Stop(); err != nil {
		return fmt.Errorf("stop datasource: %w", err)
	}

	wrapper.Active = false
	m.sources[name] = wrapper
	m.activeName = ""
	return nil
}

// Switch 切换数据源（停止旧的，启动新的）
func (m *DataSourceManager) Switch(fromName, toName string) error {
	m.logger.Info("Switching datasource",
		log.String("from", fromName),
		log.String("to", toName))

	// 停止旧数据源
	if fromName != "" {
		if err := m.Stop(fromName); err != nil {
			return fmt.Errorf("failed to stop old datasource %s: %w", fromName, err)
		}
		m.logger.Info("Old datasource stopped", log.String("datasource", fromName))
	}

	// 启动新数据源
	if err := m.StartByName(toName); err != nil {
		return fmt.Errorf("failed to start new datasource %s: %w", toName, err)
	}

	m.logger.Info("New datasource started successfully", log.String("datasource", toName))
	return nil
}

// SwitchTo 切换到指定数据源（支持惰性创建）
// 如果目标数据源尚未注册，则从配置中惰性创建实例
// 返回切换前的数据源名称
func (m *DataSourceManager) SwitchTo(name string) (string, error) {
	// 验证目标类型是否在工厂中注册
	supportedTypes := Factory.GetSupportedTypes()
	isSupported := false
	for _, t := range supportedTypes {
		if t == name {
			isSupported = true
			break
		}
	}
	if !isSupported {
		return "", fmt.Errorf("unknown datasource type: %s, supported: %v", name, supportedTypes)
	}

	// 惰性创建：如果不在 m.sources 中，从配置创建
	if err := m.ensureRegistered(name); err != nil {
		return "", fmt.Errorf("ensure datasource %s: %w", name, err)
	}

	// 记录切换前的活跃数据源
	m.mu.RLock()
	previous := m.activeName
	// 如果 activeName 为空（Start() 可能在初始化时未设置），
	// 遍历 m.sources 查找实际活跃的数据源
	if previous == "" {
		for name, wrapper := range m.sources {
			if wrapper.Active {
				previous = name
				break
			}
		}
	}
	m.mu.RUnlock()

	if previous == name {
		return previous, fmt.Errorf("datasource %s already active", name)
	}

	// 停止旧数据源
	if previous != "" {
		if err := m.Stop(previous); err != nil {
			return previous, fmt.Errorf("stop old datasource %s: %w", previous, err)
		}
	}

	// 启动新数据源
	if err := m.StartByName(name); err != nil {
		return previous, fmt.Errorf("start new datasource %s: %w", name, err)
	}

	m.logger.Info("Datasource switched",
		log.String("from", previous),
		log.String("to", name))
	return previous, nil
}

// ensureRegistered 确保数据源已注册，如果未注册则从配置惰性创建
func (m *DataSourceManager) ensureRegistered(name string) error {
	m.mu.RLock()
	_, exists := m.sources[name]
	m.mu.RUnlock()
	if exists {
		return nil // 已注册
	}

	if m.cfg == nil {
		return fmt.Errorf("config manager not set, cannot lazily create datasource %s", name)
	}

	// 读取 [datasource.{name}] 配置
	typeConfig := m.cfg.GetMap(fmt.Sprintf("datasource.%s", name))
	if typeConfig == nil {
		return fmt.Errorf("no config section [datasource.%s] found", name)
	}

	// 注入 type 和 name
	typeConfig["type"] = name
	typeConfig["name"] = name

	// 工厂创建实例
	ds, err := Factory.CreateWithMap(typeConfig)
	if err != nil {
		return fmt.Errorf("create datasource %s: %w", name, err)
	}

	// 设置 logger
	if ls, ok := ds.(interface{ SetLogger(log.Logger) }); ok {
		ls.SetLogger(m.logger)
	}

	// 注册到管理器
	if err := m.Register(name, ds, nil); err != nil {
		return fmt.Errorf("register datasource %s: %w", name, err)
	}

	m.logger.Info("Datasource lazily created and registered", log.String("name", name))
	return nil
}

// StopAll 停止所有数据源
func (m *DataSourceManager) StopAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var firstErr error
	for name, wrapper := range m.sources {
		if wrapper.Active {
			if err := wrapper.DataSource.Stop(); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("stop %s: %w", name, err)
				}
			}
			wrapper.Active = false
			m.sources[name] = wrapper
		}
	}
	m.activeName = ""

	return firstErr
}

// StartActive 启动指定的活跃数据源
// 按照设计约定，只启动配置中指定的单个数据源
func (m *DataSourceManager) StartActive(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Starting active datasource", log.String("name", name))

	wrapper, exists := m.sources[name]
	if !exists {
		return fmt.Errorf("datasource %s not registered", name)
	}
	m.logger.Debug("Datasource found", log.String("name", name), log.Any("active", wrapper.Active))

	if wrapper.Active {
		m.logger.Info("Datasource already active", log.String("name", name))
		return nil // 已在运行
	}

	m.logger.Debug("Calling Start for datasource", log.String("name", name))
	if err := wrapper.DataSource.Start(m.ctx, m.eventCh); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}

	wrapper.Active = true
	m.sources[name] = wrapper
	m.activeName = name
	m.logger.Info("Datasource marked as active", log.String("name", name))
	return nil
}

// Get 获取数据源
func (m *DataSourceManager) Get(name string) (DataSource, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return nil, fmt.Errorf("datasource %s not found", name)
	}

	return wrapper.DataSource, nil
}

// GetWrapper 获取数据源包装器（包含 Active 状态）
func (m *DataSourceManager) GetWrapper(name string) (*DataSourceWrapper, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return nil, fmt.Errorf("datasource %s not found", name)
	}

	return &wrapper, nil
}

// List 列出所有已实例化的数据源（仅返回名称列表）
func (m *DataSourceManager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.sources))
	for name := range m.sources {
		names = append(names, name)
	}
	return names
}

// ListAllWithStatus 列出工厂中所有已注册的数据源类型及其激活状态
// 工厂注册的类型全部列出，同时标记哪些已实例化且处于活跃状态
func (m *DataSourceManager) ListAllWithStatus() []DatasourceInfo {
	allTypes := Factory.GetSupportedTypes()

	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]DatasourceInfo, 0, len(allTypes))
	for _, t := range allTypes {
		info := DatasourceInfo{Name: t, Active: false}
		if wrapper, exists := m.sources[t]; exists && wrapper.Active {
			info.Active = true
		}
		result = append(result, info)
	}
	return result
}

// GetActive 获取激活的数据源列表
func (m *DataSourceManager) GetActive() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0)
	for name, wrapper := range m.sources {
		if wrapper.Active {
			names = append(names, name)
		}
	}
	return names
}

// Health 检查指定数据源健康状态
func (m *DataSourceManager) Health(name string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return fmt.Errorf("datasource %s not found", name)
	}

	return wrapper.DataSource.Health()
}

// HealthAll 检查所有数据源健康状态
func (m *DataSourceManager) HealthAll() map[string]error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make(map[string]error)
	for name, wrapper := range m.sources {
		if wrapper.Active {
			results[name] = wrapper.DataSource.Health()
		}
	}
	return results
}

// GetStats 获取指定数据源统计信息
func (m *DataSourceManager) GetStats(name string) (*DataSourceStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	wrapper, exists := m.sources[name]
	if !exists {
		return nil, fmt.Errorf("datasource %s not found", name)
	}

	return wrapper.DataSource.GetStats(), nil
}

// GetAllStats 获取所有数据源统计信息
func (m *DataSourceManager) GetAllStats() map[string]*DataSourceStats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := make(map[string]*DataSourceStats)
	for name, wrapper := range m.sources {
		if wrapper.Active {
			stats[name] = wrapper.DataSource.GetStats()
		}
	}
	return stats
}

// Close 关闭管理器
// 按照设计约定，仅同一时刻生效一个数据源，直接停止 activeName
func (m *DataSourceManager) Close() error {
	if m.cancel != nil {
		m.cancel()
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activeName == "" {
		return nil
	}
	wrapper, ok := m.sources[m.activeName]
	if !ok {
		m.activeName = ""
		return nil
	}
	if err := wrapper.DataSource.Stop(); err != nil {
		return fmt.Errorf("stop datasource %s: %w", m.activeName, err)
	}
	wrapper.Active = false
	m.sources[m.activeName] = wrapper
	m.logger.Info("Datasource stopped", log.String("name", m.activeName))
	m.activeName = ""
	return nil
}

// RegisterFactory 注册数据源工厂
func (m *DataSourceManager) RegisterFactory(dsType string, factory DataSourceFactoryFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.factories[dsType] = factory
}

// CreateDataSource 使用工厂创建数据源
func (m *DataSourceManager) CreateDataSource(dsType, name string, config map[string]interface{}) (DataSource, error) {
	m.mu.RLock()
	factory, ok := m.factories[dsType]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no factory registered for datasource type: %s", dsType)
	}

	return factory(name, config)
}

// Init 从配置初始化数据源
// 统一接口：读取配置 -> 工厂创建 -> 注册
func (m *DataSourceManager) Init(cfg config.ConfigManager) error {
	// 保存配置管理器，用于后续惰性创建数据源
	m.cfg = cfg

	// 读取 [datasource] 配置
	dsConfig := cfg.Get("datasource")
	dsMap, ok := dsConfig.(map[string]interface{})
	if !ok {
		m.logger.Info("No datasource configuration found")
		return nil // 没有配置数据源
	}

	dsType, _ := dsMap["type"].(string)
	if dsType == "" {
		m.logger.Info("No datasource type specified")
		return nil // 没有指定类型
	}
	m.logger.Info("Using datasource type", log.String("type", dsType))

	// 读取 [datasource.{type}] 具体配置
	typeConfig := cfg.GetMap(fmt.Sprintf("datasource.%s", dsType))
	if typeConfig == nil {
		typeConfig = make(map[string]interface{})
	}

	// 注入 type 和 name（使用 type 作为 name）
	typeConfig["type"] = dsType
	typeConfig["name"] = dsType

	// 工厂创建实例
	ds, err := Factory.CreateWithMap(typeConfig)
	if err != nil {
		return fmt.Errorf("create datasource %s: %w", dsType, err)
	}
	m.logger.Info("Datasource instance created", log.String("type", dsType))

	// 设置 logger 到 datasource
	if ls, ok := ds.(interface{ SetLogger(log.Logger) }); ok {
		ls.SetLogger(m.logger)
	}

	// 注册到管理器
	// 获取解析后的配置（从工厂创建时已经解析）
	// 这里传递nil，因为工厂已经通过CreateWithMap解析了配置
	if err := m.Register(dsType, ds, nil); err != nil {
		return fmt.Errorf("register datasource %s: %w", dsType, err)
	}
	m.logger.Info("Datasource registered", log.String("type", dsType))

	return nil
}

// LoadAndStart 从配置加载并启动数据源
// 统一接口：读取配置 -> 工厂创建 -> 注册 -> 启动
// @deprecated 使用 Init + Start 代替
func (m *DataSourceManager) LoadAndStart(cfg config.ConfigManager) error {
	if err := m.Init(cfg); err != nil {
		return err
	}

	// 获取第一个数据源名称
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name := range m.sources {
		return m.StartByName(name)
	}
	return nil
}
