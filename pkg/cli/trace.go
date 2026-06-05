package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// traceCmd 追踪管理命令
var traceCmd = &cobra.Command{
	Use:   "trace",
	Short: "事件追踪管理",
	Long:  `查询事件链路追踪（TraceID）和事件日志（EventLog）`,
}

// traceGetCmd 按 TraceID 查询追踪时间线
var traceGetCmd = &cobra.Command{
	Use:   "get [trace-id]",
	Short: "查询追踪时间线",
	Long:  `按 TraceID 查询完整的事件链路时间线，包含所有流转阶段`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getTrace(args[0])
	},
}

// traceEventsCmd 按 TaskID 查询关联事件
var traceEventsCmd = &cobra.Command{
	Use:   "events [task-id]",
	Short: "查询任务关联事件",
	Long:  `按 TaskID 查询关联的 EventLog 事件条目`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getTaskEvents(args[0])
	},
}

func init() {
	rootCmd.AddCommand(traceCmd)
	traceCmd.AddCommand(traceGetCmd)
	traceCmd.AddCommand(traceEventsCmd)
}

// getTrace 查询追踪时间线
func getTrace(traceID string) error {
	resp, err := httpClient.Get("/api/v1/traces/" + traceID)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// getTaskEvents 查询任务关联事件
func getTaskEvents(taskID string) error {
	resp, err := httpClient.Get("/api/v1/tasks/" + taskID + "/events")
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}
