package datasource

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
)

// NRIEventType represents the type of NRI event
type NRIEventType string

const (
	// Pod lifecycle events
	NRIEventTypeRunPodSandbox    NRIEventType = "RunPodSandbox"
	NRIEventTypeStopPodSandbox   NRIEventType = "StopPodSandbox"
	NRIEventTypeRemovePodSandbox NRIEventType = "RemovePodSandbox"

	// Container creation events
	NRIEventTypeCreateContainer     NRIEventType = "CreateContainer"
	NRIEventTypePostCreateContainer NRIEventType = "PostCreateContainer"

	// Container start events
	NRIEventTypeStartContainer     NRIEventType = "StartContainer"
	NRIEventTypePostStartContainer NRIEventType = "PostStartContainer"

	// Container update events
	NRIEventTypeUpdateContainer     NRIEventType = "UpdateContainer"
	NRIEventTypePostUpdateContainer NRIEventType = "PostUpdateContainer"

	// Container stop events
	NRIEventTypeStopContainer NRIEventType = "StopContainer"

	// Container remove events
	NRIEventTypeRemoveContainer NRIEventType = "RemoveContainer"
)

// NRIEvent represents an NRI event with additional metadata
type NRIEvent struct {
	Type      NRIEventType
	Pod       *api.PodSandbox
	Container *api.Container
	Timestamp time.Time
}

// NRIEventHandler handles NRI events
type NRIEventHandler interface {
	HandleNRIEvent(event *NRIEvent) error
}

// NRIDataSource implements DataSource using containerd NRI
type NRIDataSource struct {
	mu            sync.RWMutex
	stub          stub.Stub
	eventHandlers []EventHandler
	eventChan     chan *NRIEvent
	ctx           context.Context
	cancel        context.CancelFunc
	running       bool
	cgroupPath    string
}

// NewNRIDataSource creates a new NRI data source
func NewNRIDataSource() *NRIDataSource {
	return &NRIDataSource{
		eventHandlers: make([]EventHandler, 0),
		eventChan:     make(chan *NRIEvent, 1000),
		cgroupPath:    "/sys/fs/cgroup", // Default cgroup path
	}
}

// Start starts the NRI data source
func (d *NRIDataSource) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return fmt.Errorf("data source is already running")
	}

	// Create context
	d.ctx, d.cancel = context.WithCancel(context.Background())

	// Create NRI stub with plugin index (must be 2 digits)
	//stub, err := stub.New(d, stub.WithPluginIdx("01"), stub.WithPluginName("nuts"))
	stub, err := stub.New(d)
	if err != nil {
		return fmt.Errorf("failed to create NRI stub: %w", err)
	}
	d.stub = stub

	// Start event processing goroutine
	go d.processEvents()

	// Start NRI stub
	if err := d.stub.Run(d.ctx); err != nil {
		return fmt.Errorf("failed to run NRI stub: %w", err)
	}

	d.running = true
	return nil
}

// Stop stops the NRI data source
func (d *NRIDataSource) Stop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return nil
	}

	d.cancel()
	d.running = false
	return nil
}

// RegisterEventHandler registers an event handler
func (d *NRIDataSource) RegisterEventHandler(handler EventHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.eventHandlers = append(d.eventHandlers, handler)
}

// processEvents processes NRI events from the channel
func (d *NRIDataSource) processEvents() {
	for {
		select {
		case <-d.ctx.Done():
			return
		case event := <-d.eventChan:
			d.handleEvent(event)
		}
	}
}

// handleEvent handles a single NRI event
func (d *NRIDataSource) handleEvent(event *NRIEvent) {
	for _, handler := range d.eventHandlers {
		if nriHandler, ok := handler.(NRIEventHandler); ok {
			if err := nriHandler.HandleNRIEvent(event); err != nil {
				fmt.Printf("Error handling NRI event: %v\n", err)
			}
		}
	}
}

