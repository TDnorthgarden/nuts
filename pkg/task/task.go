package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/nuts-project/nuts/pkg/policy"
	"github.com/nuts-project/nuts/pkg/statemachine"
)

// Task represents a policy task with state management
type Task struct {
	mu           sync.RWMutex
	ID           string
	PolicyID     string
	CgroupID     string
	StateMachine *statemachine.StateMachine
	StartTime    time.Time
	EndTime      time.Time
	Metrics      map[string][]string
	Duration     time.Duration
	Error        error
	Result       *TaskResult
	notifier     policy.PolicyNotifier
}

// TaskResult represents the result of a completed task
type TaskResult struct {
	CollectedData  map[string]interface{} `json:"collected_data"`
	AggregatedData map[string]interface{} `json:"aggregated_data"`
	AnalysisResult map[string]interface{} `json:"analysis_result"`
	Report         map[string]interface{} `json:"report"`
}

// NewTask creates a new policy task
func NewTask(id, policyID, cgroupID string, metrics map[string][]string, duration time.Duration, notifier policy.PolicyNotifier) *Task {
	sm := statemachine.NewStateMachine(statemachine.StateCreated)

	task := &Task{
		ID:           id,
		PolicyID:     policyID,
		CgroupID:     cgroupID,
		StateMachine: sm,
		StartTime:    time.Now(),
		Metrics:      metrics,
		Duration:     duration,
		Result:       &TaskResult{},
		notifier:     notifier,
	}

	// Register state transition handlers
	task.registerStateHandlers()

	return task
}

// registerStateHandlers registers handlers for state transitions
func (t *Task) registerStateHandlers() {
	// Handler for Created -> Running transition
	t.StateMachine.RegisterHandler(statemachine.StateRunning, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyCollectorStart(t.CgroupID, t.PolicyID, t.Metrics); err != nil {
				return fmt.Errorf("failed to notify collector start: %w", err)
			}
			if err := t.notifier.NotifyAggregationStart(t.CgroupID, t.PolicyID, t.Duration); err != nil {
				return fmt.Errorf("failed to notify aggregation start: %w", err)
			}
		}
		return nil
	})

	// Handler for Running -> Stopped transition
	t.StateMachine.RegisterHandler(statemachine.StateStopped, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyCollectorStop(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify collector stop: %w", err)
			}
			if err := t.notifier.NotifyAggregationStop(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify aggregation stop: %w", err)
			}
		}
		return nil
	})

	// Handler for any -> Completed transition
	t.StateMachine.RegisterHandler(statemachine.StateCompleted, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyTaskCompleted(t.ID, t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify task completed: %w", err)
			}
		}
		return nil
	})

	// Handler for any -> Failed transition
	t.StateMachine.RegisterHandler(statemachine.StateFailed, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyTaskFailed(t.ID, t.CgroupID, t.PolicyID, t.Error); err != nil {
				return fmt.Errorf("failed to notify task failed: %w", err)
			}
		}
		return nil
	})
}

// StartRunning transitions the task to running state
func (t *Task) StartRunning(reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.StateMachine.Transition(statemachine.StateRunning, reason); err != nil {
		return fmt.Errorf("failed to start running: %w", err)
	}

	return nil
}

// Stop transitions the task to stopped state
func (t *Task) Stop(reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.StateMachine.Transition(statemachine.StateStopped, reason); err != nil {
		return fmt.Errorf("failed to stop task: %w", err)
	}

	return nil
}

// Complete transitions the task to completed state
func (t *Task) Complete(reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.StateMachine.Transition(statemachine.StateCompleted, reason); err != nil {
		return fmt.Errorf("failed to complete task: %w", err)
	}

	t.EndTime = time.Now()
	return nil
}

// Fail transitions the task to failed state
func (t *Task) Fail(reason string, err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.StateMachine.Transition(statemachine.StateFailed, reason); err != nil {
		return fmt.Errorf("failed to mark task as failed: %w", err)
	}

	t.Error = err
	t.EndTime = time.Now()
	return nil
}

