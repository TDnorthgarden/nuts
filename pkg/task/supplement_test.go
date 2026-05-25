package task

import (
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func TestIsTerminalState(t *testing.T) {
	cfg := &StateMachineConfig{
		TerminalStates: []string{"completed", "failed", "cancelled"},
	}

	tests := []struct {
		state string
		want  bool
	}{
		{"completed", true},
		{"failed", true},
		{"cancelled", true},
		{"pending", false},
		{"processing", false},
		{"unknown", false},
		{"", false},
	}

	for _, tc := range tests {
		got := cfg.IsTerminalState(tc.state)
		if got != tc.want {
			t.Errorf("IsTerminalState(%q) = %v, want %v", tc.state, got, tc.want)
		}
	}
}

func TestIsTransitionAllowed(t *testing.T) {
	cfg := &StateMachineConfig{
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "pending", To: "completed", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
			{From: "processing", To: "failed", Allowed: true},
			{From: "failed", To: "pending", Allowed: true},
		},
	}

	tests := []struct {
		from, to string
		want     bool
	}{
		{"pending", "processing", true},
		{"pending", "completed", true},
		{"processing", "completed", true},
		{"processing", "failed", true},
		{"failed", "pending", true},
		{"pending", "failed", false},
		{"completed", "pending", false},
		{"unknown", "pending", false},
	}

	for _, tc := range tests {
		got := cfg.IsTransitionAllowed(tc.from, tc.to)
		if got != tc.want {
			t.Errorf("IsTransitionAllowed(%q, %q) = %v, want %v", tc.from, tc.to, got, tc.want)
		}
	}
}

func TestListFiltersArchivedTasks(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	now := time.Now()

	active := &Task{
		ID:         "active-1",
		State:      TaskStatePending,
	}
	if err := store.Create(active); err != nil {
		t.Fatal(err)
	}

	archived := &Task{
		ID:         "archived-1",
		State:      TaskStateCompleted,
		ArchivedAt: &now,
	}
	if err := store.Create(archived); err != nil {
		t.Fatal(err)
	}

	// Default filter excludes archived
	tasks, err := store.List(TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 || tasks[0].ID != "active-1" {
		t.Errorf("expected only active task, got %d tasks", len(tasks))
	}

	// IncludeArchived returns all
	tasks, err = store.List(TaskFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 tasks with IncludeArchived, got %d", len(tasks))
	}
}

func TestCountFiltersArchivedTasks(t *testing.T) {
	store := NewTaskStore(db.NewMemoryDB(), 100)

	now := time.Now()

	active := &Task{
		ID:         "active-1",
		State:      TaskStatePending,
	}
	if err := store.Create(active); err != nil {
		t.Fatal(err)
	}

	archived := &Task{
		ID:         "archived-1",
		State:      TaskStateCompleted,
		ArchivedAt: &now,
	}
	if err := store.Create(archived); err != nil {
		t.Fatal(err)
	}

	cnt, err := store.Count(TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 {
		t.Errorf("expected count 1 without IncludeArchived, got %d", cnt)
	}

	cnt, err = store.Count(TaskFilter{IncludeArchived: true})
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 2 {
		t.Errorf("expected count 2 with IncludeArchived, got %d", cnt)
	}
}

func TestTaskArchivedAtAfterTerminalTransition(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	cfg := &StateMachineConfig{
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending":   {},
			"completed": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "completed", Allowed: true},
		},
	}

	engine := NewDefaultStateMachineEngine(store, cfg, nil)

	ctx := t.Context()
	spec := TaskSpec{
		ID:   "task-1",
		Name: "test",
	}
	task, err := engine.CreateTask(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	if task.ArchivedAt != nil {
		t.Fatal("new task should not have ArchivedAt set")
	}

	cmd := TransitionCommand{
		TaskID:       "task-1",
		CurrentState: TaskStatePending,
		TargetState:  TaskStateCompleted,
	}
	if err := engine.HandleTransitionCommand(ctx, cmd); err != nil {
		t.Fatal(err)
	}

	task, err = store.Get("task-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.ArchivedAt == nil {
		t.Fatal("task should have ArchivedAt set after terminal transition")
	}

	// Default list should exclude it
	tasks, err := store.List(TaskFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 active tasks, got %d", len(tasks))
	}
}

func TestTaskArchivedAtClearedOnRetry(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	cfg := &StateMachineConfig{
		InitialState:   "pending",
		TerminalStates: []string{"failed"},
		States: map[string]StateConfig{
			"pending": {},
			"failed":  {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "failed", Allowed: true},
			{From: "failed", To: "pending", Allowed: true},
		},
	}

	engine := NewDefaultStateMachineEngine(store, cfg, nil)

	ctx := t.Context()
	spec := TaskSpec{ID: "retry-task", Name: "test"}
	_, err := engine.CreateTask(ctx, spec)
	if err != nil {
		t.Fatal(err)
	}

	// Transition to terminal state
	cmd := TransitionCommand{
		TaskID:       "retry-task",
		CurrentState: TaskStatePending,
		TargetState:  TaskStateFailed,
	}
	if err := engine.HandleTransitionCommand(ctx, cmd); err != nil {
		t.Fatal(err)
	}

	task, _ := store.Get("retry-task")
	if task.ArchivedAt == nil {
		t.Fatal("ArchivedAt should be set after terminal transition")
	}

	// Retry: transition back to pending
	cmd2 := TransitionCommand{
		TaskID:       "retry-task",
		CurrentState: TaskStateFailed,
		TargetState:  TaskStatePending,
	}
	if err := engine.HandleTransitionCommand(ctx, cmd2); err != nil {
		t.Fatal(err)
	}

	task, _ = store.Get("retry-task")
	if task.ArchivedAt != nil {
		t.Fatal("ArchivedAt should be cleared on retry")
	}
}
