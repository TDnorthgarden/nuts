package eventbus

import (
	"testing"
	"time"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
)

// =============================================================================
// Helper functions
// =============================================================================

func makePodEvent() *common.Event {
	event := common.NewEvent("PodEvent", "pod.topic", "nri")
	event.TypedPayload = &api.Event_Pod{
		Pod: &api.PodEventPayload{
			PodName:      "test-pod",
			PodNamespace: "default",
			PodUid:       "uid-123",
			PodId:        "pod-123",
			Namespace:    "default",
			Labels:       map[string]string{"app": "test", "env": "dev"},
			Extensions:   map[string]string{"custom_key": "custom_value"},
		},
	}
	return event
}

func makeTaskEvent() *common.Event {
	event := common.NewEvent("TaskStateChanged", "task.state_changed_pending", "statemachine-engine")
	event.TypedPayload = &api.Event_Task{
		Task: &api.TaskEventPayload{
			TaskId:           "task-001",
			OldState:         "",
			NewState:         "pending",
			CurrentState:     "pending",
			TargetState:      "validating",
				Timestamp:        time.Now().Unix(),
			PolicyId:         "policy-001",
			TriggerEventType: "ContainerStart",
			TriggerEventId:   "evt-001",
			Extensions:       map[string]string{"custom_meta": "meta_value"},
		},
	}
	return event
}

func makePolicyEvent() *common.Event {
	event := common.NewEvent("PolicyMatched", "policy.matched", "policy-engine")
	event.TypedPayload = &api.Event_Policy{
		Policy: &api.PolicyEventPayload{
			PolicyId:           "policy-001",
			TriggerEventType:   "ContainerStart",
			TriggerEventSource: "nri",
			TriggerEventId:     "evt-001",
			Timestamp:          time.Now().Unix(),
			Extensions:         map[string]string{"action": "log", "timeout": "300"},
		},
	}
	return event
}

func makeComponentEvent() *common.Event {
	event := common.NewEvent("ComponentHeartbeat", "component.heartbeat", "validating-component")
	event.TypedPayload = &api.Event_Component{
		Component: &api.ComponentEventPayload{
			TaskId:        "task-001",
			CurrentState:  "validating",
			TargetState:   "running",
			ComponentName: "validating-component",
			Success:       true,
			Message:       "validation passed",
		},
	}
	return event
}

// =============================================================================
// Unit Tests - TypedPayload path (Pod)
// =============================================================================

func TestProtobufSerializer_Serialize_Pod(t *testing.T) {
	s := NewProtobufSerializer()
	event := makePodEvent()

	data, err := s.Serialize(event)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("Serialize() returned empty data")
	}
}

func TestProtobufSerializer_RoundTrip_Pod(t *testing.T) {
	s := NewProtobufSerializer()
	original := makePodEvent()

	data, err := s.Serialize(original)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}

	// Verify TypedPayload is preserved
	if restored.TypedPayload == nil {
		t.Fatal("TypedPayload is nil after deserialization")
	}

	pod, ok := restored.TypedPayload.(*api.Event_Pod)
	if !ok {
		t.Fatalf("TypedPayload is not *api.Event_Pod, got %T", restored.TypedPayload)
	}

	if pod.Pod.PodName != "test-pod" {
		t.Errorf("PodName mismatch: got %q", pod.Pod.PodName)
	}
	if pod.Pod.PodNamespace != "default" {
		t.Errorf("PodNamespace mismatch: got %q", pod.Pod.PodNamespace)
	}
	if pod.Pod.Labels["app"] != "test" {
		t.Errorf("Labels[app] mismatch: got %q", pod.Pod.Labels["app"])
	}
	if pod.Pod.Extensions["custom_key"] != "custom_value" {
		t.Errorf("Extensions[custom_key] mismatch: got %q", pod.Pod.Extensions["custom_key"])
	}
}

// =============================================================================
// Unit Tests - TypedPayload path (Task)
// =============================================================================

func TestProtobufSerializer_RoundTrip_Task(t *testing.T) {
	s := NewProtobufSerializer()
	original := makeTaskEvent()

	data, err := s.Serialize(original)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}

	task, ok := restored.TypedPayload.(*api.Event_Task)
	if !ok {
		t.Fatalf("TypedPayload is not *api.Event_Task, got %T", restored.TypedPayload)
	}

	if task.Task.TaskId != "task-001" {
		t.Errorf("TaskId mismatch: got %q", task.Task.TaskId)
	}
	if task.Task.NewState != "pending" {
		t.Errorf("NewState mismatch: got %q", task.Task.NewState)
	}
	if task.Task.Extensions["custom_meta"] != "meta_value" {
		t.Errorf("Extensions[custom_meta] mismatch: got %q", task.Task.Extensions["custom_meta"])
	}
}

// =============================================================================
// Unit Tests - TypedPayload path (Policy)
// =============================================================================

