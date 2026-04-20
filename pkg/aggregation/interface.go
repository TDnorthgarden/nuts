package aggregation

import "time"

// EventAggregator is the interface for aggregating events
type EventAggregator interface {
	// Aggregate aggregates a list of events
	Aggregate(events []*Event) (*AggregatedEvent, error)

	// SetAlgorithm sets the aggregation algorithm
	SetAlgorithm(algorithm AggregationAlgorithm) error
}

// AggregationAlgorithm is the interface for aggregation algorithms
type AggregationAlgorithm interface {
	// Name returns the name of the algorithm
	Name() string

	// Aggregate aggregates a list of events
	Aggregate(events []*Event) (*AggregatedEvent, error)

	// Validate validates the events before aggregation
	Validate(events []*Event) error
}

// TaskScheduler is the interface for scheduling aggregation tasks
type TaskScheduler interface {
	// Schedule schedules an aggregation task
	Schedule(task *Task) error

	// Cancel cancels an aggregation task
	Cancel(taskID string) error
}

// Event represents an event from the data source
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	CgroupID  string            `json:"cgroup_id"`
	PolicyID  string            `json:"policy_id"`
	Timestamp time.Time         `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// AggregatedEvent represents an aggregated event
type AggregatedEvent struct {
	ID          string                 `json:"id"`
	CgroupID    string                 `json:"cgroup_id"`
	PolicyID    string                 `json:"policy_id"`
	StartTime   time.Time              `json:"start_time"`
	EndTime     time.Time              `json:"end_time"`
	EventCount  int                    `json:"event_count"`
	Aggregated  map[string]interface{} `json:"aggregated"`
	Algorithm   string                 `json:"algorithm"`
}

// Task represents an aggregation task
type Task struct {
	ID        string        `json:"id"`
	CgroupID  string        `json:"cgroup_id"`
	PolicyID  string        `json:"policy_id"`
	Duration  time.Duration `json:"duration"`
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Status    string        `json:"status"`
}

// Audit represents an audit record
type Audit struct {
	ID             string                 `json:"id"`
	PolicyID       string                 `json:"policy_id"`
	CgroupID       string                 `json:"cgroup_id"`
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	AggregatedData map[string]interface{} `json:"aggregated_data"`
	CreatedAt      time.Time              `json:"created_at"`
}
