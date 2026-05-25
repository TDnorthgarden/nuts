package task

import (
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/pkg/db"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

func TestParseStateTimeout(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{"", DefaultStateTimeout},
		{"0s", DefaultStateTimeout},
		{"30s", 30 * time.Second},
		{"5m", 5 * time.Minute},
		{"1h", time.Hour},
		{"invalid", DefaultStateTimeout},
		{"-10s", DefaultStateTimeout},
	}
	for _, tc := range tests {
		got := parseStateTimeout(tc.input)
		if got != tc.want {
			t.Errorf("parseStateTimeout(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIsTaskTimeout(t *testing.T) {
	now := time.Now()

	checker := &TimeoutChecker{
		config: &StateMachineConfig{
			States: map[string]StateConfig{
				"pending":    {StateTimeout: "30s"},
				"processing": {StateTimeout: "5m"},
				"no-timeout": {},
			},
		},
		logger: log.GetDefault(),
	}

	tests := []struct {
		name string
		task *Task
		want bool
	}{
		{
			name: "timeout via TimeoutAt",
			task: &Task{
				State:          "pending",
				TimeoutAt:      ptrTime(now.Add(-time.Second)),
				StateUpdatedAt: now,
			},
			want: true,
		},
		{
			name: "not timeout via TimeoutAt (future)",
			task: &Task{
				State:          "pending",
				TimeoutAt:      ptrTime(now.Add(time.Hour)),
				StateUpdatedAt: now,
			},
			want: false,
		},
		{
			name: "timeout via state config",
			task: &Task{
				State:          "pending",
				StateUpdatedAt: now.Add(-31 * time.Second),
			},
			want: true,
		},
		{
			name: "not timeout via state config",
			task: &Task{
				State:          "pending",
				StateUpdatedAt: now.Add(-5 * time.Second),
			},
			want: false,
		},
		{
			name: "state with no timeout config",
			task: &Task{
				State:          "no-timeout",
				StateUpdatedAt: now.Add(-365 * 24 * time.Hour),
			},
			want: false,
		},
		{
			name: "unknown state",
			task: &Task{
				State:          "unknown",
				StateUpdatedAt: now.Add(-time.Hour),
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
	config := &StateMachineConfig{
		InitialState: "pending",
		States: map[string]StateConfig{
			"pending":    {StateTimeout: "10s"},
			"processing": {StateTimeout: "30s"},
		},
	}

	checker := &TimeoutChecker{
		store:         store,
		config:        config,
		checkInterval: time.Minute,
		logger:        log.GetDefault(),
	}

	task1 := &Task{
		ID:             "task-1",
		State:          TaskStatePending,
		StateUpdatedAt: now.Add(-8 * time.Second),
	}
	if err := store.Create(task1); err != nil {
		t.Fatal(err)
	}

	nextWake := checker.checkTimeoutsAndGetNextWake()
	if nextWake < time.Second || nextWake > 3*time.Second {
		t.Errorf("expected ~2s wake for approaching timeout, got %v", nextWake)
	}
}

func TestCheckTimeoutsAndGetNextWake_alreadyTimedOut(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	now := time.Now()
	config := &StateMachineConfig{
		States: map[string]StateConfig{
			"pending": {StateTimeout: "10s"},
		},
	}

	checker := &TimeoutChecker{
		store:         store,
		config:        config,
		checkInterval: time.Minute,
		logger:        log.GetDefault(),
	}

	task1 := &Task{
		ID:             "timeout-task",
		State:          TaskStatePending,
		StateUpdatedAt: now.Add(-15 * time.Second),
	}
	if err := store.Create(task1); err != nil {
		t.Fatal(err)
	}

	nextWake := checker.checkTimeoutsAndGetNextWake()
	if nextWake < 100*time.Millisecond {
		t.Errorf("expected min wake >= 100ms, got %v", nextWake)
	}
}

func TestCheckTimeoutsAndGetNextWake_noTasks(t *testing.T) {
	memDB := db.NewMemoryDB()
	store := NewTaskStore(memDB, 100)

	config := &StateMachineConfig{
		States: map[string]StateConfig{
			"pending": {StateTimeout: "10s"},
		},
	}

	checker := &TimeoutChecker{
		store:         store,
		config:        config,
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
