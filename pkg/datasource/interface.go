package datasource

import (
	"context"
	"fmt"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/common"
)

// DataSource 数据源接口
// 定义数据源必须实现的方法，用于捕获容器事件
type DataSource interface {
	// ParseConfig 解析配置
	// config: 配置字符串或map
	ParseConfig(config map[string]interface{}) error

	// Start 启动数据源
	// ctx: 用于控制生命周期
	// eventCh: 输出事件的channel
	Start(ctx context.Context, eventCh chan<- *common.Event) error

	// Stop 停止数据源
	Stop() error

	// Health 健康检查
	Health() error

	// GetStats 获取数据源统计信息
	GetStats() *DataSourceStats

	// Ready 返回就绪信号通道
	// 当数据源初始化完成后，通道会被关闭
	Ready() <-chan struct{}
}

// DataSourceStats 数据源统计信息
type DataSourceStats struct {
	// EventsReceived 接收事件总数
	EventsReceived int64

	// EventsSent 发送事件总数
	EventsSent int64

	// EventsDropped 丢弃事件总数
	EventsDropped int64

	// LastEventTime 最后事件时间
	LastEventTime time.Time

	// Connected 是否已连接
	Connected bool

	// Uptime 运行时长
	Uptime time.Duration
}

// DataSourceConfig 数据源配置接口
type DataSourceConfig interface {
	// Validate 验证配置
	Validate() error

	// GetType 获取数据源类型
	GetType() string

	// GetName 获取数据源名称
	GetName() string
}

// BaseDataSourceConfig 基础配置
type BaseDataSourceConfig struct {
	Type string `toml:"type"`
	Name string `toml:"name"`

	// Enabled 是否启用
	Enabled bool `toml:"enabled"`

	// BufferSize 事件缓冲区大小
	BufferSize int `toml:"buffer_size"`

	// DropPolicy 缓冲区满时的丢弃策略
	DropPolicy string `toml:"drop_policy"` // "oldest", "newest", "block"

	// HealthCheckInterval 健康检查间隔
	HealthCheckInterval time.Duration `toml:"health_check_interval"`

	// ReconnectInterval 重连间隔
	ReconnectInterval time.Duration `toml:"reconnect_interval"`
}

// Validate 验证基础配置
func (c *BaseDataSourceConfig) Validate() error {
	if c.Type == "" {
		return fmt.Errorf("type is required")
	}
	if c.Name == "" {
		return fmt.Errorf("name is required")
	}
	if c.BufferSize <= 0 {
		c.BufferSize = 1000 // 默认值
	}
	if c.HealthCheckInterval == 0 {
		c.HealthCheckInterval = 30 * time.Second
	}
	if c.ReconnectInterval == 0 {
		c.ReconnectInterval = 5 * time.Second
	}
	return nil
}

// GetType 获取类型
func (c *BaseDataSourceConfig) GetType() string {
	return c.Type
}

// GetName 获取名称
func (c *BaseDataSourceConfig) GetName() string {
	return c.Name
}
