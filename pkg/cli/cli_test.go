package cli

import (
	"testing"
)

// TestRootCommand 测试根命令
func TestRootCommand(t *testing.T) {
	// 测试版本信息
	if rootCmd.Version == "" {
		t.Error("Expected version to be set")
	}

	// 测试命令名称
	if rootCmd.Use != "nuts-cli" {
		t.Errorf("Expected command name 'nuts-cli', got '%s'", rootCmd.Use)
	}
}

// TestCommands 测试所有子命令已注册
func TestCommands(t *testing.T) {
	commands := []string{
		"status",
		"datasource",
		"policy",
		"task",
	}

	for _, cmdName := range commands {
		found := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == cmdName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected command '%s' not found", cmdName)
		}
	}
}

// TestDatasourceCommands 测试数据源子命令
func TestDatasourceCommands(t *testing.T) {
	subcommands := []string{"list", "status", "disable", "switch"}

	for _, cmdName := range subcommands {
		found := false
		for _, cmd := range datasourceCmd.Commands() {
			if cmd.Name() == cmdName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected datasource subcommand '%s' not found", cmdName)
		}
	}
}

// TestPolicyCommands 测试策略子命令
func TestPolicyCommands(t *testing.T) {
	subcommands := []string{"list", "get", "add", "enable", "disable", "remove", "evaluate"}

	for _, cmdName := range subcommands {
		found := false
		for _, cmd := range policyCmd.Commands() {
			if cmd.Name() == cmdName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected policy subcommand '%s' not found", cmdName)
		}
	}
}

// TestTaskCommands 测试任务子命令
func TestTaskCommands(t *testing.T) {
	subcommands := []string{"list", "get", "history"}

	for _, cmdName := range subcommands {
		found := false
		for _, cmd := range taskCmd.Commands() {
			if cmd.Name() == cmdName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected task subcommand '%s' not found", cmdName)
		}
	}
}
