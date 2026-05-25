package task

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func TestDefaultStateMachineEngine_CreateTask(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending": {},
		},
	}

	engine := NewDefaultStateMachineEngine(store, config, nil)

	spec := TaskSpec{
		ID:   "task-1",
		Name: "Test Task",
	}

	ctx := context.Background()
	task, err := engine.CreateTask(ctx, spec)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if task.State != TaskStatePending {
		t.Errorf("Initial state should be pending, got %s", task.State)
	}

	// 使用GetStateHistory获取状态历史
	history, err := store.GetStateHistory("task-1")
	if err != nil {
		t.Fatalf("GetStateHistory failed: %v", err)
	}

	if len(history) != 1 {
		t.Errorf("Should have 1 history record, got %d", len(history))
	}
}

func TestDefaultStateMachineEngine_HandleTransitionCommand(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending":    {},
			"processing": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
		},
	}

	engine := NewDefaultStateMachineEngine(store, config, nil)

	// 先创建任务
	spec := TaskSpec{
		ID:   "task-1",
		Name: "Test Task",
	}
	ctx := context.Background()
	engine.CreateTask(ctx, spec)

	// 执行状态转换
	cmd := TransitionCommand{
		TaskID:       "task-1",
		CurrentState: TaskStatePending,
		TargetState:  TaskStateProcessing,
		ComponentInfo: ComponentInfo{
			Name: "test-component",
		},
		Result: &CommandResult{
			Success: true,
			Message: "test transition",
		},
	}

	err := engine.HandleTransitionCommand(ctx, cmd)
	if err != nil {
		t.Fatalf("HandleTransitionCommand failed: %v", err)
	}

	// 验证状态
	task, _ := store.Get("task-1")
	if task.State != TaskStateProcessing {
		t.Errorf("State should be processing, got %s", task.State)
	}
}

func TestDefaultStateMachineEngine_TransitionNotAllowed(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending": {},
			"running": {},
			"failed":  {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "running", Allowed: true},
			// pending -> failed 不允许
		},
	}

	engine := NewDefaultStateMachineEngine(store, config, nil)

	// 创建任务
	spec := TaskSpec{
		ID:   "task-1",
		Name: "Test Task",
	}
	ctx := context.Background()
	engine.CreateTask(ctx, spec)

	// 尝试非法转换
	cmd := TransitionCommand{
		TaskID:       "task-1",
		CurrentState: TaskStatePending,
		TargetState:  TaskStateFailed,
		ComponentInfo: ComponentInfo{
			Name: "test-component",
		},
		Result: &CommandResult{
			Success: true,
			Message: "should fail",
		},
	}

	err := engine.HandleTransitionCommand(ctx, cmd)
	if err == nil {
		t.Error("Should fail for disallowed transition")
	}
}

func TestDefaultStateMachineEngine_GetTaskHistory(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending":    {},
			"processing": {},
			"completed":  {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
		},
	}

	engine := NewDefaultStateMachineEngine(store, config, nil)

	// 创建任务
	spec := TaskSpec{
		ID:   "task-1",
		Name: "Test Task",
	}
	ctx := context.Background()
	engine.CreateTask(ctx, spec)

	// 多次状态转换
	engine.HandleTransitionCommand(ctx, TransitionCommand{
		TaskID:        "task-1",
		CurrentState:  TaskStatePending,
		TargetState:   TaskStateProcessing,
		ComponentInfo: ComponentInfo{Name: "test"},
		Result:        &CommandResult{Success: true},
	})

	engine.HandleTransitionCommand(ctx, TransitionCommand{
		TaskID:        "task-1",
		CurrentState:  TaskStateProcessing,
		TargetState:   TaskStateCompleted,
		ComponentInfo: ComponentInfo{Name: "test"},
		Result:        &CommandResult{Success: true},
	})

	// 获取历史
	history, err := engine.GetTaskHistory("task-1")
	if err != nil {
		t.Fatalf("GetTaskHistory failed: %v", err)
	}

	// 应该有 3 条记录（pending创建 + pending->running + running->completed）
	if len(history) != 3 {
		t.Errorf("Should have 3 history records, got %d", len(history))
	}
}

func TestDefaultStateMachineEngine_ConcurrentTransitionDifferentTasks(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending":    {},
			"processing": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
		},
	}

	engine := NewDefaultStateMachineEngine(store, config, nil)
	ctx := context.Background()

	// 创建 10 个不同任务
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("concurrent-engine-%d", i)
		_, err := engine.CreateTask(ctx, TaskSpec{ID: id})
		if err != nil {
			t.Fatalf("CreateTask %s failed: %v", id, err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-engine-%d", idx)
			for j := 0; j < 20; j++ {
				cmd := TransitionCommand{
					TaskID:        id,
					CurrentState:  TaskStatePending,
					TargetState:   TaskStateProcessing,
					ComponentInfo: ComponentInfo{Name: "test"},
					Result:        &CommandResult{Success: true},
				}
				engine.HandleTransitionCommand(ctx, cmd)
			}
		}(i)
	}
	wg.Wait()

	// 每个任务最终应处于 processing 状态
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("concurrent-engine-%d", i)
		got, err := store.Get(id)
		if err != nil {
			t.Errorf("Get %s failed: %v", id, err)
			continue
		}
		if got.State != TaskStateProcessing {
			t.Errorf("%s: expected processing, got %s", id, got.State)
		}
	}
}

func TestDefaultStateMachineEngine_ConcurrentTransitionSameTask(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending": {},
		},
	}

	engine := NewDefaultStateMachineEngine(store, config, nil)
	ctx := context.Background()
	_, err := engine.CreateTask(ctx, TaskSpec{ID: "same-task"})
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := TransitionCommand{
				TaskID:        "same-task",
				CurrentState:  TaskStatePending,
				TargetState:   TaskStateProcessing,
				ComponentInfo: ComponentInfo{Name: "test"},
				Result:        &CommandResult{Success: true},
			}
			// 大量并发尝试（仅一次能成功，其余应因状态不匹配失败）
			for j := 0; j < 50; j++ {
				engine.HandleTransitionCommand(ctx, cmd)
			}
		}()
	}
	wg.Wait()

	got, err := store.Get("same-task")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	// 状态为 pending（因为配置没有 pending -> processing 转换）
	if got.State != TaskStatePending {
		t.Errorf("expected pending (no transition defined), got %s", got.State)
	}
}
