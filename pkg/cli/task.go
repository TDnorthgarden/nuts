package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// taskCmd 任务管理命令
var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "任务管理",
	Long:  `管理任务，包括查看任务列表、查看任务状态等操作`,
}

// taskListCmd 列出任务
var taskListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有任务",
	Long:  `列出所有当前的任务及其状态`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listTasks()
	},
}

// taskGetCmd 获取任务详情
var taskGetCmd = &cobra.Command{
	Use:   "get [task-id]",
	Short: "获取任务详情",
	Long:  `获取指定任务的详细信息和状态`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getTask(args[0])
	},
}

// taskHistoryCmd 查看任务历史（包括已完成任务）
var taskHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "查看任务历史",
	Long:  `查看所有任务历史记录，包括已完成、失败、取消的任务`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listTaskHistory()
	},
}

func init() {
	// 添加子命令
	rootCmd.AddCommand(taskCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskHistoryCmd)
}

// listTasks 列出任务（HTTP API调用）
func listTasks() error {
	resp, err := httpClient.Get("/api/v1/tasks")
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// getTask 获取任务详情（HTTP API调用）
func getTask(id string) error {
	resp, err := httpClient.Get("/api/v1/tasks/" + id)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// listTaskHistory 列出所有任务历史（包括已完成）
func listTaskHistory() error {
	resp, err := httpClient.Get("/api/v1/tasks?include_completed=true")
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}
