package datasource

import (
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// ContainerdConfig containerd 数据源配置
type ContainerdConfig struct {
	// SocketPath containerd gRPC socket 路径，默认 /run/containerd/containerd.sock
	SocketPath string `toml:"socket_path"`

	// Namespace containerd namespace，默认 "k8s.io"
	Namespace string `toml:"namespace"`

	// Events 订阅的事件类型列表
	// 可选值: TaskCreate, TaskStart, TaskExit, TaskDelete, TaskPause, TaskResume,
	//         ContainerCreate, ContainerDelete
	Events []string `toml:"events"`

	// BufferSize 事件缓冲区大小
	BufferSize int `toml:"buffer_size"`
}

// ParseContainerdConfig 解析 containerd 配置
func ParseContainerdConfig(config interface{}) (DataSourceConfig, error) {
	// 默认配置
	cfg := &ContainerdConfig{
		SocketPath: "/run/containerd/containerd.sock",
		Namespace:  "k8s.io",
		Events: []string{
			"TaskCreate",
			"TaskStart",
			"TaskExit",
			"TaskDelete",
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
		if namespace, ok := configMap["namespace"].(string); ok {
			cfg.Namespace = namespace
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
func (c *ContainerdConfig) GetType() string {
	return "containerd"
}

// GetName 获取数据源名称
func (c *ContainerdConfig) GetName() string {
	return "containerd"
}

// Validate 验证 containerd 配置
func (c *ContainerdConfig) Validate() error {
	if c.SocketPath == "" {
		return fmt.Errorf("socket_path is required")
	}
	if c.Namespace == "" {
		c.Namespace = "k8s.io" // 设置默认值
	}
	if len(c.Events) == 0 {
		return fmt.Errorf("events list cannot be empty")
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 1000 // 设置默认值
	}
	return nil
}