// GetState returns the current state of the task
func (t *Task) GetState() statemachine.State {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.StateMachine.Current()
}

// GetDuration returns the duration of the task
func (t *Task) GetDuration() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.EndTime.IsZero() {
		return time.Since(t.StartTime)
	}
	return t.EndTime.Sub(t.StartTime)
}

// GetHistory returns the state transition history
func (t *Task) GetHistory() []statemachine.StateTransition {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.StateMachine.History()
}

// SetCollectedData sets the collected data
func (t *Task) SetCollectedData(data map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Result.CollectedData = data
}

// SetAggregatedData sets the aggregated data
func (t *Task) SetAggregatedData(data map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Result.AggregatedData = data
}

// SetAnalysisResult sets the analysis result
func (t *Task) SetAnalysisResult(result map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Result.AnalysisResult = result
}

// SetReport sets the report
func (t *Task) SetReport(report map[string]interface{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Result.Report = report
}

// ToPolicy converts task to policy event
func (t *Task) ToPolicyEvent() *policy.Event {
	t.mu.RLock()
	defer t.mu.RUnlock()

	return &policy.Event{
		ID:       t.ID,
		CgroupID: t.CgroupID,
		Metadata: map[string]string{
			"policy_id": t.PolicyID,
			"state":     string(t.StateMachine.Current()),
		},
	}
}

// TaskManager manages multiple policy tasks
type TaskManager struct {
	mu       sync.RWMutex
	tasks    map[string]*Task
	notifier policy.PolicyNotifier
}

// NewTaskManager creates a new task manager
func NewTaskManager(notifier policy.PolicyNotifier) *TaskManager {
	return &TaskManager{
		tasks:    make(map[string]*Task),
		notifier: notifier,
	}
}

// SetNotifier sets the policy notifier for the task manager
func (tm *TaskManager) SetNotifier(notifier policy.PolicyNotifier) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.notifier = notifier
}

// CreateTask creates a new task
func (tm *TaskManager) CreateTask(id, policyID, cgroupID string, metrics map[string][]string, duration time.Duration) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task := NewTask(id, policyID, cgroupID, metrics, duration, tm.notifier)
	tm.tasks[id] = task
	return task
}

// GetTask retrieves a task by ID
func (tm *TaskManager) GetTask(id string) (*Task, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	task, exists := tm.tasks[id]
	return task, exists
}

// DeleteTask deletes a task by ID
func (tm *TaskManager) DeleteTask(id string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	delete(tm.tasks, id)
}

// ListTasks returns all tasks
func (tm *TaskManager) ListTasks() []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make([]*Task, 0, len(tm.tasks))
	for _, task := range tm.tasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// ListTasksByPolicy returns all tasks for a specific policy
func (tm *TaskManager) ListTasksByPolicy(policyID string) []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make([]*Task, 0)
	for _, task := range tm.tasks {
		if task.PolicyID == policyID {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// ListTasksByCgroup returns all tasks for a specific cgroup
func (tm *TaskManager) ListTasksByCgroup(cgroupID string) []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make([]*Task, 0)
	for _, task := range tm.tasks {
		if task.CgroupID == cgroupID {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// ListTasksByState returns all tasks in a specific state
func (tm *TaskManager) ListTasksByState(state statemachine.State) []*Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	tasks := make([]*Task, 0)
	for _, task := range tm.tasks {
		if task.GetState() == state {
			tasks = append(tasks, task)
		}
	}
	return tasks
}

// CleanupCompletedTasks removes completed or failed tasks older than the specified duration
func (tm *TaskManager) CleanupCompletedTasks(olderThan time.Duration) int {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	now := time.Now()
	count := 0

	for id, task := range tm.tasks {
		if task.StateMachine.IsTerminal() && !task.EndTime.IsZero() {
			if now.Sub(task.EndTime) > olderThan {
				delete(tm.tasks, id)
				count++
			}
		}
	}

	return count
}
