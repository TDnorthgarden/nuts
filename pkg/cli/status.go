package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

// statusCmd 状态检查命令
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "检查服务端状态",
	Long:  "通过HTTP API检查Nuts服务端运行状态",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := httpClient.Get("/api/v1/status")
		if err != nil {
			return fmt.Errorf("连接服务端失败: %w", err)
		}

		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}
		resp.Body.Close()

		// 格式化输出
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
