package datasource

import (
	"context"
	"testing"
	"time"

	containerdEvents "github.com/containerd/containerd/api/events"
	"github.com/containerd/containerd/events"
	"github.com/containerd/typeurl/v2"
	nutsapi "github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
)

// TestNewContainerdDataSource 测试创建 containerd 数据源
func TestNewContainerdDataSource(t *testing.T) {
	// 测试默认配置
	ds, err := NewContainerdDataSource(nil)
	if err != nil {
		t.Fatalf("NewContainerdDataSource(nil) failed: %v", err)
	}
	if ds == nil {
		t.Fatal("NewContainerdDataSource(nil) returned nil")
	}
	if ds.config.SocketPath != "/run/containerd/containerd.sock" {
		t.Errorf("expected default socket path, got %s", ds.config.SocketPath)
	}
	if ds.config.Namespace != "k8s.io" {
		t.Errorf("expected default namespace k8s.io, got %s", ds.config.Namespace)
	}

	// 测试自定义配置
	cfg := &ContainerdConfig{
		SocketPath: "/tmp/test.sock",
		Namespace:  "test-ns",
		Events:     []string{"TaskCreate", "TaskExit"},
		BufferSize: 500,
	}
	ds2, err := NewContainerdDataSource(cfg)
	if err != nil {
		t.Fatalf("NewContainerdDataSource(cfg) failed: %v", err)
	}
	if ds2.config.SocketPath != "/tmp/test.sock" {
		t.Errorf("expected /tmp/test.sock, got %s", ds2.config.SocketPath)
	}
	if ds2.config.Namespace != "test-ns" {
		t.Errorf("expected test-ns, got %s", ds2.config.Namespace)
	}
}

