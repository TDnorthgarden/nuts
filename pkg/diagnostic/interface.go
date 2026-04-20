package diagnostic

import "time"

// DiagnosticEngine is the interface for diagnostic analysis
type DiagnosticEngine interface {
	// Analyze performs diagnostic analysis on an audit
	Analyze(audit *Audit) (*DiagnosisResult, error)

	// GenerateReport generates a diagnostic report
	GenerateReport(diagnosis *DiagnosisResult) (*Report, error)
}

// DiagnosticStrategy is the interface for diagnostic strategies
type DiagnosticStrategy interface {
	// Name returns the name of the strategy
	Name() string

	// Analyze performs diagnostic analysis
	Analyze(audit *Audit) (*DiagnosisResult, error)
}

// BottleneckDetector is the interface for detecting bottlenecks
type BottleneckDetector interface {
	// Detect detects bottlenecks in an audit
	Detect(audit *Audit) ([]*Bottleneck, error)
}

// Audit represents an audit record from the aggregation engine
type Audit struct {
	ID             string                 `json:"id"`
	PolicyID       string                 `json:"policy_id"`
	CgroupID       string                 `json:"cgroup_id"`
	StartTime      time.Time              `json:"start_time"`
	EndTime        time.Time              `json:"end_time"`
	AggregatedData map[string]interface{} `json:"aggregated_data"`
	CreatedAt      time.Time              `json:"created_at"`
}

// DiagnosisResult represents the result of a diagnostic analysis
type DiagnosisResult struct {
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

// Report represents a diagnostic report
type Report struct {
	ID          string    `json:"id"`
	DiagnosisID string    `json:"diagnosis_id"`
	Content     string    `json:"content"`
	Format      string    `json:"format"` // json, html, markdown
	CreatedAt   time.Time `json:"created_at"`
}
