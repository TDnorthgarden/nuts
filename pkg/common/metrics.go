package common

// MetricsRecorder records key metrics for the task scheduler.
type MetricsRecorder interface {
	// TaskCreated increments when a task is created.
	TaskCreated()
	// TaskStateTransition counts state transitions.
	TaskStateTransition(from, to string)
	// TaskTimeout counts task timeouts.
	TaskTimeout()
	// TaskRetry counts task retries.
	TaskRetry()
	// TaskArchived counts tasks archived (terminal state).
	TaskArchived()
	// TaskDeleted counts tasks deleted by ArchiveCleaner.
	TaskDeleted()
	// TaskError counts task errors (failed transitions, store errors, etc.).
	TaskError()
}

// NoopMetricsRecorder is a no-op implementation of MetricsRecorder.
type NoopMetricsRecorder struct{}

func (NoopMetricsRecorder) TaskCreated()            {}
func (NoopMetricsRecorder) TaskStateTransition(_, _ string) {}
func (NoopMetricsRecorder) TaskTimeout()            {}
func (NoopMetricsRecorder) TaskRetry()              {}
func (NoopMetricsRecorder) TaskArchived()           {}
func (NoopMetricsRecorder) TaskDeleted()            {}
func (NoopMetricsRecorder) TaskError()              {}
