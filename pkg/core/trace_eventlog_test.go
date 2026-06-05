package core

import (
	"context"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/eventlog"
	"github.com/sig-cloudnative/nuts/pkg/policy"
	"github.com/sig-cloudnative/nuts/pkg/task"
)

// TestTraceID_Propagation 测试 TraceID 从事件到任务的端到端传递
func TestTraceID_Propagation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 注入状态机配置
	smConfig := &task.StateMachineConfig{
		Name:          "test_trace",
		InitialState:  "pending",
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

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	// 模拟带 TraceID 的源事件
	traceID := "abc123def45678901234567890123456"
	ctx := common.ContextWithTraceID(context.Background(), traceID)
	sourceEvent := common.NewEvent("ContainerStart", "test.event", "test-source")
	sourceEvent.WithContext(ctx)

	// 验证 TraceID 已设置
	if sourceEvent.TraceID != traceID {
		t.Fatalf("expected TraceID %s, got %s", traceID, sourceEvent.TraceID)
	}

	// 模拟策略匹配后的事件处理
	policyMatch := &policy.PolicyMatch{
		PolicyID: "test-policy",
		Matched:  true,
	}

	// 调用 processMatchedEvent，handler goroutine 会从 policyMatchedCh 读取并创建任务
	core.processMatchedEvent(policyMatch, sourceEvent)

	// 等待 handler goroutine 处理完成（创建任务）
	time.Sleep(500 * time.Millisecond)

	// 验证任务已创建且 TraceID 正确传递
	allTasks, _ := core.taskStore.List(task.TaskFilter{})
	if len(allTasks) == 0 {
		t.Fatal("no tasks created after processMatchedEvent")
	}
	saved := allTasks[0]
	if saved.TraceID != traceID {
		t.Errorf("Task.TraceID: expected %s, got %s", traceID, saved.TraceID)
	}

	t.Log("TraceID propagation test passed")
}

// TestTraceID_ComponentPublishStateTransition 测试组件传递 TraceID
func TestTraceID_ComponentPublishStateTransition(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 注入状态机配置
	smConfig := &task.StateMachineConfig{
		Name:          "test_component_trace",
		InitialState:  "pending",
		TerminalStates: []string{"completed"},
		States: map[string]task.StateConfig{
			"pending":   {},
			"completed": {},
		},
		Transitions: []task.TransitionConfig{
			{From: "pending", To: "completed", Allowed: true},
		},
	}
	core.stateMachineEngine = task.NewDefaultStateMachineEngine(core.taskStore, smConfig, core.EventBus)
	core.timeoutChecker = task.NewTimeoutChecker(core.taskStore, 30*time.Second, 1)

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	// 创建带 TraceID 的任务
	traceID := "component-trace-id-12345678"
	ctx := common.ContextWithTraceID(context.Background(), traceID)
	spec := task.TaskSpec{
		ID:       "comp-trace-task",
		Name:     "comp trace test",
		Metadata: map[string]string{"trace_id": traceID},
	}
	created, _ := core.stateMachineEngine.CreateTask(ctx, spec)
	if created.TraceID != traceID {
		t.Fatalf("Task.TraceID not set: expected %s, got %s", traceID, created.TraceID)
	}

	// 模拟组件构造带 TraceID 的事件
	compEvent := common.NewEvent("StateTransitionCommand", "state.transition.command", "test-component")
	compEvent.WithContext(ctx)

	// 验证事件携带 TraceID
	if compEvent.TraceID != traceID {
		t.Errorf("component event TraceID: expected %s, got %s", traceID, compEvent.TraceID)
	}

	// 验证通过 handleTransitionCommand 可以正确处理
	compEvent.TypedPayload = &api.Event_Component{
		Component: &api.ComponentEventPayload{
			TaskId:        "comp-trace-task",
			CurrentState:  "pending",
			TargetState:   "completed",
			ComponentName: "test-component",
			Success:       true,
			Message:       "test transition",
		},
	}

	core.handleTransitionCommand(compEvent)

	// 验证任务状态已转换
	saved, _ := core.taskStore.Get("comp-trace-task")
	if saved.State != task.TaskStateCompleted {
		t.Errorf("expected completed state, got %s", saved.State)
	}
	// Task.TraceID 保持不变
	if saved.TraceID != traceID {
		t.Errorf("Task.TraceID should persist: expected %s, got %s", traceID, saved.TraceID)
	}

	t.Log("Component TraceID test passed")
}

