package task

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

// ============================================================
// engine_store.go error paths
// ============================================================

func TestCreate_EmptyID(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	err := store.Create(&Task{})
	if err == nil {
		t.Fatal("expected error for empty ID")
	}
}

func TestGet_NotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	_, err := store.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestUpdate_VersionConflict(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "v1", Name: "test"}
	if err := store.Create(task); err != nil {
		t.Fatal(err)
	}

	// 先读一次拿到 version
	got, _ := store.Get("v1")

	// 外部修改并写入，version 递增
	got.Name = "modified"
	if err := store.Update(got); err != nil {
		t.Fatal(err)
	}

	// 用旧 version 写入，应冲突
	task.Name = "stale"
	if err := store.Update(task); err == nil {
		t.Fatal("expected version conflict error")
	}
}

func TestUpdate_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "ghost", Name: "test"}
	err := store.Update(task)
	if err == nil {
		t.Fatal("expected error for updating non-existent task")
	}
}

func TestUpdateState_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	err := store.UpdateState("nonexistent", TaskStateProcessing)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestUpdateStateWithRecord_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	err := store.UpdateStateWithRecord("nonexistent", TaskStateProcessing, "test", "reason")
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestTransitionState_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	_, err := store.TransitionState("nonexistent", TaskStateProcessing, "test", "reason", nil, false, false, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestGetStateHistory_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	_, err := store.GetStateHistory("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestUpdateResult_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	err := store.UpdateResult("nonexistent", &TaskResult{Success: true})
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestDelete_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	// Delete non-existent task should not error (idempotent)
	err := store.Delete("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTransitionState_PostCommitFailure(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "pc-fail", Name: "test"}
	if err := store.Create(task); err != nil {
		t.Fatal(err)
	}

	postCommit := func() error {
		return fmt.Errorf("eventbus unavailable")
	}

	// postCommit 失败不应影响状态转换
	result, err := store.TransitionState("pc-fail", TaskStateProcessing, "test", "reason", nil, false, false, nil, nil, postCommit)
	if err != nil {
		t.Fatalf("TransitionState should succeed even if postCommit fails: %v", err)
	}
	if result.State != TaskStateProcessing {
		t.Fatalf("state should be processing, got %s", result.State)
	}
}

func TestTransitionState_SetRetryCount(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "retry", Name: "test"}
	if err := store.Create(task); err != nil {
		t.Fatal(err)
	}

	retryCount := 3
	result, err := store.TransitionState("retry", TaskStateProcessing, "test", "retry", nil, false, false, nil, &retryCount, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.RetryCount != 3 {
		t.Fatalf("expected retry count 3, got %d", result.RetryCount)
	}
}

func TestTransitionState_ArchiveAndClear(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	task := &Task{ID: "archive", Name: "test"}
	if err := store.Create(task); err != nil {
		t.Fatal(err)
	}

	// 归档
	now := time.Now()
	result, err := store.TransitionState("archive", TaskStateCompleted, "test", "done", &now, false, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ArchivedAt == nil {
		t.Fatal("expected ArchivedAt to be set")
	}

	// 清除归档（重试场景）
	result2, err := store.TransitionState("archive", TaskStatePending, "test", "retry", nil, true, false, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result2.ArchivedAt != nil {
		t.Fatal("expected ArchivedAt to be cleared")
	}
}

func TestList_WithPagination(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	for i := 0; i < 10; i++ {
		task := &Task{ID: fmt.Sprintf("page-%d", i), Name: "test"}
		if err := store.Create(task); err != nil {
			t.Fatal(err)
		}
	}

	// Limit
	tasks, err := store.List(TaskFilter{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks))
	}

	// Offset + Limit
	tasks2, err := store.List(TaskFilter{Offset: 5, Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks2) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks2))
	}

	// Offset beyond total
	tasks3, err := store.List(TaskFilter{Offset: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks3) != 0 {
		t.Fatalf("expected 0 tasks, got %d", len(tasks3))
	}
}

func TestList_FilterByPriority(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	store.Create(&Task{ID: "hi", Priority: 100})
	store.Create(&Task{ID: "lo", Priority: 1})

	p := 100
	tasks, err := store.List(TaskFilter{Priority: &p})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "hi" {
		t.Fatalf("expected 1 high-priority task, got %v", tasks)
	}
}

// ============================================================
// state_machine_engine.go error paths
// ============================================================

func TestHandleTransitionCommand_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		InitialState: "pending",
		States:       map[string]StateConfig{"pending": {}, "done": {}},
		Transitions:  []TransitionConfig{{From: "pending", To: "done", Allowed: true}},
	}
	engine := NewDefaultStateMachineEngine(store, config, nil)

	cmd := TransitionCommand{
		TaskID:       "nonexistent",
		CurrentState: TaskStatePending,
		TargetState:  "done",
	}
	err := engine.HandleTransitionCommand(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestHandleTransitionCommand_StateMismatch(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		InitialState: "pending",
		States:       map[string]StateConfig{"pending": {}, "processing": {}, "done": {}},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "processing", To: "done", Allowed: true},
		},
	}
	engine := NewDefaultStateMachineEngine(store, config, nil)
	engine.CreateTask(context.Background(), TaskSpec{ID: "mismatch"})

	// 先转换到 processing
	engine.HandleTransitionCommand(context.Background(), TransitionCommand{
		TaskID: "mismatch", CurrentState: TaskStatePending, TargetState: TaskStateProcessing,
	})

	// 用旧状态去转换，应报 state mismatch
	err := engine.HandleTransitionCommand(context.Background(), TransitionCommand{
		TaskID: "mismatch", CurrentState: TaskStatePending, TargetState: "done",
	})
	if err == nil {
		t.Fatal("expected state mismatch error")
	}
}

