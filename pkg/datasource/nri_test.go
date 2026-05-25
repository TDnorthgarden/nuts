package datasource

import (
	"context"
	"testing"
	"time"

	"github.com/containerd/nri/pkg/api"
	nutsapi "github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
)

func TestNewNRIDataSource(t *testing.T) {
	ds, err := NewNRIDataSource(nil)
	if err != nil {
		t.Fatalf("NewNRIDataSource(nil) failed: %v", err)
	}
	if ds == nil {
		t.Fatal("NewNRIDataSource(nil) returned nil")
	}
	if ds.config.SocketPath != "/var/run/nri.sock" {
		t.Errorf("expected default socket path, got %s", ds.config.SocketPath)
	}
	if ds.config.PluginName != "01-nuts" {
		t.Errorf("expected default plugin name, got %s", ds.config.PluginName)
	}

	cfg := &NRIConfig{
		SocketPath:  "/tmp/test-nri.sock",
		PluginName:  "test-plugin",
		PluginIndex: "99",
		Events:      []string{"RunPodSandbox", "StopPodSandbox"},
		BufferSize:  500,
	}
	ds2, err := NewNRIDataSource(cfg)
	if err != nil {
		t.Fatalf("NewNRIDataSource(cfg) failed: %v", err)
	}
	if ds2.config.SocketPath != "/tmp/test-nri.sock" {
		t.Errorf("expected /tmp/test-nri.sock, got %s", ds2.config.SocketPath)
	}
}

func TestNRIConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *NRIConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &NRIConfig{
				SocketPath: "/var/run/nri.sock",
				PluginName: "test",
				Events:     []string{"RunPodSandbox"},
				BufferSize: 1000,
			},
			wantErr: false,
		},
		{
			name: "empty socket path",
			config: &NRIConfig{
				SocketPath: "",
				PluginName: "test",
				Events:     []string{"RunPodSandbox"},
			},
			wantErr: true,
		},
		{
			name: "empty plugin name",
			config: &NRIConfig{
				SocketPath: "/var/run/nri.sock",
				PluginName: "",
				Events:     []string{"RunPodSandbox"},
			},
			wantErr: true,
		},
		{
			name: "empty events",
			config: &NRIConfig{
				SocketPath: "/var/run/nri.sock",
				PluginName: "test",
				Events:     []string{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestNRIParseConfig(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	config := map[string]interface{}{
		"socket_path":  "/custom/path/nri.sock",
		"plugin_name":  "custom-plugin",
		"plugin_index": "50",
		"events":       []string{"RunPodSandbox", "StopPodSandbox"},
		"buffer_size":  2000,
	}

	err := ds.ParseConfig(config)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if ds.config.SocketPath != "/custom/path/nri.sock" {
		t.Errorf("expected /custom/path/nri.sock, got %s", ds.config.SocketPath)
	}
	if ds.config.PluginName != "custom-plugin" {
		t.Errorf("expected custom-plugin, got %s", ds.config.PluginName)
	}
	if ds.config.PluginIndex != "50" {
		t.Errorf("expected 50, got %s", ds.config.PluginIndex)
	}
	if len(ds.config.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(ds.config.Events))
	}
	if ds.config.BufferSize != 2000 {
		t.Errorf("expected 2000, got %d", ds.config.BufferSize)
	}
}

func TestNRIHealth(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	err := ds.Health()
	if err == nil {
		t.Error("Health() should return error when not running")
	}

	ds.started.Store(true)
	err = ds.Health()
	if err == nil {
		t.Error("Health() should return error when stub is nil")
	}
	ds.started.Store(false)
}

func TestNRIGetStats(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)
	ds.startTime = time.Now()

	stats := ds.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}
	if stats.Connected != false {
		t.Error("expected Connected=false when not running")
	}
}

