package datasource

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/containerd/nri/pkg/api"
	"github.com/containerd/nri/pkg/stub"
	nutsapi "github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
	"github.com/sig-cloudnative/nuts/pkg/log"
)

var _ = stub.ConfigureInterface(&NRIDataSource{})

type NRIConfig struct {
	SocketPath  string   `toml:"socket_path"`
	PluginName  string   `toml:"plugin_name"`
	PluginIndex string   `toml:"plugin_index"`
	Events      []string `toml:"events"`
	BufferSize  int      `toml:"buffer_size"`
	StopTimeout int      `toml:"stop_timeout"`

	HealthCheckInterval time.Duration `toml:"health_check_interval"`
	ReconnectInterval   time.Duration `toml:"reconnect_interval"`
}

type NRIDataSource struct {
	config *NRIConfig
	logger log.Logger

	stub stub.Stub
	mask stub.EventMask

	eventCh chan<- *common.Event

	ownCtx  context.Context
	stop    context.CancelFunc
	wg      sync.WaitGroup
	started atomic.Bool
	readyCh chan struct{}
	readyOnce sync.Once

	startTime time.Time
}

func NewNRIDataSource(config *NRIConfig) (*NRIDataSource, error) {
	if config == nil {
		config = &NRIConfig{
			SocketPath:  "/var/run/nri.sock",
			PluginName:  "01-nuts",
			PluginIndex: "01",
			Events:      []string{"RunPodSandbox", "StopPodSandbox", "StartContainer", "StopContainer", "RemoveContainer"},
			BufferSize:  1000,
		}
	}

	return &NRIDataSource{
		config:  config,
		logger:  log.GetDefault(),
		readyCh: make(chan struct{}),
	}, nil
}

func (n *NRIDataSource) SetLogger(logger log.Logger) {
	n.logger = logger
}

func (n *NRIDataSource) Start(ctx context.Context, eventCh chan<- *common.Event) error {
	if !n.started.CompareAndSwap(false, true) {
		return fmt.Errorf("NRI data source is already running")
	}

	n.ownCtx, n.stop = context.WithCancel(ctx)
	n.eventCh = eventCh
	n.startTime = time.Now()

	n.logger.Info("Starting NRI data source",
		log.String("socket_path", n.config.SocketPath),
		log.String("plugin_name", n.config.PluginName))

	var err error
	n.mask, err = api.ParseEventMask(n.config.Events...)
	if err != nil {
		n.stop()
		n.started.Store(false)
		return fmt.Errorf("failed to parse event mask: %w", err)
	}

	n.logger.Info("Event mask parsed successfully",
		log.String("events", fmt.Sprintf("%v", n.config.Events)),
		log.String("mask", fmt.Sprintf("%d", n.mask)))

	opts := []stub.Option{
		stub.WithPluginName(n.config.PluginName),
		stub.WithOnClose(n.onClose),
	}

	if n.config.PluginIndex != "" {
		opts = append(opts, stub.WithPluginIdx(n.config.PluginIndex))
	}

	n.stub, err = stub.New(n, opts...)
	if err != nil {
		n.stop()
		n.started.Store(false)
		n.logger.Error("Failed to create NRI stub", log.Error(err))
		return fmt.Errorf("failed to create NRI stub: %w", err)
	}

	n.logger.Info("NRI stub created successfully")

	n.wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				n.logger.Error("NRI run panic", log.Any("recover", r))
			}
		}()
		defer n.wg.Done()
		n.runNRI()
	}()

	n.readyOnce.Do(func() { close(n.readyCh) })

	n.logger.Info("NRI data source started successfully")
	return nil
}

func (n *NRIDataSource) Stop() error {
	if !n.started.CompareAndSwap(true, false) {
		return nil
	}

	n.logger.Info("Stopping NRI data source")

	n.stop()

	done := make(chan struct{})
	go func() {
		n.wg.Wait()
		close(done)
	}()

	stopTimeout := n.config.StopTimeout
	if stopTimeout <= 0 {
		stopTimeout = 5
	}
	select {
	case <-done:
		n.logger.Info("NRI data source stopped gracefully")
	case <-time.After(time.Duration(stopTimeout) * time.Second):
		n.logger.Warn("NRI data source stop timeout, forcing exit")
	}

	if n.stub != nil {
		n.stub = nil
	}

	n.logger.Info("NRI data source stopped")
	return nil
}

func (n *NRIDataSource) ParseConfig(config map[string]interface{}) error {
	if socketPath, ok := config["socket_path"].(string); ok {
		n.config.SocketPath = socketPath
	}
	if pluginName, ok := config["plugin_name"].(string); ok {
		n.config.PluginName = pluginName
	}
	if pluginIndex, ok := config["plugin_index"].(string); ok {
		n.config.PluginIndex = pluginIndex
	}
	if events, ok := config["events"].([]string); ok {
		n.config.Events = events
	}
	if bufferSize, ok := config["buffer_size"].(int); ok {
		n.config.BufferSize = bufferSize
	}
	if v, ok := config["stop_timeout"].(int64); ok {
		n.config.StopTimeout = int(v)
	}

	return n.config.Validate()
}

