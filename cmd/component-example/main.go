// component-example 组件使用示例
// 展示如何使用组件框架创建自定义组件
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/component"
	"github.com/sig-cloudnative/nuts/pkg/component/examples"
	"github.com/sig-cloudnative/nuts/pkg/eventbus"
)

func main() {
	fmt.Println("[ComponentExample] Starting component example...")

	var address string
	flag.StringVar(&address, "address", "tcp://localhost:50051", "配置文件路径")
	flag.Parse()
	// 创建 gRPC EventBus 客户端（连接到 nuts 服务）
	bus, err := eventbus.NewGRPCEventBusClient(address, nil)
	if err != nil {
		fmt.Printf("[ComponentExample] Failed to create eventbus client: %v\n", err)
		os.Exit(1)
	}

	if err := bus.Start(context.Background()); err != nil {
		fmt.Printf("[ComponentExample] Failed to start eventbus: %v\n", err)
		os.Exit(1)
	}

	// 创建 pending 组件（处理初始状态到 validating 的转换）
	pendingComp := examples.NewPendingComponent(bus)

	// 创建验证组件
	validatingComp := examples.NewValidatingComponent(
		bus,
		examples.DefaultValidationRules(),
	)

	// 创建处理组件
	processingComp := examples.NewProcessingComponent(bus, nil)

	// 创建故障转移组件
	failoverComp := examples.NewFailoverComponent(
		bus,
		examples.DefaultRetryPolicy(),
	)

	// 初始化组件
	config := component.ComponentConfig{
		MaxConcurrent: 10,
		Timeout:       30 * time.Second,
		RetryAttempts: 3,
	}

	if err := pendingComp.Init(config); err != nil {
		fmt.Printf("[ComponentExample] Failed to init pending component: %v\n", err)
		os.Exit(1)
	}

	if err := validatingComp.Init(config); err != nil {
		fmt.Printf("[ComponentExample] Failed to init validating component: %v\n", err)
		os.Exit(1)
	}

	if err := processingComp.Init(config); err != nil {
		fmt.Printf("[ComponentExample] Failed to init processing component: %v\n", err)
		os.Exit(1)
	}

	if err := failoverComp.Init(config); err != nil {
		fmt.Printf("[ComponentExample] Failed to init failover component: %v\n", err)
		os.Exit(1)
	}

	// 启动组件
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := pendingComp.Start(ctx); err != nil {
		fmt.Printf("[ComponentExample] Failed to start pending component: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[ComponentExample] Pending component started")

	if err := validatingComp.Start(ctx); err != nil {
		fmt.Printf("[ComponentExample] Failed to start validating component: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[ComponentExample] Validating component started")

	if err := processingComp.Start(ctx); err != nil {
		fmt.Printf("[ComponentExample] Failed to start processing component: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[ComponentExample] Processing component started")

	if err := failoverComp.Start(ctx); err != nil {
		fmt.Printf("[ComponentExample] Failed to start failover component: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("[ComponentExample] Failover component started")

	// 等待中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	fmt.Println("[ComponentExample] Components running. Press Ctrl+C to stop.")

	select {
	case sig := <-sigChan:
		fmt.Printf("[ComponentExample] Received signal: %v\n", sig)
	case <-time.After(300 * time.Second):
		fmt.Println("[ComponentExample] Timeout reached, stopping...")
	}

	// 优雅关闭
	fmt.Println("[ComponentExample] Stopping components...")

	if err := pendingComp.Stop(); err != nil {
		fmt.Printf("[ComponentExample] Error stopping pending component: %v\n", err)
	}

	if err := validatingComp.Stop(); err != nil {
		fmt.Printf("[ComponentExample] Error stopping validating component: %v\n", err)
	}

	if err := processingComp.Stop(); err != nil {
		fmt.Printf("[ComponentExample] Error stopping processing component: %v\n", err)
	}

	if err := failoverComp.Stop(); err != nil {
		fmt.Printf("[ComponentExample] Error stopping failover component: %v\n", err)
	}

	fmt.Println("[ComponentExample] All components stopped")
}