func TestNRIReady(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	readyCh := ds.Ready()

	select {
	case <-readyCh:
		t.Error("Ready should not be signaled when not running")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestNRIDataSourceLifecycle(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)

	err := ds.Start(ctx, eventCh)
	if err != nil {
		t.Logf("Start failed as expected (no NRI running): %v", err)
	} else {
		defer ds.Stop()
	}

	err = ds.Start(ctx, eventCh)
	if err == nil {
		t.Error("second Start should return error (already running)")
	}

	err = ds.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}

func TestNRIDataSourceInterface(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	var _ DataSource = ds

	err := ds.Stop()
	if err != nil {
		t.Errorf("Stop() on non-running datasource should not error: %v", err)
	}
}

func TestCreateNRIDataSource(t *testing.T) {
	cfg := &NRIConfig{
		SocketPath: "/var/run/nri.sock",
		PluginName: "test",
		Events:     []string{"RunPodSandbox"},
		BufferSize: 1000,
	}

	ds, err := CreateNRIDataSource(cfg)
	if err != nil {
		t.Fatalf("CreateNRIDataSource failed: %v", err)
	}
	if ds == nil {
		t.Fatal("CreateNRIDataSource returned nil")
	}

	_, err = CreateNRIDataSource(nil)
	if err == nil {
		t.Error("CreateNRIDataSource(nil) should return error")
	}
}

func TestCreatePodEvent(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	event := ds.createPodEvent("RunPodSandbox", nil)
	if event != nil {
		t.Error("createPodEvent with nil pod should return nil")
	}

	pod := &api.PodSandbox{
		Uid:            "pod-uid-123",
		Name:           "test-pod",
		Namespace:      "default",
		Id:             "sandbox-id-456",
		Labels:         map[string]string{"app": "test"},
		RuntimeHandler: "runc",
	}

	event = ds.createPodEvent("RunPodSandbox", pod)
	if event == nil {
		t.Fatal("createPodEvent returned nil")
	}
	if event.Type != "RunPodSandbox" {
		t.Errorf("expected RunPodSandbox, got %s", event.Type)
	}
	if event.Source != "nri" {
		t.Errorf("expected nri, got %s", event.Source)
	}

	payload, ok := event.TypedPayload.(*nutsapi.Event_Pod)
	if !ok {
		t.Fatal("TypedPayload is not *Event_Pod")
	}
	if payload.Pod.PodUid != "pod-uid-123" {
		t.Errorf("expected pod uid pod-uid-123, got %s", payload.Pod.PodUid)
	}
	if payload.Pod.PodName != "test-pod" {
		t.Errorf("expected pod name test-pod, got %s", payload.Pod.PodName)
	}
}

func TestCreateContainerEvent(t *testing.T) {
	ds, _ := NewNRIDataSource(nil)

	event := ds.createContainerEvent("ContainerStart", nil, nil)
	if event != nil {
		t.Error("createContainerEvent with nil container should return nil")
	}

	container := &api.Container{
		Id:           "container-id-789",
		Name:         "test-container",
		State:        api.ContainerState_CONTAINER_RUNNING,
		PodSandboxId: "sandbox-id-456",
	}

	event = ds.createContainerEvent("ContainerStart", nil, container)
	if event == nil {
		t.Fatal("createContainerEvent returned nil")
	}
	if event.Type != "ContainerStart" {
		t.Errorf("expected ContainerStart, got %s", event.Type)
	}
	if event.Source != "nri" {
		t.Errorf("expected nri, got %s", event.Source)
	}

	payload, ok := event.TypedPayload.(*nutsapi.Event_Pod)
	if !ok {
		t.Fatal("TypedPayload is not *Event_Pod")
	}
	if payload.Pod.Extensions["container_id"] != "container-id-789" {
		t.Errorf("expected container_id container-id-789, got %s", payload.Pod.Extensions["container_id"])
	}

	pod := &api.PodSandbox{
		Uid:       "pod-uid-123",
		Name:      "test-pod",
		Namespace: "default",
		Labels:    map[string]string{"app": "test"},
	}

	event = ds.createContainerEvent("ContainerStart", pod, container)
	if event == nil {
		t.Fatal("createContainerEvent with pod returned nil")
	}

	payload = event.TypedPayload.(*nutsapi.Event_Pod)
	if payload.Pod.PodUid != "pod-uid-123" {
		t.Errorf("expected pod uid pod-uid-123, got %s", payload.Pod.PodUid)
	}
	if payload.Pod.PodName != "test-pod" {
		t.Errorf("expected pod name test-pod, got %s", payload.Pod.PodName)
	}
	if payload.Pod.PodNamespace != "default" {
		t.Errorf("expected namespace default, got %s", payload.Pod.PodNamespace)
	}
}
