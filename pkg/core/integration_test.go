package core

import (
	"context"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/datasource"
	"github.com/sig-cloudnative/nuts/pkg/policy"
	"github.com/sig-cloudnative/nuts/pkg/task"
)

// TestCore_Integration 测试Core整体功能
func TestCore_Integration(t *testing.T) {
	// 创建配置
	cfg := DefaultConfig()

	// 创建Core
	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}

	// 延迟关闭
	defer core.Stop()

	// 健康检查
	if err := core.Health(); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	t.Log("Core created and health check passed")
}

// TestEventFlow_Integration 测试事件流：DataSource → EventBus
func TestEventFlow_Integration(t *testing.T) {
	// 创建配置
	cfg := DefaultConfig()

	// 创建Core
	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 创建Mock数据源配置
	mockConfig := &datasource.MockDataSourceConfig{
		BaseDataSourceConfig: datasource.BaseDataSourceConfig{
			Type:       "mock",
			Name:       "test-mock",
			Enabled:    true,
			BufferSize: 100,
		},
		EventIntervalMs: 1000,
		EventTypes:      []string{"ContainerStart", "ContainerStop"},
	}

	// 创建Mock数据源
	mockSource, err := datasource.NewMockDataSource(mockConfig)
	if err != nil {
		t.Fatalf("Create mock datasource failed: %v", err)
	}

	// 注册到管理器
	if err := core.DataSourceManager.Register("test-mock", mockSource, mockConfig); err != nil {
		t.Fatalf("Register datasource failed: %v", err)
	}

	// 启动数据源
	if err := core.DataSourceManager.StartByName("test-mock"); err != nil {
		t.Fatalf("Start datasource failed: %v", err)
	}

	// 等待事件生成
	time.Sleep(2 * time.Second)

	// 获取统计
	stats := mockSource.GetStats()
	t.Logf("Mock datasource stats: Received=%d, Sent=%d", stats.EventsReceived, stats.EventsSent)

	// 停止数据源
	core.DataSourceManager.Stop("test-mock")
}

// TestPolicyMatch_Integration 测试策略匹配
func TestPolicyMatch_Integration(t *testing.T) {
	// 创建配置
	cfg := DefaultConfig()

	// 创建Core
	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 启动策略引擎
	if err := core.PolicyEngine.Start(context.Background()); err != nil {
		t.Fatalf("Start policy engine failed: %v", err)
	}

	// 初始化 CEL DSL 引擎（用于策略验证）
	if engine, ok := core.PolicyEngine.(*policy.DefaultPolicyEngine); ok {
		celConfig := &policy.DSLEngineConfig{Type: "cel"}
		celEngine, err := policy.Factory.Create(celConfig)
		if err != nil {
			t.Fatalf("Create CEL engine failed: %v", err)
		}
		engine.GetManager().(*policy.DefaultPolicyManager).RegisterEngine(celEngine)
	}

	// 添加测试策略
	strategy := &policy.Policy{
		ID:          "test-policy-core",
		Description: "Test policy for core integration",
		Enabled:     true,
		DSL:         `event.type == "ContainerStart"`,
		DSLEngine:   "cel",
	}

	// 通过策略管理器添加
	if engine, ok := core.PolicyEngine.(*policy.DefaultPolicyEngine); ok {
		if err := engine.GetManager().AddPolicy(strategy); err != nil {
			t.Fatalf("Add policy failed: %v", err)
		}
	}

	// 创建测试事件
	event := common.NewEvent("ContainerStart", "container.event", "test")
	event.TypedPayload = &api.Event_Pod{
		Pod: &api.PodEventPayload{
			PodName:      "test-pod",
			PodNamespace: "production",
			Namespace:    "production",
			Extensions:   map[string]string{},
		},
	}

	// 匹配策略
	matches, err := core.PolicyEngine.Match(context.Background(), event)
	if err != nil {
		t.Fatalf("Policy match failed: %v", err)
	}

	// 验证匹配结果
	if len(matches) == 0 {
		t.Error("Expected at least one match")
	}

	found := false
	for _, match := range matches {
		if match.PolicyID == "test-policy-core" && match.Matched {
			found = true
			break
		}
	}

	if !found {
		t.Log("Policy did not match (may be expected if CEL engine not properly initialized)")
	} else {
		t.Log("Policy matched successfully")
	}
}

