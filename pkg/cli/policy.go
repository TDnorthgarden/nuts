package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// policyCmd 策略管理命令
var policyCmd = &cobra.Command{
	Use:   "policy",
	Short: "策略管理",
	Long:  `管理策略，包括查看、添加、启用、禁用等操作`,
}

// policyListCmd 列出策略
var policyListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有策略",
	Long:  `列出所有已配置的策略`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return listPolicies()
	},
}

// policyGetCmd 获取策略详情
var policyGetCmd = &cobra.Command{
	Use:   "get [policy-id]",
	Short: "获取策略详情",
	Long:  `获取指定策略的详细信息`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getPolicy(args[0])
	},
}

// policyAddCmd 添加策略
var policyAddCmd = &cobra.Command{
	Use:     "add [policy-file]",
	Short:   "添加策略",
	Long:    `从JSON文件添加新策略`,
	Args:    cobra.ExactArgs(1),
	Example: `  nuts policy add ./my-policy.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return addPolicy(args[0])
	},
}

// policyEnableCmd 启用策略
var policyEnableCmd = &cobra.Command{
	Use:   "enable [policy-id]",
	Short: "启用策略",
	Long:  `启用指定的策略`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return enablePolicy(args[0])
	},
}

// policyDisableCmd 禁用策略
var policyDisableCmd = &cobra.Command{
	Use:   "disable [policy-id]",
	Short: "禁用策略",
	Long:  `禁用指定的策略`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return disablePolicy(args[0])
	},
}

// policyRemoveCmd 移除策略
var policyRemoveCmd = &cobra.Command{
	Use:   "remove [policy-id]",
	Short: "移除策略",
	Long:  `移除指定的策略`,
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return removePolicy(args[0])
	},
}

// policyEvaluateCmd 评估策略
var policyEvaluateCmd = &cobra.Command{
	Use:   "evaluate [policy-file]",
	Short: "评估策略（校验规则语法）",
	Long: `校验策略 JSON 文件的 DSL 语法是否正确。
该命令只进行语法验证，不会将策略添加到服务端。`,
	Args:    cobra.ExactArgs(1),
	Example: `  nuts policy evaluate ./my-policy.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return evaluatePolicy(args[0])
	},
}

func init() {
	// 添加子命令
	rootCmd.AddCommand(policyCmd)
	policyCmd.AddCommand(policyListCmd)
	policyCmd.AddCommand(policyGetCmd)
	policyCmd.AddCommand(policyAddCmd)
	policyCmd.AddCommand(policyEnableCmd)
	policyCmd.AddCommand(policyDisableCmd)
	policyCmd.AddCommand(policyRemoveCmd)
	policyCmd.AddCommand(policyEvaluateCmd)
}

// listPolicies 列出策略（HTTP API调用）
func listPolicies() error {
	resp, err := httpClient.Get("/api/v1/policies")
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// getPolicy 获取策略详情（HTTP API调用）
func getPolicy(id string) error {
	resp, err := httpClient.Get("/api/v1/policies/" + id)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// addPolicy 添加策略（HTTP API调用）
// 支持单条策略（JSON 对象）或批量策略（JSON 数组）
func addPolicy(file string) error {
	// 从文件读取配置
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 尝试解析为数组
	var policies []map[string]interface{}
	if err := json.Unmarshal(data, &policies); err == nil {
		// 批量添加
		for i, policy := range policies {
			resp, err := httpClient.Post("/api/v1/policies", policy)
			if err != nil {
				return fmt.Errorf("添加第 %d 条策略失败: %w", i+1, err)
			}
			if err := PrintResponse(resp); err != nil {
				return err
			}
		}
		return nil
	}

	// 尝试解析为单条策略
	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("解析JSON失败（应为对象或数组）: %w", err)
	}

	resp, err := httpClient.Post("/api/v1/policies", config)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// enablePolicy 启用策略（HTTP API调用）
func enablePolicy(id string) error {
	resp, err := httpClient.Post("/api/v1/policies/"+id+"/enable", nil)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// disablePolicy 禁用策略（HTTP API调用）
func disablePolicy(id string) error {
	resp, err := httpClient.Post("/api/v1/policies/"+id+"/disable", nil)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// removePolicy 移除策略（HTTP API调用）
func removePolicy(id string) error {
	resp, err := httpClient.Delete("/api/v1/policies/" + id)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}

// evaluatePolicy 评估策略（校验规则语法，不保存）
func evaluatePolicy(file string) error {
	// 从文件读取策略配置
	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	// 解析 JSON
	var policy map[string]interface{}
	if err := json.Unmarshal(data, &policy); err != nil {
		return fmt.Errorf("解析 JSON 失败: %w", err)
	}

	// 调用服务端校验 DSL 语法（不保存）
	resp, err := httpClient.Post("/api/v1/policies/validate", policy)
	if err != nil {
		return fmt.Errorf("连接服务端失败: %w", err)
	}
	return PrintResponse(resp)
}
