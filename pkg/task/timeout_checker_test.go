package task

import (
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

func TestIsTaskTimeout(t *testing.T) {
	now := time.Now()

	checker := &TimeoutChecker{
		logger: log.GetDefault(),
	}

	tests := []struct {
		name string
		task *Task
		want bool
	}{
		{
			name: "timeout (past TimeoutAt)",
			task: &Task{
				State:     "pending",
				TimeoutAt: ptrTime(now.Add(-time.Second)),
			},
			want: true,
		},
		{
			name: "not timeout (future TimeoutAt)",
			task: &Task{
				State:     "pending",
				TimeoutAt: ptrTime(now.Add(time.Hour)),
			},
			want: false,
		},
		{
			name: "no TimeoutAt set",
			task: &Task{
				State: "pending",
			},
			want: false,
		},
		{
			name: "nil TimeoutAt",
			task: &Task{
				State:     "processing",
				TimeoutAt: nil,
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := checker.isTaskTimeout(tc.task, now)
			if got != tc.want {
				t.Errorf("isTaskTimeout = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCheckTimeoutsAndGetNextWake(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	now := time.Now()
	timeoutAt := now.Add(2 * time.Second)

	checker := &TimeoutChecker{
		store:         store,
		checkInterval: time.Minute,
		logger:        log.GetDefault(),
	}

	task1 := &Task{
		ID:        "task-1",
		State:     TaskStatePending,
		TimeoutAt: &timeoutAt,
	}
	if err := store.Create(task1); err != nil {
		t.Fatal(err)
	}
	checker.Push("task-1", timeoutAt)

	nextWake := checker.checkTimeoutsAndGetNextWake()
	if nextWake < time.Second || nextWake > 3*time.Second {
		t.Errorf("expected ~2s wake for approaching timeout, got %v", nextWake)
	}
}

func TestCheckTimeoutsAndGetNextWake_alreadyTimedOut(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	timeoutAt := time.Now().Add(-time.Second)

	checker := &TimeoutChecker{
		store:         store,
		checkInterval: time.Minute,
		logger:        log.GetDefault(),
	}

	var called bool
	checker.SetOnTimeout(func(taskID string, _ TaskState) {
		called = true
	})

	store.Create(&Task{
		ID: "timeout-task", State: TaskStatePending, TimeoutAt: &timeoutAt,
	})
	checker.rebuildHeap()

	checker.checkTimeoutsAndGetNextWake()

	if !called {
		t.Fatal("expected timeout callback to be invoked for already expired task")
	}
}

func TestCheckTimeoutsAndGetNextWake_noTasks(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	checker := &TimeoutChecker{
		store:         store,
		checkInterval: 30 * time.Second,
		logger:        log.GetDefault(),
	}

	nextWake := checker.checkTimeoutsAndGetNextWake()
	if nextWake < 30*time.Second {
		t.Errorf("expected full interval for no tasks, got %v", nextWake)
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