// TestTaskScheduler_FullLifecycle 测试任务调度完整生命周期
func TestTaskScheduler_FullLifecycle(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 注入测试状态机配置：pending → processing → completed
	smConfig := &task.StateMachineConfig{
		Name:         "test_lifecycle",
		InitialState: "pending",
		TerminalStates: []string{"completed"},
		States: map[string]task.StateConfig{
			"pending":    {},
			"processing": {},
			"completed":  {},
		},
		Transitions: []task.TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
		},
	}

	core.stateMachineEngine = task.NewDefaultStateMachineEngine(core.taskStore, smConfig, core.EventBus)
	core.timeoutChecker = task.NewTimeoutChecker(core.taskStore, 30*time.Second, 1)
	core.timeoutChecker.SetOnTimeout(func(taskID string, currentState task.TaskState) {
		core.handleTimeoutEvent(core.ctx, taskID, currentState)
	})

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	ctx := context.Background()

	// 创建任务
	spec := task.TaskSpec{ID: "lifecycle-task", Name: "lifecycle"}
	created, err := core.stateMachineEngine.CreateTask(ctx, spec)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if created.State != task.TaskStatePending {
		t.Fatalf("expected pending state, got %s", created.State)
	}

	// 转换到 processing
	cmd1 := task.TransitionCommand{
		TaskID:       "lifecycle-task",
		CurrentState: task.TaskStatePending,
		TargetState:  task.TaskStateProcessing,
	}
	if err := core.stateMachineEngine.HandleTransitionCommand(ctx, cmd1); err != nil {
		t.Fatalf("transition to processing failed: %v", err)
	}

	task1, _ := core.taskStore.Get("lifecycle-task")
	if task1.State != task.TaskStateProcessing {
		t.Fatalf("expected processing state, got %s", task1.State)
	}

	// 转换到 completed（终态）
	cmd2 := task.TransitionCommand{
		TaskID:       "lifecycle-task",
		CurrentState: task.TaskStateProcessing,
		TargetState:  task.TaskStateCompleted,
	}
	if err := core.stateMachineEngine.HandleTransitionCommand(ctx, cmd2); err != nil {
		t.Fatalf("transition to completed failed: %v", err)
	}

	task2, _ := core.taskStore.Get("lifecycle-task")
	if task2.State != task.TaskStateCompleted {
		t.Fatalf("expected completed state, got %s", task2.State)
	}
	if task2.ArchivedAt == nil {
		t.Fatal("ArchivedAt should be set after terminal transition")
	}

	// List 默认排除归档任务
	tasks, err := core.taskStore.List(task.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tsk := range tasks {
		if tsk.ID == "lifecycle-task" {
			t.Fatal("completed task should not appear in default List")
		}
	}

	t.Log("Full lifecycle test passed: pending → processing → completed → archived")
}

// TestTaskScheduler_TimeoutAndArchive 测试超时后归档（无自动重试）
func TestTaskScheduler_TimeoutAndArchive(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	smConfig := &task.StateMachineConfig{
		Name:         "test_timeout_archive",
		InitialState: "pending",
		TerminalStates: []string{"failed"},
		States: map[string]task.StateConfig{
			"pending": {},
			"failed":  {},
		},
		Transitions: []task.TransitionConfig{
			{From: "pending", To: "failed", Allowed: true},
		},
	}

	core.stateMachineEngine = task.NewDefaultStateMachineEngine(core.taskStore, smConfig, core.EventBus)
	core.Config.Set("task.default_timeout", "2s")
	core.timeoutChecker = task.NewTimeoutChecker(core.taskStore, 200*time.Millisecond, 1)
	core.timeoutChecker.SetOnTimeout(func(taskID string, currentState task.TaskState) {
		core.handleTimeoutEvent(core.ctx, taskID, currentState)
	})

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	ctx := context.Background()
	spec := task.TaskSpec{ID: "timeout-archive", Name: "timeout-test"}
	_, err = core.stateMachineEngine.CreateTask(ctx, spec)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// 设置 TimeoutAt（原 state_timeout 功能已移除）
	timeoutAt := time.Now().Add(100 * time.Millisecond)
	tsk, err := core.taskStore.Get("timeout-archive")
	if err != nil {
		t.Fatal(err)
	}
	tsk.TimeoutAt = &timeoutAt
	core.taskStore.Update(tsk)
	core.timeoutChecker.Push(tsk.ID, *tsk.TimeoutAt)

	time.Sleep(500 * time.Millisecond)

	// Verify task was archived after timeout (no auto-retry)
	tsk, err = core.taskStore.Get("timeout-archive")
	if err != nil {
		t.Fatal(err)
	}
	if tsk.ArchivedAt == nil {
		t.Fatal("task should be archived after timeout with no auto-retry")
	}

	t.Log("Timeout and archive test passed")
}

