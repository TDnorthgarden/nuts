package policy

import (
	"fmt"
	"time"
)

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error on field '%s': %s", e.Field, e.Message)
}

// PolicyMatcher is the interface for matching policies against events
type PolicyMatcher interface {
	// Match checks if an event matches any policy
	Match(event *Event) (*MatchResult, error)
}

// PolicyReceiver is the interface for receiving and managing policies
type PolicyReceiver interface {
	// Receive creates a new policy
	Receive(policy *Policy) error

	// Update updates an existing policy
	Update(policy *Policy) error

	// Delete deletes a policy by ID
	Delete(id string) error

	// Get retrieves a policy by ID
	Get(id string) (*Policy, error)

	// List retrieves all policies
	List() ([]*Policy, error)
}

// PolicyNotifier is the interface for notifying other components about policy events
type PolicyNotifier interface {
	// NotifyCollectorStart notifies the collector to start collection
	NotifyCollectorStart(cgroupID string, policyID string, metrics map[string][]string) error

	// NotifyCollectorStop notifies the collector to stop collection
	NotifyCollectorStop(cgroupID string, policyID string) error

	// NotifyAggregationStart notifies the aggregation engine to start aggregation
	NotifyAggregationStart(cgroupID string, policyID string, duration time.Duration) error

	// NotifyAggregationStop notifies the aggregation engine to stop aggregation
	NotifyAggregationStop(cgroupID string, policyID string) error

	// NotifyAnalysisStart notifies the analysis engine to start analysis
	NotifyAnalysisStart(cgroupID string, policyID string) error

	// NotifyAnalysisStop notifies the analysis engine to stop analysis
	NotifyAnalysisStop(cgroupID string, policyID string) error

	// NotifyReportStart notifies the reporting engine to start report generation
	NotifyReportStart(cgroupID string, policyID string) error

	// NotifyReportStop notifies the reporting engine to stop report generation
	NotifyReportStop(cgroupID string, policyID string) error

	// NotifyTaskCompleted notifies that a task has completed successfully
	NotifyTaskCompleted(taskID string, cgroupID string, policyID string) error

	// NotifyTaskFailed notifies that a task has failed
	NotifyTaskFailed(taskID string, cgroupID string, policyID string, err error) error
}

// Policy represents a monitoring policy
type Policy struct {
	ID        string              `json:"id"`
	Name      string              `json:"name"`
	Metrics   map[string][]string `json:"metrics"`  // key: category (process/file/network/io/perf), value: list of script names
	Duration  int64               `json:"duration"` // in seconds
	Rule      string              `json:"rule"`     // DSL rule in YAML format (can include macros, lists, etc.)
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

// Event represents an event from the data source
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"event.type"` // NRI event type
	CgroupID  string            `json:"cgroup.id"`
	PodName   string            `json:"pod.name"`
	Namespace string            `json:"pod.namespace"`
	Container string            `json:"container.name"`
	PID       int32             `json:"pod.pid"`
	Timestamp time.Time         `json:"event.timestamp"`
	Metadata  map[string]string `json:"metadata"`
}

// MatchResult represents the result of a policy match
type MatchResult struct {
	PolicyID string              `json:"policy_id"`
	Metrics  map[string][]string `json:"metrics"`  // key: category, value: list of script names
	Duration int64               `json:"duration"` // in seconds
	Matched  bool                `json:"matched"`
	Reason   string              `json:"reason"`
}

// Validate validates the policy
func (p *Policy) Validate() error {
	if p.ID == "" {
		return &ValidationError{Field: "id", Message: "id is required"}
	}
	if p.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	if len(p.Metrics) == 0 {
		return &ValidationError{Field: "metrics", Message: "at least one metric category is required"}
	}
	validCategories := map[string]bool{
		"process": true,
		"file":    true,
		"network": true,
		"io":      true,
		"perf":    true,
	}
	for category := range p.Metrics {
		if !validCategories[category] {
			return &ValidationError{Field: "metrics", Message: fmt.Sprintf("invalid metric category '%s', must be one of: process, file, network, io, perf", category)}
		}
		if len(p.Metrics[category]) == 0 {
			return &ValidationError{Field: "metrics", Message: fmt.Sprintf("metric category '%s' must have at least one script", category)}
		}
	}
	if p.Duration <= 0 {
		return &ValidationError{Field: "duration", Message: "duration must be greater than 0"}
	}
	return nil
}