// TestContainerdConfigValidate 测试配置验证
func TestContainerdConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  *ContainerdConfig
		wantErr bool
	}{
		{
			name: "valid config",
			config: &ContainerdConfig{
				SocketPath: "/run/containerd/containerd.sock",
				Namespace:  "k8s.io",
				Events:     []string{"TaskCreate"},
				BufferSize: 1000,
			},
			wantErr: false,
		},
		{
			name: "empty socket path",
			config: &ContainerdConfig{
				SocketPath: "",
				Namespace:  "k8s.io",
				Events:     []string{"TaskCreate"},
			},
			wantErr: true,
		},
		{
			name: "empty events",
			config: &ContainerdConfig{
				SocketPath: "/run/containerd/containerd.sock",
				Namespace:  "k8s.io",
				Events:     []string{},
			},
			wantErr: true,
		},
		{
			name: "empty namespace defaults to k8s.io",
			config: &ContainerdConfig{
				SocketPath: "/run/containerd/containerd.sock",
				Namespace:  "",
				Events:     []string{"TaskCreate"},
				BufferSize: 1000,
			},
			wantErr: false,
		},
		{
			name: "zero buffer size defaults to 1000",
			config: &ContainerdConfig{
				SocketPath: "/run/containerd/containerd.sock",
				Namespace:  "k8s.io",
				Events:     []string{"TaskCreate"},
				BufferSize: 0,
			},
			wantErr: false,
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

// TestContainerdConfigGetType 测试 GetType/GetName
func TestContainerdConfigGetType(t *testing.T) {
	cfg := &ContainerdConfig{
		SocketPath: "/run/containerd/containerd.sock",
		Namespace:  "k8s.io",
		Events:     []string{"TaskCreate"},
	}

	if cfg.GetType() != "containerd" {
		t.Errorf("GetType() = %s, want containerd", cfg.GetType())
	}
	if cfg.GetName() != "containerd" {
		t.Errorf("GetName() = %s, want containerd", cfg.GetName())
	}
}

// TestBuildFilters 测试过滤器构建
func TestBuildFilters(t *testing.T) {
	cfg := &ContainerdConfig{
		SocketPath: "/run/containerd/containerd.sock",
		Namespace:  "k8s.io",
		Events: []string{
			"TaskCreate",
			"TaskStart",
			"TaskExit",
			"TaskDelete",
			"TaskPause",
			"TaskResume",
			"ContainerCreate",
			"ContainerDelete",
		},
		BufferSize: 1000,
	}

	ds, err := NewContainerdDataSource(cfg)
	if err != nil {
		t.Fatalf("NewContainerdDataSource failed: %v", err)
	}

	filters := ds.buildFilters()

	expectedFilters := map[string]bool{
		`topic=="/tasks/create"`:      false,
		`topic=="/tasks/start"`:       false,
		`topic=="/tasks/exit"`:        false,
		`topic=="/tasks/delete"`:      false,
		`topic=="/tasks/paused"`:      false,
		`topic=="/tasks/resumed"`:     false,
		`topic=="/containers/create"`: false,
		`topic=="/containers/delete"`: false,
	}

	for _, f := range filters {
		if _, ok := expectedFilters[f]; ok {
			expectedFilters[f] = true
		} else {
			t.Errorf("unexpected filter: %s", f)
		}
	}

	for f, found := range expectedFilters {
		if !found {
			t.Errorf("expected filter not found: %s", f)
		}
	}
}

// TestHandleTaskCreate 测试 TaskCreate 事件处理
func TestHandleTaskCreate(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	taskCreate := &containerdEvents.TaskCreate{
		ContainerID: "abc123",
		Bundle:      "/run/containerd/io.containerd.runtime.v2.task/k8s.io/abc123",
		Pid:         12345,
		Checkpoint:  "",
	}

	event := ds.handleTaskCreate(taskCreate)
	if event == nil {
		t.Fatal("handleTaskCreate returned nil")
	}

	if event.Type != "TaskCreate" {
		t.Errorf("expected TaskCreate, got %s", event.Type)
	}
	if event.Source != "containerd" {
		t.Errorf("expected containerd, got %s", event.Source)
	}

	podPayload, ok := event.TypedPayload.(*nutsapi.Event_Pod)
	if !ok {
		t.Fatal("TypedPayload is not *Event_Pod")
	}
	if podPayload.Pod.Extensions["container_id"] != "abc123" {
		t.Errorf("expected container_id abc123, got %s", podPayload.Pod.Extensions["container_id"])
	}
	if podPayload.Pod.Extensions["pid"] != "12345" {
		t.Errorf("expected pid 12345, got %s", podPayload.Pod.Extensions["pid"])
	}
}

// TestHandleTaskStart 测试 TaskStart 事件处理
func TestHandleTaskStart(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	taskStart := &containerdEvents.TaskStart{
		ContainerID: "abc123",
		Pid:         12345,
	}

	event := ds.handleTaskStart(taskStart)
	if event == nil {
		t.Fatal("handleTaskStart returned nil")
	}

	if event.Type != "TaskStart" {
		t.Errorf("expected TaskStart, got %s", event.Type)
	}

	podPayload := event.TypedPayload.(*nutsapi.Event_Pod)
	if podPayload.Pod.Extensions["container_id"] != "abc123" {
		t.Errorf("expected container_id abc123, got %s", podPayload.Pod.Extensions["container_id"])
	}
}

// TestHandleTaskExit 测试 TaskExit 事件处理
func TestHandleTaskExit(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	exitedAt := time.Now()
	taskExit := &containerdEvents.TaskExit{
		ContainerID: "abc123",
		ExitStatus:  0,
		Pid:         12345,
		ExitedAt:    nil, // 测试 nil ExitedAt
	}

	event := ds.handleTaskExit(taskExit)
	if event == nil {
		t.Fatal("handleTaskExit returned nil")
	}

	if event.Type != "TaskExit" {
		t.Errorf("expected TaskExit, got %s", event.Type)
	}

	podPayload := event.TypedPayload.(*nutsapi.Event_Pod)
	if podPayload.Pod.Extensions["exit_status"] != "0" {
		t.Errorf("expected exit_status 0, got %s", podPayload.Pod.Extensions["exit_status"])
	}

	// 测试非零退出码
	taskExit2 := &containerdEvents.TaskExit{
		ContainerID: "def456",
		ExitStatus:  137,
		Pid:         12346,
		ExitedAt:    nil,
	}
	event2 := ds.handleTaskExit(taskExit2)
	podPayload2 := event2.TypedPayload.(*nutsapi.Event_Pod)
	if podPayload2.Pod.Extensions["exit_status"] != "137" {
		t.Errorf("expected exit_status 137, got %s", podPayload2.Pod.Extensions["exit_status"])
	}

	_ = exitedAt // suppress unused warning
}

// TestHandleTaskDelete 测试 TaskDelete 事件处理
func TestHandleTaskDelete(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	taskDelete := &containerdEvents.TaskDelete{
		ContainerID: "abc123",
		Pid:         12345,
		ExitStatus:  0,
	}

	event := ds.handleTaskDelete(taskDelete)
	if event == nil {
		t.Fatal("handleTaskDelete returned nil")
	}

	if event.Type != "TaskDelete" {
		t.Errorf("expected TaskDelete, got %s", event.Type)
	}

	podPayload := event.TypedPayload.(*nutsapi.Event_Pod)
	if podPayload.Pod.Extensions["container_id"] != "abc123" {
		t.Errorf("expected container_id abc123, got %s", podPayload.Pod.Extensions["container_id"])
	}
}

// TestHandleTaskPaused 测试 TaskPaused 事件处理
func TestHandleTaskPaused(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	taskPaused := &containerdEvents.TaskPaused{
		ContainerID: "abc123",
	}

	event := ds.handleTaskPaused(taskPaused)
	if event == nil {
		t.Fatal("handleTaskPaused returned nil")
	}

	if event.Type != "TaskPause" {
		t.Errorf("expected TaskPause, got %s", event.Type)
	}
}

// TestHandleTaskResumed 测试 TaskResumed 事件处理
func TestHandleTaskResumed(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	taskResumed := &containerdEvents.TaskResumed{
		ContainerID: "abc123",
	}

	event := ds.handleTaskResumed(taskResumed)
	if event == nil {
		t.Fatal("handleTaskResumed returned nil")
	}

	if event.Type != "TaskResume" {
		t.Errorf("expected TaskResume, got %s", event.Type)
	}
}

// TestHandleContainerCreate 测试 ContainerCreate 事件处理
func TestHandleContainerCreate(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	containerCreate := &containerdEvents.ContainerCreate{
		ID:    "abc123",
		Image: "docker.io/library/nginx:latest",
		Runtime: &containerdEvents.ContainerCreate_Runtime{
			Name: "io.containerd.runc.v2",
		},
	}

	event := ds.handleContainerCreate(containerCreate)
	if event == nil {
		t.Fatal("handleContainerCreate returned nil")
	}

	if event.Type != "ContainerCreate" {
		t.Errorf("expected ContainerCreate, got %s", event.Type)
	}

	podPayload := event.TypedPayload.(*nutsapi.Event_Pod)
	if podPayload.Pod.Extensions["container_id"] != "abc123" {
		t.Errorf("expected container_id abc123, got %s", podPayload.Pod.Extensions["container_id"])
	}
	if podPayload.Pod.Extensions["image"] != "docker.io/library/nginx:latest" {
		t.Errorf("expected image, got %s", podPayload.Pod.Extensions["image"])
	}
	if podPayload.Pod.Extensions["runtime_name"] != "io.containerd.runc.v2" {
		t.Errorf("expected runtime_name, got %s", podPayload.Pod.Extensions["runtime_name"])
	}
}

// TestHandleContainerDelete 测试 ContainerDelete 事件处理
func TestHandleContainerDelete(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	containerDelete := &containerdEvents.ContainerDelete{
		ID: "abc123",
	}

	event := ds.handleContainerDelete(containerDelete)
	if event == nil {
		t.Fatal("handleContainerDelete returned nil")
	}

	if event.Type != "ContainerDelete" {
		t.Errorf("expected ContainerDelete, got %s", event.Type)
	}
}

// TestHandleEnvelope 测试 Envelope 分发
func TestHandleEnvelope(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	tests := []struct {
		name     string
		topic    string
		event    interface{}
		wantType string
	}{
		{
			name:     "TaskCreate",
			topic:    "/tasks/create",
			event:    &containerdEvents.TaskCreate{ContainerID: "test1", Pid: 100},
			wantType: "TaskCreate",
		},
		{
			name:     "TaskStart",
			topic:    "/tasks/start",
			event:    &containerdEvents.TaskStart{ContainerID: "test1", Pid: 100},
			wantType: "TaskStart",
		},
		{
			name:     "TaskExit",
			topic:    "/tasks/exit",
			event:    &containerdEvents.TaskExit{ContainerID: "test1", ExitStatus: 0, Pid: 100},
			wantType: "TaskExit",
		},
		{
			name:     "TaskDelete",
			topic:    "/tasks/delete",
			event:    &containerdEvents.TaskDelete{ContainerID: "test1", Pid: 100, ExitStatus: 0},
			wantType: "TaskDelete",
		},
		{
			name:     "TaskPaused",
			topic:    "/tasks/paused",
			event:    &containerdEvents.TaskPaused{ContainerID: "test1"},
			wantType: "TaskPause",
		},
		{
			name:     "TaskResumed",
			topic:    "/tasks/resumed",
			event:    &containerdEvents.TaskResumed{ContainerID: "test1"},
			wantType: "TaskResume",
		},
		{
			name:     "ContainerCreate",
			topic:    "/containers/create",
			event:    &containerdEvents.ContainerCreate{ID: "test1", Image: "nginx"},
			wantType: "ContainerCreate",
		},
		{
			name:     "ContainerDelete",
			topic:    "/containers/delete",
			event:    &containerdEvents.ContainerDelete{ID: "test1"},
			wantType: "ContainerDelete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			anyEvent, err := typeurl.MarshalAny(tt.event)
			if err != nil {
				t.Fatalf("typeurl.MarshalAny failed: %v", err)
			}

			envelope := &events.Envelope{
				Topic: tt.topic,
				Event: anyEvent,
			}

			result := ds.handleEnvelope(envelope)
			if result == nil {
				t.Fatal("handleEnvelope returned nil")
			}
			if result.Type != tt.wantType {
				t.Errorf("expected type %s, got %s", tt.wantType, result.Type)
			}
			if result.Source != "containerd" {
				t.Errorf("expected source containerd, got %s", result.Source)
			}
		})
	}
}

// TestHandleEnvelopeNil 测试 nil envelope
func TestHandleEnvelopeNil(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	if event := ds.handleEnvelope(nil); event != nil {
		t.Error("handleEnvelope(nil) should return nil")
	}

	if event := ds.handleEnvelope(&events.Envelope{}); event != nil {
		t.Error("handleEnvelope with nil Event should return nil")
	}
}

// TestHandleEnvelopeUnknownTopic 测试未知 topic
func TestHandleEnvelopeUnknownTopic(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	anyEvent, _ := typeurl.MarshalAny(&containerdEvents.TaskStart{ContainerID: "test"})
	envelope := &events.Envelope{
		Topic: "/unknown/topic",
		Event: anyEvent,
	}

	if event := ds.handleEnvelope(envelope); event != nil {
		t.Error("handleEnvelope with unknown topic should return nil")
	}
}

// TestParseConfig 测试 ParseConfig
func TestContainerdParseConfig(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	config := map[string]interface{}{
		"socket_path": "/custom/path/containerd.sock",
		"namespace":   "custom-ns",
		"events":      []string{"TaskCreate", "TaskExit"},
		"buffer_size": 2000,
	}

	err := ds.ParseConfig(config)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}

	if ds.config.SocketPath != "/custom/path/containerd.sock" {
		t.Errorf("expected /custom/path/containerd.sock, got %s", ds.config.SocketPath)
	}
	if ds.config.Namespace != "custom-ns" {
		t.Errorf("expected custom-ns, got %s", ds.config.Namespace)
	}
	if len(ds.config.Events) != 2 {
		t.Errorf("expected 2 events, got %d", len(ds.config.Events))
	}
	if ds.config.BufferSize != 2000 {
		t.Errorf("expected 2000, got %d", ds.config.BufferSize)
	}
}