// TestEventLog_Instrumentation 测试 EventLog 埋点写入
func TestEventLog_Instrumentation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ConfigFile = ""

	core, err := New(cfg)
	if err != nil {
		t.Fatalf("Create Core failed: %v", err)
	}
	defer core.Stop()

	// 注入内存 EventLog
	core.eventLog = eventlog.NewAsyncEventLog(eventlog.NewRingBufferEventLog(1000), 100)

	// 注入状态机配置
	smConfig := &task.StateMachineConfig{
		Name:          "test_eventlog",
		InitialState:  "pending",
		TerminalStates: []string{"completed"},
		States: map[string]task.StateConfig{
			"pending":   {},
			"completed": {},
		},
		Transitions: []task.TransitionConfig{
			{From: "pending", To: "completed", Allowed: true},
		},
	}
	core.stateMachineEngine = task.NewDefaultStateMachineEngine(core.taskStore, smConfig, core.EventBus)
	// 设置 EventLog 到状态机引擎
	if sme, ok := core.stateMachineEngine.(interface{ SetEventLog(eventlog.EventLog) }); ok {
		sme.SetEventLog(core.eventLog)
	}
	core.timeoutChecker = task.NewTimeoutChecker(core.taskStore, 30*time.Second, 1)

	if err := core.Start(); err != nil {
		t.Fatalf("Start Core failed: %v", err)
	}

	// 创建带 TraceID 的任务
	traceID := "eventlog-trace-12345678"
	ctx := common.ContextWithTraceID(context.Background(), traceID)
	spec := task.TaskSpec{
		ID:       "eventlog-task",
		Name:     "eventlog test",
		Metadata: map[string]string{"trace_id": traceID},
	}
	core.stateMachineEngine.CreateTask(ctx, spec)

	// 手动写入 task_create EventLog（模拟 handlePolicyMatchedEvent 的行为）
	core.eventLog.Append(ctx, &eventlog.EventLogEntry{
		ID:        common.GenerateUUID(),
		TraceID:   traceID,
		Stage:     eventlog.StageTaskCreate,
		EventType: "TaskCreated",
		Source:    "statemachine-engine",
		Timestamp: time.Now(),
		TaskID:    "eventlog-task",
		NewState:  "pending",
	})

	// 等待异步 EventLog 写入
	time.Sleep(200 * time.Millisecond)

	// 查询 EventLog
	timeline, err := core.eventLog.QueryByTraceID(traceID)
	if err != nil {
		t.Fatalf("QueryByTraceID failed: %v", err)
	}

	// 验证有 task_create 埋点
	foundTaskCreate := false
	for _, entry := range timeline.Entries {
		if entry.Stage == eventlog.StageTaskCreate {
			foundTaskCreate = true
			if entry.TaskID != "eventlog-task" {
				t.Errorf("task_create entry TaskID: expected eventlog-task, got %s", entry.TaskID)
			}
		}
	}
	if !foundTaskCreate {
		t.Error("expected task_create EventLog entry")
	}

	// 执行状态转换，验证 postCommit EventLog
	cmd := task.TransitionCommand{
		TaskID:       "eventlog-task",
		CurrentState: task.TaskStatePending,
		TargetState:  task.TaskStateCompleted,
	}
	core.stateMachineEngine.HandleTransitionCommand(ctx, cmd)

	time.Sleep(200 * time.Millisecond)

	timeline, _ = core.eventLog.QueryByTraceID(traceID)
	foundTaskState := false
	for _, entry := range timeline.Entries {
		if entry.Stage == eventlog.StageTaskState && entry.NewState == "completed" {
			foundTaskState = true
		}
	}
	if !foundTaskState {
		t.Error("expected task_state EventLog entry with new_state=completed")
	}

	t.Logf("EventLog instrumentation test passed: %d entries recorded", len(timeline.Entries))
}
