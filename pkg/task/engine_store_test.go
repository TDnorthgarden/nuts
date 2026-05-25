package task

import (
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func TestEngineTaskStore(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	// Test Create
	task := &Task{
		ID:       "task-1",
		Name:     "test task",
		Priority: 50,
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	if task.State != TaskStatePending {
		t.Fatalf("expected pending state, got %s", task.State)
	}

	// Test Get
	got, err := store.Get("task-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != "task-1" {
		t.Fatalf("unexpected id: %s", got.ID)
	}
	if got.Name != "test task" {
		t.Fatalf("unexpected name: %s", got.Name)
	}

	// Test UpdateState
	if err := store.UpdateState("task-1", TaskStateProcessing); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}
	got2, _ := store.Get("task-1")
	if got2.State != TaskStateProcessing {
		t.Fatalf("expected running state, got %s", got2.State)
	}
	if len(got2.StateHistory) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(got2.StateHistory))
	}

	// Test UpdateResult
	result := &TaskResult{Success: true, Output: "done"}
	if err := store.UpdateResult("task-1", result); err != nil {
		t.Fatalf("UpdateResult failed: %v", err)
	}
	got3, _ := store.Get("task-1")
	if !got3.Result.Success {
		t.Fatal("expected success result")
	}

	// Test List with filter
	tasks, err := store.List(TaskFilter{State: TaskStateProcessing})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}

	// Test Count
	cnt, _ := store.Count(TaskFilter{})
	if cnt != 1 {
		t.Fatalf("expected count 1, got %d", cnt)
	}

	// Test Delete
	if err := store.Delete("task-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = store.Get("task-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestEngineTaskStoreWithSQLite(t *testing.T) {
	path := "task_sqlite_test.db"
	dbInst, err := db.NewSQLiteDB(path, "")
	if err != nil {
		t.Fatalf("create sqlite: %v", err)
	}
	defer dbInst.Close()

	store := NewTaskStore(dbInst, 100)

	task := &Task{
		ID:        "sqlite-task",
		Name:      "sqlite task",
		Priority:  10,
		State:     TaskStatePending,
		CreatedAt: time.Now(),
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get("sqlite-task")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != "sqlite-task" {
		t.Fatalf("unexpected id: %s", got.ID)
	}

	// Update state
	if err := store.UpdateState("sqlite-task", TaskStateCompleted); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}

	history, err := store.GetStateHistory("sqlite-task")
	if err != nil {
		t.Fatalf("GetStateHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 history record, got %d", len(history))
	}
	if history[0].From != TaskStatePending || history[0].To != TaskStateCompleted {
		t.Fatalf("unexpected transition: %s -> %s", history[0].From, history[0].To)
	}
}

func TestEngineTaskStoreTimeFields(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	now := time.Now()
	started := now.Add(-time.Minute)
	completed := now

	task := &Task{
		ID:          "time-task",
		State:       TaskStateCompleted,
		CreatedAt:   now.Add(-2 * time.Minute),
		StartedAt:   &started,
		CompletedAt: &completed,
	}
	if err := store.Create(task); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get("time-task")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.StartedAt == nil || got.CompletedAt == nil {
		t.Fatal("time pointers should not be nil after unmarshal")
	}
	if !got.StartedAt.Equal(started) {
		t.Fatalf("started time mismatch: %v vs %v", got.StartedAt, started)
	}
	if !got.CompletedAt.Equal(completed) {
		t.Fatalf("completed time mismatch: %v vs %v", got.CompletedAt, completed)
	}
}
