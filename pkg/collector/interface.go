package collector

import "time"

// Collector is the unified interface for all collectors
type Collector interface {
	// Start starts collecting metrics for the specified cgroup and policy
	Start(cgroupID string, policyID string, metrics []string) error

	// Stop stops collecting metrics for the specified cgroup and policy
	Stop(cgroupID string, policyID string) error

	// IsRunning checks if collection is running for the specified cgroup and policy
	IsRunning(cgroupID string, policyID string) bool
}

// StartCollectionRequest represents a request to start collection
type StartCollectionRequest struct {
	CgroupID string   `json:"cgroup_id"`
	PolicyID string   `json:"policy_id"`
	Metrics  []string `json:"metrics"`
}

// StopCollectionRequest represents a request to stop collection
type StopCollectionRequest struct {
	CgroupID string `json:"cgroup_id"`
	PolicyID string `json:"policy_id"`
}

// CollectionStatus represents the status of a collection task
type CollectionStatus struct {
	CgroupID    string    `json:"cgroup_id"`
	PolicyID    string    `json:"policy_id"`
	IsRunning   bool      `json:"is_running"`
	StartTime   time.Time `json:"start_time"`
	Metrics     []string  `json:"metrics"`
}

// ScriptManager is the interface for managing BPF scripts
type ScriptManager interface {
	// LoadScript loads a BPF script of the specified type
	LoadScript(scriptType string, scriptPath string) error

	// UnloadScript unloads a BPF script of the specified type
	UnloadScript(scriptType string, scriptPath string) error

	// ExecuteScript executes a BPF script with the given arguments
	ExecuteScript(scriptType string, args []string) ([]byte, error)
}

// ScriptType represents the type of BPF script
type ScriptType string

const (
	ScriptTypeProcess ScriptType = "process"
	ScriptTypeFile    ScriptType = "file"
	ScriptTypeNetwork ScriptType = "network"
	ScriptTypeIO      ScriptType = "io"
	ScriptTypePerf    ScriptType = "perf"
)
