package client

import "time"

// CollectorClient is the interface for the collector gRPC client
type CollectorClient interface {
	// StartCollection starts collection for the specified cgroup and policy
	StartCollection(req *StartCollectionRequest) error

	// StopCollection stops collection for the specified cgroup and policy
	StopCollection(req *StopCollectionRequest) error

	// IsCollecting checks if collection is running for the specified cgroup and policy
	IsCollecting(cgroupID string, policyID string) (bool, error)

	// GetStatus gets the status of collection for the specified cgroup and policy
	GetStatus(cgroupID string, policyID string) (*CollectionStatus, error)

	// Close closes the client connection
	Close() error
}

// StartCollectionRequest represents a request to start collection
type StartCollectionRequest struct {
	CgroupID string   `json:"cgroup_id"`
	PolicyID string   `json:"policy_id"`
	Metrics  []string `json:"metrics"`
	Timeout  time.Duration `json:"timeout,omitempty"`
}

// StopCollectionRequest represents a request to stop collection
type StopCollectionRequest struct {
	CgroupID string `json:"cgroup_id"`
	PolicyID string `json:"policy_id"`
	Timeout  time.Duration `json:"timeout,omitempty"`
}

// CollectionStatus represents the status of a collection task
type CollectionStatus struct {
	CgroupID  string    `json:"cgroup_id"`
	PolicyID  string    `json:"policy_id"`
	IsRunning bool      `json:"is_running"`
	StartTime time.Time `json:"start_time"`
	Metrics   []string  `json:"metrics"`
}

// CollectorConfig represents the configuration for the collector client
type CollectorConfig struct {
	Address    string        `json:"address"`     // collector service address (e.g., "localhost:50051")
	Timeout    time.Duration `json:"timeout"`     // request timeout
	MaxRetries int           `json:"max_retries"` // maximum number of retries
}
