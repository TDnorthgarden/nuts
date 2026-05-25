package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// datasourceCmd 数据源管理命令
var datasourceCmd = &cobra.Command{
	Use:   "datasource",
	Short: "数据源管理",
	Long:  `管理数据源，包括查看、启用、禁用等操作`,
}

// datasourceListCmd 列出数据源
var datasourceListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有数据源",
	Long:  `列出所有已注册的数据源及其状态`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listDatasources()
	},
}

// datasourceStatusCmd 查看数据源状态
var datasourceStatusCmd = &cobra.Command{
	Use:   "status [datasource-id]",
	Short: "查看数据源状态",
	Long:  `查看指定数据源的详细状态和统计信息`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getDatasourceStatus(args[0])
	},
}

// datasourceDisableCmd 禁用数据源
var datasourceDisableCmd = &cobra.Command{
	Use:   "disable [datasource-id]",
	Short: "禁用数据源",
	Long:  `禁用指定的数据源`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return disableDatasource(args[0])
	},
}

// datasourceSwitchCmd 切换数据源
var datasourceSwitchCmd = &cobra.Command{
	Use:   "switch [datasource-id]",
	Short: "切换数据源",
	Long:  `切换到指定的数据源（热切换）`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return switchDatasource(args[0])
	},
}

func init() {
	// 添加子命令
	rootCmd.AddCommand(datasourceCmd)
	datasourceCmd.AddCommand(datasourceListCmd)
	datasourceCmd.AddCommand(datasourceStatusCmd)
	datasourceCmd.AddCommand(datasourceDisableCmd)
	datasourceCmd.AddCommand(datasourceSwitchCmd)
}

// listDatasources 列出数据源（HTTP API调用）
func listDatasources() error {
	resp, err := httpClient.Get("/api/v1/datasources")
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// getDatasourceStatus 获取数据源状态（HTTP API调用）
func getDatasourceStatus(id string) error {
	resp, err := httpClient.Get("/api/v1/datasources/" + id)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// disableDatasource 禁用数据源（HTTP API调用）
func disableDatasource(id string) error {
	resp, err := httpClient.Post("/api/v1/datasources/"+id+"/disable", nil)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// switchDatasource 切换数据源（HTTP API调用）
func switchDatasource(id string) error {
	resp, err := httpClient.Post("/api/v1/datasources/"+id+"/switch", nil)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	defer resp.Body.Close()

	var apiResp struct {
		Code    int                    `json:"code"`
		Message string                 `json:"message"`
		Data    map[string]interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: %s", apiResp.Message)
	}

	// 处理 already_active 情况，输出简洁消息
	if action, ok := apiResp.Data["action"].(string); ok && action == "already_active" {
		fmt.Printf("datasource %s already active\n", id)
		return nil
	}

	// 处理 switched 但 previous 为空的情况（数据源已经是当前活跃状态）
	if action, ok := apiResp.Data["action"].(string); ok && action == "switched" {
		if prev, ok := apiResp.Data["previous"].(string); ok && prev == "" {
			fmt.Printf("datasource %s already active\n", id)
			return nil
		}
	}

	// 正常输出
	data, _ := json.MarshalIndent(apiResp.Data, "", "  ")
	fmt.Println(string(data))
	return nil
}
