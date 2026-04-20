package storage

import "time"

// PolicyStore is the interface for storing policies
type PolicyStore interface {
	// Create creates a new policy
	Create(policy *Policy) error

	// Update updates an existing policy
	Update(policy *Policy) error

	// Delete deletes a policy by ID
	Delete(id string) error

	// Get retrieves a policy by ID
	Get(id string) (*Policy, error)

	// List retrieves all policies
	List() ([]*Policy, error)

	// Query retrieves policies based on query criteria
	Query(query *PolicyQuery) ([]*Policy, error)
}

// Policy represents a monitoring policy
type Policy struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Targets   []Target  `json:"targets"`
	Metrics   []string  `json:"metrics"`
	Duration  int64     `json:"duration"` // in seconds
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Target represents a monitoring target (pod or container)
type Target struct {
	Type      string `json:"type"`      // "pod" or "container"
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
}

// PolicyQuery represents query criteria for policies
type PolicyQuery struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Target    string `json:"target,omitempty"`
}

// EventStore is the interface for storing events (time-series data)
type EventStore interface {
	// Write writes a single event
	Write(event *Event) error

	// WriteBatch writes multiple events
	WriteBatch(events []*Event) error

	// Query retrieves events based on query criteria
	Query(query *EventQuery) ([]*Event, error)

	// QueryByTimeRange retrieves events within a time range
	QueryByTimeRange(start, end time.Time, filters map[string]string) ([]*Event, error)

	// Delete deletes events for a cgroup and policy
	Delete(cgroupID string, policyID string) error
}

// Event represents an event from the data source
type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	CgroupID  string                 `json:"cgroup_id"`
	PolicyID  string                 `json:"policy_id"`
	Timestamp time.Time              `json:"timestamp"`
	Data      map[string]interface{} `json:"data"`
}

// EventQuery represents query criteria for events
type EventQuery struct {
	CgroupID  string    `json:"cgroup_id,omitempty"`
	PolicyID  string    `json:"policy_id,omitempty"`
	EventType string    `json:"event_type,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

// AuditStore is the interface for storing audit records
type AuditStore interface {
	// Create creates a new audit record
	Create(audit *Audit) error

	// Get retrieves an audit by ID
	Get(id string) (*Audit, error)

	// ListByPolicy retrieves all audits for a policy
	ListByPolicy(policyID string) ([]*Audit, error)

	// ListByCgroup retrieves all audits for a cgroup
	ListByCgroup(cgroupID string) ([]*Audit, error)

	// Update updates an existing audit
	Update(audit *Audit) error
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

// DiagnosisStore is the interface for storing diagnosis results
type DiagnosisStore interface {
	// Create creates a new diagnosis
	Create(diagnosis *Diagnosis) error

	// Get retrieves a diagnosis by ID
	Get(id string) (*Diagnosis, error)

	// ListByAudit retrieves all diagnoses for an audit
	ListByAudit(auditID string) ([]*Diagnosis, error)

	// Update updates an existing diagnosis
	Update(diagnosis *Diagnosis) error
}

// Diagnosis represents a diagnosis result
type Diagnosis struct {
	ID          string       `json:"id"`
	AuditID     string       `json:"audit_id"`
	Bottlenecks []*Bottleneck `json:"bottlenecks"`
	Summary     string       `json:"summary"`
	Severity    string       `json:"severity"`
	CreatedAt   time.Time    `json:"created_at"`
}

// Bottleneck represents a performance bottleneck
type Bottleneck struct {
	Type        string                 `json:"type"`        // cpu, memory, io, network, process
	Severity    string                 `json:"severity"`    // low, medium, high, critical
	Description string                 `json:"description"`
	Metrics     map[string]interface{} `json:"metrics"`
	Suggestions []string               `json:"suggestions"`
}
