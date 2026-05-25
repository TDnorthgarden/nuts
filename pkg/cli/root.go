package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	// 版本信息
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"

	// 服务端地址
	serverURL string
	// 认证 token
	authToken string
)

// httpClient 全局HTTP客户端
var httpClient *HTTPClient

// rootCmd 根命令
var rootCmd = &cobra.Command{
	Use:   "nuts-cli",
	Short: "Nuts CLI - 云原生事件驱动任务编排平台客户端",
	Long: `Nuts CLI 是 Nuts 服务端的命令行客户端，通过 HTTP API 与服务端通信。

Examples:
  # 查看服务端状态
  nuts-cli status

  # 指定服务端地址
  nuts-cli --server http://192.168.1.100:8080 status

  # 管理数据源
  nuts-cli datasource list

  # 管理任务
  nuts-cli task list
  nuts-cli task get <task-id>

  # 管理策略
  nuts-cli policy list`,
	Version: fmt.Sprintf("%s (build: %s, commit: %s)", Version, BuildTime, GitCommit),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// 初始化HTTP客户端
		httpClient = NewHTTPClient(serverURL)
		token := authToken
		if token == "" {
			token = os.Getenv("NUTS_AUTH_TOKEN")
		}
		httpClient.SetToken(token)
	},
}

// Execute 执行根命令
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// 全局标志
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "tcp://localhost:8080", "服务端地址 (tcp://host:port 或 unix:///path/to/socket)")
	rootCmd.PersistentFlags().StringVar(&authToken, "token", "", "API 认证 token (从服务端日志获取)")
}
