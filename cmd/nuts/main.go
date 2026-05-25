package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sig-cloudnative/nuts/pkg/core"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

func main() {
	// 解析命令行参数
	var configFile string
	flag.StringVar(&configFile, "config", "configs/nuts.toml", "配置文件路径")
	flag.Parse()

	fmt.Println("NUTS Framework Starting...")

	// 使用配置创建Core
	cfg := core.DefaultConfig()
	cfg.ConfigFile = configFile

	// 创建并初始化Core（包含所有模块：日志、配置、EventBus、数据源、任务调度、策略引擎、回滚管理、工作流引擎）
	core, err := core.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create core: %v\n", err)
		os.Exit(1)
	}

	// 启动Core服务
	if err := core.Start(); err != nil {
		core.Logger.Error("Failed to start core", log.Error(err))
		os.Exit(1)
	}

	core.Logger.Info("NUTS Framework started successfully!")

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan

	core.Logger.Info("Shutting down NUTS Framework...")

	// 优雅关闭
	if err := core.Stop(); err != nil {
		core.Logger.Error("Error during shutdown", log.Error(err))
	}

	core.Logger.Info("NUTS Framework stopped")
}