// emitEvent emits an NRI event to the channel
func (d *NRIDataSource) emitEvent(eventType NRIEventType, pod *api.PodSandbox, container *api.Container) {
	event := &NRIEvent{
		Type:      eventType,
		Pod:       pod,
		Container: container,
		Timestamp: time.Now(),
	}

	select {
	case d.eventChan <- event:
	default:
		fmt.Printf("Event channel full, dropping event: %s\n", eventType)
	}
}

// Configure implements stub.ConfigureInterface
func (d *NRIDataSource) Configure(ctx context.Context, config, runtime, version string) (api.EventMask, error) {
	// Subscribe to events that need policy matching
	// Only these events are needed: RunPodSandbox, StartContainer, StopPodSandbox, StopContainer, RemoveContainer
	mask := api.EventMask(0)
	mask.Set(api.Event_RUN_POD_SANDBOX)
	mask.Set(api.Event_START_CONTAINER)
	mask.Set(api.Event_STOP_POD_SANDBOX)
	mask.Set(api.Event_STOP_CONTAINER)
	mask.Set(api.Event_REMOVE_CONTAINER)
	return mask, nil
}

// Synchronize implements stub.SynchronizeInterface
func (d *NRIDataSource) Synchronize(ctx context.Context, pods []*api.PodSandbox, containers []*api.Container) ([]*api.ContainerUpdate, error) {
	// Handle synchronization
	return nil, nil
}

// Shutdown implements stub.ShutdownInterface
func (d *NRIDataSource) Shutdown(ctx context.Context) {
	d.Stop()
}

// RunPodSandbox implements stub.RunPodInterface
func (d *NRIDataSource) RunPodSandbox(ctx context.Context, pod *api.PodSandbox) error {
	d.emitEvent(NRIEventTypeRunPodSandbox, pod, nil)
	return nil
}

// StopPodSandbox implements stub.StopPodInterface
func (d *NRIDataSource) StopPodSandbox(ctx context.Context, pod *api.PodSandbox) error {
	d.emitEvent(NRIEventTypeStopPodSandbox, pod, nil)
	return nil
}

// RemovePodSandbox implements stub.RemovePodInterface
func (d *NRIDataSource) RemovePodSandbox(ctx context.Context, pod *api.PodSandbox) error {
	d.emitEvent(NRIEventTypeRemovePodSandbox, pod, nil)
	return nil
}

// CreateContainer implements stub.CreateContainerInterface
func (d *NRIDataSource) CreateContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	d.emitEvent(NRIEventTypeCreateContainer, pod, container)
	return nil, nil, nil
}

// PostCreateContainer implements stub.PostCreateContainerInterface
func (d *NRIDataSource) PostCreateContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	d.emitEvent(NRIEventTypePostCreateContainer, pod, container)
	return nil
}

// StartContainer implements stub.StartContainerInterface
func (d *NRIDataSource) StartContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	d.emitEvent(NRIEventTypeStartContainer, pod, container)
	return nil
}

// PostStartContainer implements stub.PostStartContainerInterface
func (d *NRIDataSource) PostStartContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	d.emitEvent(NRIEventTypePostStartContainer, pod, container)
	return nil
}

// UpdateContainer implements stub.UpdateContainerInterface
func (d *NRIDataSource) UpdateContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	d.emitEvent(NRIEventTypeUpdateContainer, pod, container)
	return nil, nil, nil
}

// PostUpdateContainer implements stub.PostUpdateContainerInterface
func (d *NRIDataSource) PostUpdateContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	d.emitEvent(NRIEventTypePostUpdateContainer, pod, container)
	return nil
}

// StopContainer implements stub.StopContainerInterface
func (d *NRIDataSource) StopContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) ([]*api.ContainerUpdate, error) {
	d.emitEvent(NRIEventTypeStopContainer, pod, container)
	return nil, nil
}