func (n *NRIDataSource) Health() error {
	if !n.started.Load() {
		return fmt.Errorf("NRI data source is not running")
	}

	if n.stub == nil {
		return fmt.Errorf("NRI stub is not initialized")
	}

	return nil
}

func (n *NRIDataSource) GetStats() *DataSourceStats {
	return &DataSourceStats{
		EventsReceived: 0,
		EventsSent:     0,
		EventsDropped:  0,
		LastEventTime:  time.Time{},
		Connected:      n.started.Load(),
		Uptime:         time.Since(n.startTime),
	}
}

func (n *NRIDataSource) Ready() <-chan struct{} {
	return n.readyCh
}

func (n *NRIDataSource) SetEventChannel(ch chan<- *common.Event) {
	n.eventCh = ch
}

func (n *NRIDataSource) runNRI() {
	n.logger.Info("NRI plugin running")

	err := n.stub.Run(n.ownCtx)
	if err != nil {
		n.logger.Error("NRI plugin exited with error", log.Error(err))
	} else {
		n.logger.Info("NRI plugin exited normally")
	}
}

func (n *NRIDataSource) onClose() {
	n.logger.Info("NRI plugin connection closed")
}

func (n *NRIDataSource) Configure(_ context.Context, config, runtime, version string) (stub.EventMask, error) {
	n.logger.Info("NRI plugin configured",
		log.String("config", config),
		log.String("runtime", runtime),
		log.String("version", version))

	if config != "" {
		n.logger.Info("Received dynamic configuration", log.String("config", config))
	}

	n.logger.Info("Returning event mask",
		log.String("events", fmt.Sprintf("%v", n.config.Events)),
		log.String("mask_value", fmt.Sprintf("%d", n.mask)))

	return n.mask, nil
}

func (n *NRIDataSource) Synchronize(_ context.Context, pods []*api.PodSandbox, containers []*api.Container) ([]*api.ContainerUpdate, error) {
	n.logger.Info("NRI synchronize",
		log.Int("pods", len(pods)),
		log.Int("containers", len(containers)))

	for _, container := range containers {
		event := n.createContainerEvent("ContainerExisting", nil, container)
		if event != nil && n.eventCh != nil {
			select {
			case n.eventCh <- event:
			case <-n.ownCtx.Done():
				return nil, n.ownCtx.Err()
			}
		}
	}

	return nil, nil
}

func (n *NRIDataSource) Shutdown() {
	n.logger.Info("NRI plugin shutdown")
}

func (n *NRIDataSource) RunPodSandbox(_ context.Context, pod *api.PodSandbox) error {
	n.logger.Info("RunPodSandbox event received",
		log.String("pod_uid", pod.Uid),
		log.String("pod_name", pod.Name))

	event := n.createPodEvent("RunPodSandbox", pod)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
			n.logger.Info("RunPodSandbox event sent to EventBus")
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	} else {
		n.logger.Warn("Failed to send RunPodSandbox event",
			log.String("event_nil", fmt.Sprintf("%v", event == nil)),
			log.String("eventCh_nil", fmt.Sprintf("%v", n.eventCh == nil)))
	}
	return nil
}

func (n *NRIDataSource) StopPodSandbox(_ context.Context, pod *api.PodSandbox) error {
	n.logger.Info("StopPodSandbox event received",
		log.String("pod_uid", pod.Uid),
		log.String("pod_name", pod.Name))

	event := n.createPodEvent("StopPodSandbox", pod)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
			n.logger.Info("StopPodSandbox event sent to EventBus")
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	} else {
		n.logger.Warn("Failed to send StopPodSandbox event",
			log.String("event_nil", fmt.Sprintf("%v", event == nil)),
			log.String("eventCh_nil", fmt.Sprintf("%v", n.eventCh == nil)))
	}
	return nil
}

func (n *NRIDataSource) RemovePodSandbox(_ context.Context, pod *api.PodSandbox) error {
	event := n.createPodEvent("PodRemove", pod)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	}
	return nil
}

func (n *NRIDataSource) CreateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) (*api.ContainerAdjustment, []*api.ContainerUpdate, error) {
	event := n.createContainerEvent("ContainerCreate", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
		case <-n.ownCtx.Done():
			return nil, nil, n.ownCtx.Err()
		}
	}
	return nil, nil, nil
}

func (n *NRIDataSource) PostCreateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) error {
	event := n.createContainerEvent("ContainerPostCreate", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	}
	return nil
}

func (n *NRIDataSource) StartContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) error {
	n.logger.Info("StartContainer event received",
		log.String("pod_uid", pod.Uid),
		log.String("container_id", container.Id))

	event := n.createContainerEvent("StartContainer", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
			n.logger.Info("StartContainer event sent to EventBus")
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	} else {
		n.logger.Warn("Failed to send StartContainer event",
			log.String("event_nil", fmt.Sprintf("%v", event == nil)),
			log.String("eventCh_nil", fmt.Sprintf("%v", n.eventCh == nil)))
	}
	return nil
}

