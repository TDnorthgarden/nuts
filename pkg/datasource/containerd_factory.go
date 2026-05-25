package datasource

import (
	"fmt"
)

// CreateContainerdDataSource 创建 containerd 数据源实例
func CreateContainerdDataSource(config DataSourceConfig) (DataSource, error) {
	containerdConfig, ok := config.(*ContainerdConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type, expected *ContainerdConfig")
	}

	return NewContainerdDataSource(containerdConfig)
}