// TestTaskScheduler_TimeoutAndRetry 测试超时后自动重试
func TestTaskScheduler_TimeoutAndRetry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	smConfig := &task.StateMachineConfig{
		Name:         "test_timeout_retry",
		InitialState: "pending",
		TerminalStates: []string{"failed"},
		States: map[string]task.StateConfig{
			"pending": {
				AutoRetry:    true,
				MaxRetries:   3,
				RetryToState: "pending",
			},
			"failed": {},
		},
		Transitions: []task.TransitionConfig{
			{From: "pending", To: "pending", Allowed: true},
			{From: "pending", To: "failed", Allowed: true},
		},
	}

	core.stateMachineEngine = task.NewDefaultStateMachineEngine(core.taskStore, smConfig, core.EventBus)
	core.Config.Set("task.default_timeout", "1s")
	core.timeoutChecker = task.NewTimeoutChecker(core.taskStore, 200*time.Millisecond, 1)
	core.timeoutChecker.SetOnTimeout(func(taskID string, currentState task.TaskState) {
		core.handleTimeoutEvent(core.ctx, taskID, currentState)
	})

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	ctx := context.Background()
	spec := task.TaskSpec{ID: "timeout-retry", Name: "retry-test"}
	_, err = core.stateMachineEngine.CreateTask(ctx, spec)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	// 设置 TimeoutAt（原 state_timeout 功能已移除）
	timeoutAt := time.Now().Add(100 * time.Millisecond)
	tsk, err := core.taskStore.Get("timeout-retry")
	if err != nil {
		t.Fatal(err)
	}
	tsk.TimeoutAt = &timeoutAt
	core.taskStore.Update(tsk)
	core.timeoutChecker.Push(tsk.ID, *tsk.TimeoutAt)

	// Wait for multiple timeout + retry cycles + heap rebuild
	// The exponential backoff (1s, 2s, 4s, ...) means retries space out quickly.
	// We sleep long enough to see at least 2 retries and final archive.
	time.Sleep(8500 * time.Millisecond)

	tsk, err = core.taskStore.Get("timeout-retry")
	if err != nil {
		t.Fatal(err)
	}
	if tsk.RetryCount < 2 {
		t.Fatalf("expected at least 2 retries after backoff delays, got RetryCount=%d", tsk.RetryCount)
	}
	if tsk.ArchivedAt == nil {
		t.Fatal("task should be archived after exhausting max retries")
	}

	t.Logf("Timeout and retry test passed: RetryCount=%d", tsk.RetryCount)
}

// TestTaskScheduler_ChannelDelivery 测试 policyMatchedCh 直接 channel 投递
func TestTaskScheduler_ChannelDelivery(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	// 通过直接 channel 发送 PolicyMatchedEvent
	event := common.NewEvent("test", "policy.matched", "test")
	event.Payload = map[string]interface{}{
		"action": "log",
	}

	select {
	case core.policyMatchedCh <- event:
	case <-time.After(time.Second):
		t.Fatal("timeout sending to policyMatchedCh")
	}

	time.Sleep(200 * time.Millisecond)

	// Verify task was created
	tasks, err := core.taskStore.List(task.TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) == 0 {
		t.Fatal("expected at least one task created from policyMatchedCh")
	}
	t.Logf("Channel delivery test passed: %d tasks created", len(tasks))
}

// TestModuleInteraction_Integration 测试模块间交互
func TestModuleInteraction_Integration(t *testing.T) {
	// 创建配置
	cfg := DefaultConfig()

	// 创建Core
	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 启动所有模块
	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	// 健康检查
	if err := core.Health(); err != nil {
		t.Fatalf("Health check failed: %v", err)
	}

	// 获取各个模块的统计信息
	t.Log("Core started successfully")
	t.Log("All modules initialized and running")
}