func (n *NRIDataSource) PostStartContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) error {
	event := n.createContainerEvent("ContainerPostStart", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	}
	return nil
}

func (n *NRIDataSource) UpdateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container, resources *api.LinuxResources) ([]*api.ContainerUpdate, error) {
	event := n.createContainerEvent("ContainerUpdate", pod, container)
	if event != nil && n.eventCh != nil {
		if resources != nil {
			if podPayload, ok := event.TypedPayload.(*nutsapi.Event_Pod); ok && podPayload.Pod != nil {
				podPayload.Pod.Extensions["resources_json"] = fmt.Sprintf("%+v", resources)
			}
		}

		select {
		case n.eventCh <- event:
		case <-n.ownCtx.Done():
			return nil, n.ownCtx.Err()
		}
	}
	return nil, nil
}

func (n *NRIDataSource) PostUpdateContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) error {
	event := n.createContainerEvent("ContainerPostUpdate", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	}
	return nil
}

func (n *NRIDataSource) StopContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) ([]*api.ContainerUpdate, error) {
	n.logger.Info("StopContainer event received",
		log.String("pod_uid", pod.Uid),
		log.String("container_id", container.Id))

	event := n.createContainerEvent("StopContainer", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
			n.logger.Info("StopContainer event sent to EventBus")
		case <-n.ownCtx.Done():
			return nil, n.ownCtx.Err()
		}
	} else {
		n.logger.Warn("Failed to send StopContainer event",
			log.String("event_nil", fmt.Sprintf("%v", event == nil)),
			log.String("eventCh_nil", fmt.Sprintf("%v", n.eventCh == nil)))
	}
	return nil, nil
}

func (n *NRIDataSource) RemoveContainer(_ context.Context, pod *api.PodSandbox, container *api.Container) error {
	n.logger.Info("RemoveContainer event received",
		log.String("pod_uid", pod.Uid),
		log.String("container_id", container.Id))

	event := n.createContainerEvent("RemoveContainer", pod, container)
	if event != nil && n.eventCh != nil {
		select {
		case n.eventCh <- event:
			n.logger.Info("RemoveContainer event sent to EventBus")
		case <-n.ownCtx.Done():
			return n.ownCtx.Err()
		}
	} else {
		n.logger.Warn("Failed to send RemoveContainer event",
			log.String("event_nil", fmt.Sprintf("%v", event == nil)),
			log.String("eventCh_nil", fmt.Sprintf("%v", n.eventCh == nil)))
	}
	return nil
}

func (n *NRIDataSource) createPodEvent(eventType string, pod *api.PodSandbox) *common.Event {
	if pod == nil {
		return nil
	}

	baseCtx := n.ownCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}
	traceID := common.GenerateTraceID(baseCtx)
	ctx := common.ContextWithTraceID(baseCtx, traceID)
	event := &common.Event{
		ID:        fmt.Sprintf("pod-%s-%d", pod.Uid, time.Now().UnixNano()),
		Type:      eventType,
		Topic:     eventType,
		Timestamp: time.Now(),
		Source:    "nri",
		TraceID:   traceID,
		Ctx:       ctx,
		TypedPayload: &nutsapi.Event_Pod{
			Pod: &nutsapi.PodEventPayload{
				PodUid:       pod.Uid,
				PodName:      pod.Name,
				PodNamespace: pod.Namespace,
				PodId:        pod.Id,
				Labels:       pod.Labels,
				Extensions: map[string]string{
					"runtime_handler": pod.RuntimeHandler,
				},
			},
		},
	}

	return event
}

func (n *NRIDataSource) createContainerEvent(eventType string, pod *api.PodSandbox, container *api.Container) *common.Event {
	if container == nil {
		return nil
	}

	baseCtx := n.ownCtx
	if baseCtx == nil {
		baseCtx = context.Background()
	}

	podPayload := &nutsapi.PodEventPayload{
		Extensions: map[string]string{
			"container_id":    container.Id,
			"container_name":  container.Name,
			"container_state": container.State.String(),
			"pod_sandbox_id":  container.PodSandboxId,
		},
	}

	if pod != nil {
		podPayload.PodUid = pod.Uid
		podPayload.PodName = pod.Name
		podPayload.PodNamespace = pod.Namespace
		podPayload.Labels = pod.Labels
	}

	traceID := common.GenerateTraceID(baseCtx)
	ctx := common.ContextWithTraceID(baseCtx, traceID)
	event := &common.Event{
		ID:        fmt.Sprintf("container-%s-%d", container.Id, time.Now().UnixNano()),
		Type:      eventType,
		Topic:     eventType,
		Timestamp: time.Now(),
		Source:    "nri",
		TraceID:   traceID,
		Ctx:       ctx,
		TypedPayload: &nutsapi.Event_Pod{
			Pod: podPayload,
		},
	}

	return event
}