// RemoveContainer implements stub.RemoveContainerInterface
func (d *NRIDataSource) RemoveContainer(ctx context.Context, pod *api.PodSandbox, container *api.Container) error {
	d.emitEvent(NRIEventTypeRemoveContainer, pod, container)
	return nil
}

// GetCgroupFromNRIEvent retrieves cgroup information from an NRI event
// Following the strategy defined in nri-cgroup.md
func GetCgroupFromNRIEvent(event *NRIEvent) (string, error) {
	// Strategy based on event type and PID availability
	switch event.Type {
	case NRIEventTypeRunPodSandbox:
		// RunPodSandbox: PID not available, construct cgroup path from pod UID
		// Kubernetes cgroup path format: /k8s.io/<pod_uid>
		if event.Pod != nil && event.Pod.Pid != 0 {
			if cgroup, err := GetCgroupFromPID(event.Pod.Pid); err == nil {
				return cgroup, nil
			}
		}
		// Fallback: try to get from pod sandbox CgroupParent
		if event.Pod != nil && event.Pod.Linux != nil && event.Pod.Linux.CgroupParent != "" {
			return event.Pod.Linux.CgroupParent, nil
		}
		return "", fmt.Errorf("no cgroup information available for RunPodSandbox event")

	case NRIEventTypeStopPodSandbox, NRIEventTypeRemovePodSandbox:
		// StopPodSandbox, RemovePodSandbox: PID not available, construct cgroup path from pod UID
		if event.Pod != nil && event.Pod.Uid != "" {
			return fmt.Sprintf("/k8s.io/%s", event.Pod.Uid), nil
		}
		// Fallback: try to get from pod sandbox CgroupParent
		if event.Pod != nil && event.Pod.Linux != nil && event.Pod.Linux.CgroupParent != "" {
			return event.Pod.Linux.CgroupParent, nil
		}
		return "", fmt.Errorf("no cgroup information available for %s event", event.Type)

	case NRIEventTypeCreateContainer:
		// CreateContainer: PID not available, try to get from pod sandbox
		if event.Pod != nil && event.Pod.Linux != nil && event.Pod.Linux.CgroupParent != "" {
			return event.Pod.Linux.CgroupParent, nil
		}
		// Fallback: construct cgroup path from pod UID
		if event.Pod != nil && event.Pod.Uid != "" {
			return fmt.Sprintf("/k8s.io/%s", event.Pod.Uid), nil
		}
		return "", fmt.Errorf("no cgroup information available for CreateContainer event")

	case NRIEventTypePostCreateContainer:
		// PostCreateContainer: PID may be available, but don't rely on it
		// Try PID first, then fall back to pod sandbox
		if event.Container != nil && event.Container.Pid != 0 {
			if cgroup, err := GetCgroupFromPID(event.Container.Pid); err == nil {
				return cgroup, nil
			}
		}
		if event.Pod != nil && event.Pod.Linux != nil && event.Pod.Linux.CgroupParent != "" {
			return event.Pod.Linux.CgroupParent, nil
		}
		// Fallback: construct cgroup path from pod UID
		if event.Pod != nil && event.Pod.Uid != "" {
			return fmt.Sprintf("/k8s.io/%s", event.Pod.Uid), nil
		}
		return "", fmt.Errorf("no cgroup information available for PostCreateContainer event")

	case NRIEventTypeStartContainer, NRIEventTypePostStartContainer,
		NRIEventTypeUpdateContainer, NRIEventTypePostUpdateContainer,
		NRIEventTypeStopContainer:
		// These events have PID available, use it to get cgroup
		if event.Container != nil && event.Container.Pid != 0 {
			return GetCgroupFromPID(event.Container.Pid)
		}
		// Fallback: try to get from pod sandbox
		if event.Pod != nil && event.Pod.Linux != nil && event.Pod.Linux.CgroupParent != "" {
			return event.Pod.Linux.CgroupParent, nil
		}
		// Fallback: construct cgroup path from pod UID
		if event.Pod != nil && event.Pod.Uid != "" {
			return fmt.Sprintf("/k8s.io/%s", event.Pod.Uid), nil
		}
		return "", fmt.Errorf("no PID available in %s event", event.Type)

	case NRIEventTypeRemoveContainer:
		// RemoveContainer: PID not available, construct cgroup path from pod UID
		if event.Pod != nil && event.Pod.Uid != "" {
			return fmt.Sprintf("/k8s.io/%s", event.Pod.Uid), nil
		}
		// Fallback: try to get from pod sandbox CgroupParent
		if event.Pod != nil && event.Pod.Linux != nil && event.Pod.Linux.CgroupParent != "" {
			return event.Pod.Linux.CgroupParent, nil
		}
		return "", fmt.Errorf("no cgroup information available for RemoveContainer event")

	default:
		return "", fmt.Errorf("unhandled event type: %s", event.Type)
	}
}

