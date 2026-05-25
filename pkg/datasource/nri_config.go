package datasource

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// ParseNRIConfig 解析NRI配置
func ParseNRIConfig(config interface{}) (DataSourceConfig, error) {
	// 默认配置
	cfg := &NRIConfig{
		SocketPath:  "/var/run/nri.sock",
		PluginName:  "01-nuts",
		PluginIndex: "01",
		Events: []string{
			"RunPodSandbox",
			"StopPodSandbox",
			"StartContainer",
			"StopContainer",
			"RemoveContainer",
		},
		BufferSize: 1000,
	}

	// 解析传入的配置（TOML字符串或map）
	if configStr, ok := config.(string); ok {
		// 解析TOML字符串
		var parsedConfig map[string]interface{}
		if err := toml.Unmarshal([]byte(configStr), &parsedConfig); err != nil {
			return nil, fmt.Errorf("failed to parse TOML config: %w", err)
		}
		config = parsedConfig
	}

	// 解析map配置
	if configMap, ok := config.(map[string]interface{}); ok {
		if socketPath, ok := configMap["socket_path"].(string); ok {
			cfg.SocketPath = socketPath
		}
		if pluginName, ok := configMap["plugin_name"].(string); ok {
			cfg.PluginName = pluginName
		}
		if pluginIndex, ok := configMap["plugin_index"].(string); ok {
			cfg.PluginIndex = pluginIndex
		}
		if bufferSize, ok := configMap["buffer_size"].(int); ok {
			cfg.BufferSize = bufferSize
		}
		if events, ok := configMap["events"].([]interface{}); ok {
			cfg.Events = make([]string, len(events))
			for i, event := range events {
				if eventStr, ok := event.(string); ok {
					cfg.Events[i] = eventStr
				}
			}
		}
	}

	return cfg, nil
}

// GetType 获取数据源类型
func (c *NRIConfig) GetType() string {
	return "nri"
}

// GetName 获取数据源名称
func (c *NRIConfig) GetName() string {
	return "nri"
}

// Validate 验证NRI配置
func (c *NRIConfig) Validate() error {
	if c.SocketPath == "" {
		return fmt.Errorf("socket_path is required")
	}
	if c.PluginName == "" {
		return fmt.Errorf("plugin_name is required")
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("events list cannot be empty")
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 1000 // 设置默认值
	}
	return nil
}
