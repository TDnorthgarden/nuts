package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/nuts-project/nuts/pkg/policy"
	"github.com/nuts-project/nuts/pkg/statemachine"
)

// TaskState represents state of a task
type TaskState int

const (
	TaskStateIdle      TaskState = iota // Idle: Policy created, waiting for match
	TaskStatePending                    // Pending: Matched, waiting to start
	TaskStateRunning                    // Running: Collecting
	TaskStateCompleted                  // Completed: Duration expired
	TaskStateStopped                    // Stopped: Pod/container stopped
	TaskStateFailed                     // Failed: Collector or aggregation engine failed
)

// String returns string representation of task state
func (s TaskState) String() string {
	switch s {
	case TaskStateIdle:
		return "idle"
	case TaskStatePending:
		return "pending"
	case TaskStateRunning:
		return "running" // Represents Collecting, Aggregating, or Diagnosing
	case TaskStateCompleted:
		return "completed"
	case TaskStateStopped:
		return "stopped"
	case TaskStateFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// TaskStore is the interface for persisting tasks
type TaskStore interface {
	// Create creates a new task
	Create(task *Task) error

	// Update updates an existing task
	Update(task *Task) error

	// Get retrieves a task by ID
	Get(id string) (*Task, error)

	// GetByCgroupAndPolicy retrieves a task by cgroup ID and policy ID
	GetByCgroupAndPolicy(cgroupID, policyID string) (*Task, error)

	// GetByState retrieves all tasks with a specific state
	GetByState(state TaskState) ([]*Task, error)

	// Delete deletes a task
	Delete(id string) error
}

// Task represents a policy task with state management
type Task struct {
	mu           sync.RWMutex
	ID           string
	PolicyID     string
	EntityID     string // Pod ID or Container ID (used as key)
	CgroupID     string // Cgroup ID (metadata only)
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
func NewTask(id, policyID, entityID, cgroupID string, metrics map[string][]string, duration time.Duration, notifier policy.PolicyNotifier) *Task {
	sm := statemachine.NewStateMachine(statemachine.StateCreated)

	task := &Task{
		ID:           id,
		PolicyID:     policyID,
		EntityID:     entityID,
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
	// Handler for Created -> Collecting transition
	t.StateMachine.RegisterHandler(statemachine.StateCollecting, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyCollectorStart(t.CgroupID, t.PolicyID, t.Metrics); err != nil {
				return fmt.Errorf("failed to notify collector start: %w", err)
			}
		}
		return nil
	})

	// Handler for Collecting -> Aggregating transition
	t.StateMachine.RegisterHandler(statemachine.StateAggregating, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyCollectorStop(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify collector stop: %w", err)
			}
			if err := t.notifier.NotifyAggregationStart(t.CgroupID, t.PolicyID, t.Duration); err != nil {
				return fmt.Errorf("failed to notify aggregation start: %w", err)
			}
		}
		return nil
	})

	// Handler for Aggregating -> Diagnosing transition
	t.StateMachine.RegisterHandler(statemachine.StateDiagnosing, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyAggregationStop(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify aggregation stop: %w", err)
			}
			if err := t.notifier.NotifyAnalysisStart(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify analysis start: %w", err)
			}
		}
		return nil
	})

	// Handler for Diagnosing -> Completed transition
	t.StateMachine.RegisterHandler(statemachine.StateCompleted, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			if err := t.notifier.NotifyAnalysisStop(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify analysis stop: %w", err)
			}
			if err := t.notifier.NotifyReportStart(t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify report start: %w", err)
			}
			if err := t.notifier.NotifyTaskCompleted(t.ID, t.CgroupID, t.PolicyID); err != nil {
				return fmt.Errorf("failed to notify task completed: %w", err)
			}
		}
		return nil
	})

	// Handler for Collecting/Aggregating/Diagnosing -> Stopped transition (pod/container stop event)
	t.StateMachine.RegisterHandler(statemachine.StateStopped, func(from, to statemachine.State, reason string) error {
		if t.notifier != nil {
			// Stop collector if in collecting state
			if from == statemachine.StateCollecting {
				if err := t.notifier.NotifyCollectorStop(t.CgroupID, t.PolicyID); err != nil {
					return fmt.Errorf("failed to notify collector stop: %w", err)
				}
			}
			// Stop aggregation if in aggregating state
			if from == statemachine.StateAggregating {
				if err := t.notifier.NotifyAggregationStop(t.CgroupID, t.PolicyID); err != nil {
					return fmt.Errorf("failed to notify aggregation stop: %w", err)
				}
			}
			// Stop analysis if in diagnosing state
			if from == statemachine.StateDiagnosing {
				if err := t.notifier.NotifyAnalysisStop(t.CgroupID, t.PolicyID); err != nil {
					return fmt.Errorf("failed to notify analysis stop: %w", err)
				}
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

// StartCollecting transitions the task to collecting state
func (t *Task) StartCollecting(reason string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if err := t.StateMachine.Transition(statemachine.StateCollecting, reason); err != nil {
		return fmt.Errorf("failed to start collecting: %w", err)
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
	store    TaskStore
	notifier policy.PolicyNotifier
}

// NewTaskManager creates a new task manager
func NewTaskManager(notifier policy.PolicyNotifier) *TaskManager {
	return &TaskManager{
		tasks:    make(map[string]*Task),
		store:    NewMemoryTaskStore(),
		notifier: notifier,
	}
}

// SetStore sets the task store
func (tm *TaskManager) SetStore(store TaskStore) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.store = store
}

// SetNotifier sets the policy notifier for the task manager
func (tm *TaskManager) SetNotifier(notifier policy.PolicyNotifier) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.notifier = notifier
}

// CreateTask creates a new task
func (tm *TaskManager) CreateTask(id, policyID, entityID, cgroupID string, metrics map[string][]string, duration time.Duration) *Task {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task := NewTask(id, policyID, entityID, cgroupID, metrics, duration, tm.notifier)
	tm.tasks[id] = task

	// Persist to store
	if tm.store != nil {
		if err := tm.store.Create(task); err != nil {
			// Log error but don't fail task creation
			fmt.Printf("Failed to persist task to store: %v\n", err)
		}
	}

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

	// Delete from store
	if tm.store != nil {
		if err := tm.store.Delete(id); err != nil {
			fmt.Printf("Failed to delete task from store: %v\n", err)
		}
	}
}

// GetOrCreateTaskByEntityID gets an existing task or creates a new one
func (tm *TaskManager) GetOrCreateTaskByEntityID(id, policyID, entityID, cgroupID string, metrics map[string][]string, duration time.Duration) (*Task, bool) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	// Check if task already exists for this entity ID and policy
	for _, task := range tm.tasks {
		if task.EntityID == entityID && task.PolicyID == policyID {
			// Only return non-terminal tasks
			if !task.StateMachine.IsTerminal() {
				return task, true
			}
		}
	}

	// Create new task
	task := NewTask(id, policyID, entityID, cgroupID, metrics, duration, tm.notifier)
	tm.tasks[id] = task

	// Persist to store
	if tm.store != nil {
		if err := tm.store.Create(task); err != nil {
			fmt.Printf("Failed to persist task to store: %v\n", err)
		}
	}

	return task, false
}

// UpdateTaskState updates task state and persists to store
func (tm *TaskManager) UpdateTaskState(taskID string, newState TaskState, reason string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	task, exists := tm.tasks[taskID]
	if !exists {
		return fmt.Errorf("task %s not found", taskID)
	}

	// Convert TaskState to statemachine.State
	var smState statemachine.State
	switch newState {
	case TaskStateIdle:
		smState = statemachine.StateCreated
	case TaskStatePending:
		smState = statemachine.StateCreated
	case TaskStateRunning:
		smState = statemachine.StateCollecting
	case TaskStateStopped:
		smState = statemachine.StateStopped
	case TaskStateCompleted:
		smState = statemachine.StateCompleted
	case TaskStateFailed:
		smState = statemachine.StateFailed
	default:
		return fmt.Errorf("invalid task state: %s", newState)
	}

	// Transition state
	if err := task.StateMachine.Transition(smState, reason); err != nil {
		return fmt.Errorf("failed to transition task state: %w", err)
	}

	// Update in store
	if tm.store != nil {
		if err := tm.store.Update(task); err != nil {
			fmt.Printf("Failed to update task in store: %v\n", err)
		}
	}

	return nil
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

// GetTaskByEntityID returns a task for a specific entity ID and policy
func (tm *TaskManager) GetTaskByEntityID(entityID, policyID string) *Task {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, task := range tm.tasks {
		if task.EntityID == entityID && task.PolicyID == policyID {
			return task
		}
	}
	return nil
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