// GetCgroupFromPID retrieves cgroup path for a given PID
func GetCgroupFromPID(pid uint32) (string, error) {
	cgroupPath := fmt.Sprintf("/proc/%d/cgroup", pid)
	data, err := os.ReadFile(cgroupPath)
	if err != nil {
		return "", fmt.Errorf("failed to read cgroup file for PID %d: %w", pid, err)
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}

		// Parse cgroup line format: hierarchy-ID:controller-list:cgroup-path
		parts := strings.Split(line, ":")
		if len(parts) >= 3 {
			cgroupPath := parts[2]
			if cgroupPath != "" {
				// Return the cgroup path
				return cgroupPath, nil
			}
		}
	}

	return "", fmt.Errorf("no valid cgroup path found for PID %d", pid)
}

// ConvertNRIEventToEvent converts an NRI event to a generic Event
func ConvertNRIEventToEvent(nriEvent *NRIEvent) (*Event, error) {
	event := &Event{
		Type:      string(nriEvent.Type),
		Timestamp: nriEvent.Timestamp,
		Metadata:  make(map[string]string),
	}

	// Add pod information
	if nriEvent.Pod != nil {
		event.ID = nriEvent.Pod.Id
		event.PodName = nriEvent.Pod.Name
		event.PID = int32(nriEvent.Pod.Pid)
		event.Namespace = nriEvent.Pod.Namespace
		event.Metadata["pod.id"] = nriEvent.Pod.Id
		event.Metadata["pod.uid"] = nriEvent.Pod.Uid
		for k, v := range nriEvent.Pod.Labels {
			event.Metadata["pod.label."+k] = v
		}
		for k, v := range nriEvent.Pod.Annotations {
			event.Metadata["pod.annotation."+k] = v
		}
	}

	// Add container information
	if nriEvent.Container != nil {
		event.ID = nriEvent.Container.Id
		event.Container = nriEvent.Container.Name
		event.PID = int32(nriEvent.Container.Pid)
		event.Metadata["container.id"] = nriEvent.Container.Id
		event.Metadata["pod_sandbox.id"] = nriEvent.Container.PodSandboxId
		event.Metadata["container.state"] = nriEvent.Container.State.String()
		for k, v := range nriEvent.Container.Labels {
			event.Metadata["container.label."+k] = v
		}
		for k, v := range nriEvent.Container.Annotations {
			event.Metadata["container.annotation."+k] = v
		}

		var builder strings.Builder
		for _, arg := range nriEvent.Container.Args {
			builder.WriteString(fmt.Sprintf(" %s", arg))
		}
		event.Metadata["container.arg"] = builder.String()

		builder.Reset()
		for _, arg := range nriEvent.Container.Env {
			builder.WriteString(fmt.Sprintf(" %s", arg))

		}
		event.Metadata["container.env"] += builder.String()
	}

	// Try to get cgroup information (works for both pod and container events)
	cgroup, err := GetCgroupFromNRIEvent(nriEvent)
	if err == nil {
		event.CgroupID = cgroup
	}

	return event, nil
}
