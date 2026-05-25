package config

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/pelletier/go-toml/v2"
)

// TOMLConfigManager TOML配置管理器实现
type TOMLConfigManager struct {
	data map[string]interface{}
	mu   sync.RWMutex
}

// NewTOMLConfigManager 创建TOML配置管理器
func NewTOMLConfigManager() *TOMLConfigManager {
	return &TOMLConfigManager{
		data: make(map[string]interface{}),
	}
}

// Load 从文件加载TOML配置
func (c *TOMLConfigManager) Load(path string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config file: %w", err)
	}

	var config map[string]interface{}
	if err := toml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parse toml: %w", err)
	}

	c.data = config
	return nil
}

// Get 获取配置值，支持嵌套key（如"server.port"）
func (c *TOMLConfigManager) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 直接查找
	if val, ok := c.data[key]; ok {
		return val
	}

	// 尝试嵌套查找（如"server.port"）
	parts := strings.Split(key, ".")
	if len(parts) < 2 {
		return nil
	}

	current := c.data
	for _, part := range parts {
		val, ok := current[part]
		if !ok {
			return nil
		}
		// 如果是最后一层，返回值
		if part == parts[len(parts)-1] {
			return val
		}
		// 否则继续深入
		next, ok := val.(map[string]interface{})
		if !ok {
			return nil
		}
		current = next
	}

	return nil
}

// GetString 获取字符串配置
func (c *TOMLConfigManager) GetString(key string) string {
	val := c.Get(key)
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

// GetInt 获取整数配置
func (c *TOMLConfigManager) GetInt(key string) int {
	val := c.Get(key)
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// GetBool 获取布尔配置
func (c *TOMLConfigManager) GetBool(key string) bool {
	val := c.Get(key)
	if b, ok := val.(bool); ok {
		return b
	}
	return false
}

// GetMap 获取Map配置
func (c *TOMLConfigManager) GetMap(key string) map[string]interface{} {
	val := c.Get(key)
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}
	return nil
}

// Set 设置配置值
func (c *TOMLConfigManager) Set(key string, value interface{}) {
	c.mu.Lock()
	c.data[key] = value
	c.mu.Unlock()
}

// Save 保存配置到文件
func (c *TOMLConfigManager) Save(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := toml.Marshal(c.data)
	if err != nil {
		return fmt.Errorf("marshal toml: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}

	return nil
}

// Close 关闭配置管理器
func (c *TOMLConfigManager) Close() error {
	return nil
}
