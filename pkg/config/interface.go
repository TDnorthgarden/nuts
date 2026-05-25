package config

// ConfigManager 配置管理接口
type ConfigManager interface {
	// Load 从指定路径加载配置
	Load(path string) error

	// Get 获取配置值
	Get(key string) interface{}

	// GetString 获取字符串配置
	GetString(key string) string

	// GetInt 获取整数配置
	GetInt(key string) int

	// GetBool 获取布尔配置
	GetBool(key string) bool

	// GetMap 获取Map配置
	GetMap(key string) map[string]interface{}

	// Set 设置配置值
	Set(key string, value interface{})

	// Save 保存配置到文件
	Save(path string) error

	// Close 关闭配置管理器
	Close() error
}