func TestProtobufSerializer_RoundTrip_Policy(t *testing.T) {
	s := NewProtobufSerializer()
	original := makePolicyEvent()

	data, err := s.Serialize(original)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}

	policy, ok := restored.TypedPayload.(*api.Event_Policy)
	if !ok {
		t.Fatalf("TypedPayload is not *api.Event_Policy, got %T", restored.TypedPayload)
	}

	if policy.Policy.PolicyId != "policy-001" {
		t.Errorf("PolicyId mismatch: got %q", policy.Policy.PolicyId)
	}
	if policy.Policy.TriggerEventType != "ContainerStart" {
		t.Errorf("TriggerEventType mismatch: got %q", policy.Policy.TriggerEventType)
	}
	if policy.Policy.Extensions["action"] != "log" {
		t.Errorf("Extensions[action] mismatch: got %q", policy.Policy.Extensions["action"])
	}
}

// =============================================================================
// Unit Tests - TypedPayload path (Component)
// =============================================================================

func TestProtobufSerializer_RoundTrip_Component(t *testing.T) {
	s := NewProtobufSerializer()
	original := makeComponentEvent()

	data, err := s.Serialize(original)
	if err != nil {
		t.Fatalf("Serialize() error = %v", err)
	}

	restored, err := s.Deserialize(data)
	if err != nil {
		t.Fatalf("Deserialize() error = %v", err)
	}

	comp, ok := restored.TypedPayload.(*api.Event_Component)
	if !ok {
		t.Fatalf("TypedPayload is not *api.Event_Component, got %T", restored.TypedPayload)
	}

	if comp.Component.ComponentName != "validating-component" {
		t.Errorf("ComponentName mismatch: got %q", comp.Component.ComponentName)
	}
	if !comp.Component.Success {
		t.Error("Success should be true")
	}
	if comp.Component.Message != "validation passed" {
		t.Errorf("Message mismatch: got %q", comp.Component.Message)
	}
}

// =============================================================================
// Unit Tests - Edge cases
// =============================================================================

func TestProtobufSerializer_Serialize_NilPayload(t *testing.T) {
	s := NewProtobufSerializer()
	event := common.NewEvent("EmptyEvent", "empty.topic", "test")
	// No TypedPayload set

	_, err := s.Serialize(event)
	if err == nil {
		t.Error("Serialize() should return error for event without TypedPayload")
	}
}

func TestProtobufSerializer_Deserialize_InvalidData(t *testing.T) {
	s := NewProtobufSerializer()

	_, err := s.Deserialize([]byte("not valid protobuf"))
	if err == nil {
		t.Error("Deserialize() should return error for invalid data")
	}
}

func TestProtobufSerializer_Deserialize_EmptyData(t *testing.T) {
	s := NewProtobufSerializer()

	// proto.Unmarshal with empty bytes returns a default-initialized message (valid protobuf behavior)
	event, err := s.Deserialize([]byte{})
	if err != nil {
		t.Fatalf("Deserialize() unexpected error: %v", err)
	}
	if event == nil {
		t.Error("Deserialize() returned nil event")
	}
	// All fields should be zero values
	if event.ID != "" {
		t.Errorf("ID should be empty, got %q", event.ID)
	}
}

func TestProtobufSerializer_ContentType(t *testing.T) {
	s := NewProtobufSerializer()
	if ct := s.ContentType(); ct != "application/protobuf" {
		t.Errorf("ContentType() = %q, want %q", ct, "application/protobuf")
	}
}

// =============================================================================
// Benchmarks - ProtobufSerializer
// =============================================================================

func BenchmarkProtobufSerializer_Serialize_Pod(b *testing.B) {
	s := NewProtobufSerializer()
	event := makePodEvent()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Serialize(event)
	}
}

func BenchmarkProtobufSerializer_Deserialize_Pod(b *testing.B) {
	s := NewProtobufSerializer()
	event := makePodEvent()
	data, _ := s.Serialize(event)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Deserialize(data)
	}
}

func BenchmarkProtobufSerializer_Serialize_Task(b *testing.B) {
	s := NewProtobufSerializer()
	event := makeTaskEvent()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Serialize(event)
	}
}

func BenchmarkProtobufSerializer_Deserialize_Task(b *testing.B) {
	s := NewProtobufSerializer()
	event := makeTaskEvent()
	data, _ := s.Serialize(event)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Deserialize(data)
	}
}

func BenchmarkProtobufSerializer_Serialize_Policy(b *testing.B) {
	s := NewProtobufSerializer()
	event := makePolicyEvent()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Serialize(event)
	}
}

func BenchmarkProtobufSerializer_Deserialize_Policy(b *testing.B) {
	s := NewProtobufSerializer()
	event := makePolicyEvent()
	data, _ := s.Serialize(event)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Deserialize(data)
	}
}

func BenchmarkProtobufSerializer_Serialize_Component(b *testing.B) {
	s := NewProtobufSerializer()
	event := makeComponentEvent()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Serialize(event)
	}
}

func BenchmarkProtobufSerializer_Deserialize_Component(b *testing.B) {
	s := NewProtobufSerializer()
	event := makeComponentEvent()
	data, _ := s.Serialize(event)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.Deserialize(data)
	}
}
