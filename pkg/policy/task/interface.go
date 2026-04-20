package task

import "time"

// TaskState represents the state of a task
type TaskState int

const (
	TaskStateIdle      TaskState = iota // Idle: Policy created, waiting for match
	TaskStatePending                    // Pending: Matched, waiting to start
	TaskStateRunning                    // Running: Collecting
	TaskStateCompleted                  // Completed: Duration expired
	TaskStateStopped                    // Stopped: Pod/container stopped
	TaskStateFailed                     // Failed: Collector or aggregation engine failed
)

// String returns the string representation of the task state
func (s TaskState) String() string {
	switch s {
	case TaskStateIdle:
		return "idle"
	case TaskStatePending:
		return "pending"
	case TaskStateRunning:
		return "running"
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

// Task represents a collection task
type Task struct {
	ID            string        `json:"id"`
	PolicyID      string        `json:"policy_id"`
	CgroupID      string        `json:"cgroup_id"`
	State         TaskState     `json:"state"`
	Metrics       []string      `json:"metrics"`
	Duration      time.Duration `json:"duration"`
	StartTime     time.Time     `json:"start_time"`
	EndTime       time.Time     `json:"end_time"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	FailureReason string        `json:"failure_reason,omitempty"`
}

// PolicyTaskManager is the interface for managing policy tasks
type PolicyTaskManager interface {
	// CreateTask creates a new task
	CreateTask(policy *Policy, event *Event) (*Task, error)

	// UpdateTaskState updates the state of a task
	UpdateTaskState(taskID string, state TaskState, reason string) error

	// GetTask retrieves a task by ID
	GetTask(taskID string) (*Task, error)

	// GetTasksByPolicy retrieves all tasks for a policy
	GetTasksByPolicy(policyID string) ([]*Task, error)

	// GetTasksByCgroup retrieves all tasks for a cgroup
	GetTasksByCgroup(cgroupID string) ([]*Task, error)

	// DeleteTask deletes a task
	DeleteTask(taskID string) error

	// HandleNRIEvent handles an NRI event and updates tasks accordingly
	HandleNRIEvent(event *Event) error
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

// Policy is a reference to the policy type from the parent package
type Policy struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Targets  []Target `json:"targets"`
	Metrics  []string `json:"metrics"`
	Duration int64    `json:"duration"`
}

// Target is a reference to the target type from the parent package
type Target struct {
	Type      string `json:"type"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// Event is a reference to the event type from the parent package
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	CgroupID  string            `json:"cgroup_id"`
	PodName   string            `json:"pod_name"`
	Namespace string            `json:"namespace"`
	Container string            `json:"container"`
	PID       int32             `json:"pid"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata"`
}
