package datasource

import (
	"fmt"
)

// CreateNRIDataSource 创建NRI数据源实例
func CreateNRIDataSource(config DataSourceConfig) (DataSource, error) {
	nriConfig, ok := config.(*NRIConfig)
	if !ok {
		return nil, fmt.Errorf("invalid config type, expected *NRIConfig")
	}

	return NewNRIDataSource(nriConfig)
}
