package datasource

import "time"

// DataSource is the interface for receiving events from container runtime
type DataSource interface {
	// Start starts listening for events
	Start() error

	// Stop stops listening for events
	Stop() error

	// RegisterEventHandler registers an event handler
	RegisterEventHandler(handler EventHandler) error

	// UnregisterEventHandler unregisters an event handler
	UnregisterEventHandler(handler EventHandler) error
}

// EventHandler is the interface for handling events
type EventHandler interface {
	// HandleEvent handles an event
	HandleEvent(event *Event) error
}

// Event represents an event from the data source
type Event struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`      // NRI event type
	CgroupID  string            `json:"cgroup_id"`
	PodName   string            `json:"pod_name"`
	Namespace string            `json:"namespace"`
	Container string            `json:"container"`
	PID       int32             `json:"pid"`
	Timestamp time.Time         `json:"timestamp"`
	Metadata  map[string]string `json:"metadata"`
}

// EventType represents the type of NRI event
type EventType string

const (
	EventTypeRunPodSandbox        EventType = "RunPodSandbox"
	EventTypeStopPodSandbox       EventType = "StopPodSandbox"
	EventTypeRemovePodSandbox     EventType = "RemovePodSandbox"
	EventTypeCreateContainer      EventType = "CreateContainer"
	EventTypePostCreateContainer  EventType = "PostCreateContainer"
	EventTypeStartContainer       EventType = "StartContainer"
	EventTypePostStartContainer   EventType = "PostStartContainer"
	EventTypeUpdateContainer      EventType = "UpdateContainer"
	EventTypePostUpdateContainer  EventType = "PostUpdateContainer"
	EventTypeStopContainer        EventType = "StopContainer"
	EventTypeRemoveContainer      EventType = "RemoveContainer"
)
