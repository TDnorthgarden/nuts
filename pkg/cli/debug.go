package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// debugCmd 运行时调试信息命令
var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "查看运行时调试信息",
	Long:  "通过HTTP API获取Nuts服务端运行时状态（goroutine数、内存、任务队列深度等）",
	RunE: func(cmd *cobra.Command, args []string) error {
		resp, err := httpClient.Get("/api/v1/debug/vars")
		if err != nil {
			return fmt.Errorf("连接服务端失败: %w", err)
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("读取响应失败: %w", err)
		}

		if resp.StatusCode != 200 {
			return fmt.Errorf("服务端返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
		}

		var result map[string]interface{}
		if err := json.Unmarshal(body, &result); err != nil {
			return fmt.Errorf("解析响应失败: %w", err)
		}

		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(debugCmd)
}