// TestHealth 测试健康检查
func TestContainerdHealth(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	// 未启动时应返回错误
	err := ds.Health()
	if err == nil {
		t.Error("Health() should return error when not running")
	}

	// 标记启动后应返回 nil（但需要 containerd 运行）
	ds.started.Store(true)
	err = ds.Health()
	if err == nil {
		t.Error("Health() should return error when client is nil")
	}
	ds.started.Store(false)
}

// TestGetStats 测试统计信息
func TestContainerdGetStats(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)
	ds.startTime = time.Now()

	stats := ds.GetStats()
	if stats == nil {
		t.Fatal("GetStats returned nil")
	}
	if stats.Connected != false {
		t.Error("expected Connected=false when not running")
	}
}

// TestReady 测试就绪信号
func TestContainerdReady(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	// 未启动时，Ready 通道应未关闭（阻塞）
	readyCh := ds.Ready()

	select {
	case <-readyCh:
		t.Error("Ready should not be signaled when not running")
	case <-time.After(100 * time.Millisecond):
		// 预期行为：未启动时阻塞
	}
}

// TestCreateContainerdDataSource 测试工厂函数
func TestCreateContainerdDataSource(t *testing.T) {
	cfg := &ContainerdConfig{
		SocketPath: "/run/containerd/containerd.sock",
		Namespace:  "k8s.io",
		Events:     []string{"TaskCreate"},
		BufferSize: 1000,
	}

	ds, err := CreateContainerdDataSource(cfg)
	if err != nil {
		t.Fatalf("CreateContainerdDataSource failed: %v", err)
	}
	if ds == nil {
		t.Fatal("CreateContainerdDataSource returned nil")
	}

	// 测试无效配置类型
	_, err = CreateContainerdDataSource(nil)
	if err == nil {
		t.Error("CreateContainerdDataSource(nil) should return error")
	}
}

// TestContainerdDataSourceInterface 验证接口实现
func TestContainerdDataSourceInterface(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	// 验证实现了 DataSource 接口
	var _ DataSource = ds

	// 验证 Stop 在未启动时不会 panic
	err := ds.Stop()
	if err != nil {
		t.Errorf("Stop() on non-running datasource should not error: %v", err)
	}
}

// TestContainerdDataSourceLifecycle 测试生命周期
func TestContainerdDataSourceLifecycle(t *testing.T) {
	ds, _ := NewContainerdDataSource(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	eventCh := make(chan *common.Event, 100)

	// 启动数据源（会尝试连接 containerd，预期失败但不应 panic）
	err := ds.Start(ctx, eventCh)
	if err != nil {
		t.Logf("Start failed as expected (no containerd running): %v", err)
	} else {
		// 如果意外成功，确保清理
		defer ds.Stop()
	}

	// 测试重复启动
	err = ds.Start(ctx, eventCh)
	if err == nil {
		t.Error("second Start should return error (already running)")
	}

	// 停止
	err = ds.Stop()
	if err != nil {
		t.Errorf("Stop failed: %v", err)
	}
}