func TestHandleTransitionCommand_Idempotent(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		InitialState: "pending",
		States:       map[string]StateConfig{"pending": {}, "processing": {}},
		Transitions:  []TransitionConfig{{From: "pending", To: "processing", Allowed: true}},
	}
	engine := NewDefaultStateMachineEngine(store, config, nil)
	engine.CreateTask(context.Background(), TaskSpec{ID: "idem"})

	// 第一次转换
	engine.HandleTransitionCommand(context.Background(), TransitionCommand{
		TaskID: "idem", CurrentState: TaskStatePending, TargetState: TaskStateProcessing,
	})

	// 重复转换（已在目标状态），应成功（幂等）
	err := engine.HandleTransitionCommand(context.Background(), TransitionCommand{
		TaskID: "idem", CurrentState: TaskStateProcessing, TargetState: TaskStateProcessing,
	})
	if err != nil {
		t.Fatalf("idempotent transition should succeed: %v", err)
	}
}

func TestHandleTransitionCommand_WithSetRetryCount(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{
		InitialState: "pending",
		States:       map[string]StateConfig{"pending": {}, "processing": {}},
		Transitions:  []TransitionConfig{{From: "pending", To: "processing", Allowed: true}},
	}
	engine := NewDefaultStateMachineEngine(store, config, nil)
	engine.CreateTask(context.Background(), TaskSpec{ID: "retry-cmd"})

	retryCount := 5
	err := engine.HandleTransitionCommand(context.Background(), TransitionCommand{
		TaskID: "retry-cmd", CurrentState: TaskStatePending, TargetState: TaskStateProcessing,
		SetRetryCount: &retryCount,
	})
	if err != nil {
		t.Fatal(err)
	}

	task, _ := store.Get("retry-cmd")
	if task.RetryCount != 5 {
		t.Fatalf("expected retry count 5, got %d", task.RetryCount)
	}
}

func TestGetTaskState_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	config := &StateMachineConfig{InitialState: "pending", States: map[string]StateConfig{"pending": {}}}
	engine := NewDefaultStateMachineEngine(store, config, nil)

	_, err := engine.GetTaskState("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

// ============================================================
// timeout_checker.go error paths
// ============================================================

func TestGetTaskTimeout_TaskNotFound(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	checker := NewTimeoutChecker(store, 0, 0)

	_, err := checker.GetTaskTimeout("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent task")
	}
}

func TestGetTaskTimeout_NoTimeoutAt(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	store.Create(&Task{ID: "no-tm", State: TaskStatePending})

	checker := NewTimeoutChecker(store, 0, 0)

	got, err := checker.GetTaskTimeout("no-tm")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil timeout for task without TimeoutAt, got %v", got)
	}
}

func TestGetTaskTimeout_WithTimeoutAt(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	timeoutAt := time.Now().Add(5 * time.Minute)
	store.Create(&Task{ID: "has-timeout", State: TaskStatePending, TimeoutAt: &timeoutAt})

	checker := NewTimeoutChecker(store, 0, 0)

	got, err := checker.GetTaskTimeout("has-timeout")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(timeoutAt) {
		t.Fatalf("expected %v, got %v", timeoutAt, *got)
	}
}

func TestTimeoutCallback_Invoked(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)
	timeoutAt := time.Now().Add(-time.Hour)
	store.Create(&Task{ID: "cb-task", State: TaskStatePending, TimeoutAt: &timeoutAt})

	checker := NewTimeoutChecker(store, 0, 0)
	// rebuild 将过期 TimeoutAt 任务 deadline 设为 now
	checker.rebuildHeap()

	var called bool
	var calledID string
	checker.SetOnTimeout(func(taskID string, currentState TaskState) {
		called = true
		calledID = taskID
	})

	checker.checkTimeoutsAndGetNextWake()

	if !called {
		t.Fatal("expected timeout callback to be invoked")
	}
	if calledID != "cb-task" {
		t.Fatalf("expected task ID cb-task, got %s", calledID)
	}
}
