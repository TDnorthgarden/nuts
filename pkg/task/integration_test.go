package task

import (
	"testing"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func TestTaskStore_Integration(t *testing.T) {
	// 创建任务存储
	store := NewTaskStore(db.NewMemoryDB(), 100)

	// 创建任务
	task := &Task{
		ID:          "test-task-2",
		Name:        "Test Task 2",
		Description: "Test task for store",
		Priority:    10,
		RetryCount:  0,
		Metadata:    map[string]string{"message": "test", "author": "test"},
	}

	// 创建任务
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 获取任务
	retrieved, err := store.Get("test-task-2")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.Name != task.Name {
		t.Errorf("Expected name %s, got %s", task.Name, retrieved.Name)
	}

	// 更新状态
	if err := store.UpdateState("test-task-2", TaskStateProcessing); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}

	retrieved, _ = store.Get("test-task-2")
	if retrieved.State != TaskStateProcessing {
		t.Errorf("Expected state %s, got %s", TaskStateProcessing, retrieved.State)
	}

	// 更新结果
	result := &TaskResult{
		Success: true,
		Output:  "test output",
	}
	if err := store.UpdateResult("test-task-2", result); err != nil {
		t.Fatalf("UpdateResult failed: %v", err)
	}

	// 列出任务
	tasks, err := store.List(TaskFilter{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Expected 1 task, got %d", len(tasks))
	}

	// 删除任务
	if err := store.Delete("test-task-2"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Get("test-task-2")
	if err == nil {
		t.Error("Task should not exist after deletion")
	}
}


