package policy

import (
	"context"
	"testing"

	"github.com/sig-cloudnative/nuts/api"
	"github.com/sig-cloudnative/nuts/pkg/common"
)

func TestPolicyEngine_Integration(t *testing.T) {
	// 创建策略存储
	store := NewMemoryPolicyStore()

	// 创建CEL引擎
	celEngine, err := NewCEngine()
	if err != nil {
		t.Fatalf("Create CEL engine failed: %v", err)
	}

	// 创建策略管理器
	manager := NewDefaultPolicyManager(store)
	manager.RegisterEngine(celEngine)

	// 创建策略引擎
	engine := NewDefaultPolicyEngine(manager, store)

	// 启动引擎
	ctx := context.Background()
	if err := engine.Start(ctx); err != nil {
		t.Fatalf("Start engine failed: %v", err)
	}
	defer engine.Stop()

	// 创建测试策略
	policy := &Policy{
		ID:          "test-policy-1",
		Description: "Test policy for integration",
		Enabled:     true,
		DSL:         `event.type == "ContainerStart" && event.payload.namespace == "production"`,
		DSLEngine:   "cel",
	}

	// 添加策略
	if err := manager.AddPolicy(policy); err != nil {
		t.Fatalf("Add policy failed: %v", err)
	}

	// 创建测试事件
	event := common.NewEvent("ContainerStart", "container.event", "test")
	event.TypedPayload = &api.Event_Pod{
		Pod: &api.PodEventPayload{
			PodName:      "test-pod",
			PodNamespace: "production",
			Namespace:    "production",
			Extensions:   map[string]string{},
		},
	}

	// 匹配策略
	matches, err := engine.Match(context.Background(), event)
	if err != nil {
		t.Fatalf("Match failed: %v", err)
	}

	// 验证匹配结果
	if len(matches) == 0 {
		t.Error("Expected at least one match")
	}

	found := false
	for _, match := range matches {
		if match.PolicyID == "test-policy-1" && match.Matched {
			found = true
			break
		}
	}

	if !found {
		t.Error("Policy should have matched")
	}

	t.Logf("Matched %d policies", len(matches))
}

func TestPolicyManager_Integration(t *testing.T) {
	// 创建策略存储
	store := NewMemoryPolicyStore()

	// 创建CEL引擎
	celEngine, err := NewCEngine()
	if err != nil {
		t.Fatalf("Create CEL engine failed: %v", err)
	}

	// 创建策略管理器
	manager := NewDefaultPolicyManager(store)
	manager.RegisterEngine(celEngine)

	// 测试添加策略
	policy := &Policy{
		ID:          "test-policy-2",
		Description: "Test policy for manager",
		Enabled:     true,
		DSL:         `event.type == "PodDeleted"`,
		DSLEngine:   "cel",
	}

	if err := manager.AddPolicy(policy); err != nil {
		t.Fatalf("Add policy failed: %v", err)
	}

	// 测试获取策略
	retrieved, err := manager.GetPolicy("test-policy-2")
	if err != nil {
		t.Fatalf("Get policy failed: %v", err)
	}

	if retrieved.ID != policy.ID {
		t.Errorf("Expected id %s, got %s", policy.ID, retrieved.ID)
	}

	// 测试列出策略
	policies, err := manager.ListPolicies()
	if err != nil {
		t.Fatalf("List policies failed: %v", err)
	}

	if len(policies) != 1 {
		t.Errorf("Expected 1 policy, got %d", len(policies))
	}

	// 测试禁用策略
	if err := manager.DisablePolicy("test-policy-2"); err != nil {
		t.Fatalf("Disable policy failed: %v", err)
	}

	retrieved, _ = manager.GetPolicy("test-policy-2")
	if retrieved.Enabled {
		t.Error("Policy should be disabled")
	}

	// 测试启用策略
	if err := manager.EnablePolicy("test-policy-2"); err != nil {
		t.Fatalf("Enable policy failed: %v", err)
	}

	retrieved, _ = manager.GetPolicy("test-policy-2")
	if !retrieved.Enabled {
		t.Error("Policy should be enabled")
	}

	// 测试删除策略
	if err := manager.RemovePolicy("test-policy-2"); err != nil {
		t.Fatalf("Remove policy failed: %v", err)
	}

	_, err = manager.GetPolicy("test-policy-2")
	if err == nil {
		t.Error("Policy should not exist after deletion")
	}
}

func TestCEngine_Integration(t *testing.T) {
	// 创建CEL引擎
	engine, err := NewCEngine()
	if err != nil {
		t.Fatalf("Create CEL engine failed: %v", err)
	}

	// 测试编译
	dsl := `event.type == "ContainerStart" && event.payload.namespace == "production"`
	program, err := engine.Compile(dsl)
	if err != nil {
		t.Fatalf("Compile failed: %v", err)
	}
	defer program.Close()

	// 测试评估
	data := map[string]interface{}{
		"type": "ContainerStart",
		"payload": map[string]interface{}{
			"namespace": "production",
		},
	}

	matched, err := engine.Evaluate(program, data)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if !matched {
		t.Error("Expected match")
	}

	// 测试不匹配
	data2 := map[string]interface{}{
		"type": "ContainerStop",
		"payload": map[string]interface{}{
			"namespace": "production",
		},
	}

	matched, _ = engine.Evaluate(program, data2)
	if matched {
		t.Error("Expected no match")
	}
}

func TestEventAccessor_Integration(t *testing.T) {
	// 创建测试事件
	event := common.NewEvent("ContainerStart", "container.event", "test")
	event.TypedPayload = &api.Event_Pod{
		Pod: &api.PodEventPayload{
			PodName:      "test-pod",
			PodNamespace: "production",
			Namespace:    "production",
			Labels:       map[string]string{"app": "nginx"},
			Extensions:   map[string]string{},
		},
	}

	// 创建访问器
	accessor := NewEventAccessor(event)

	// 测试字段访问
	namespace := accessor.GetString("payload.namespace")
	if namespace != "production" {
		t.Errorf("Expected namespace 'production', got '%s'", namespace)
	}

	podName := accessor.GetString("payload.pod_name")
	if podName != "test-pod" {
		t.Errorf("Expected pod_name 'test-pod', got '%s'", podName)
	}

	// 测试ToMap (nested structure for CEL)
	data := accessor.ToMap()
	if data["type"] != "ContainerStart" {
		t.Error("Event type mismatch")
	}
	if payload, ok := data["payload"].(map[string]interface{}); ok {
		if payload["namespace"] != "production" {
			t.Error("Payload namespace mismatch")
		}
	} else {
		t.Error("ToMap should have nested payload map")
	}

	// 测试Flatten (flat keys without prefix)
	flattened := accessor.Flatten()
	if _, ok := flattened["namespace"]; !ok {
		t.Error("Flattened data should contain namespace")
	}
	if _, ok := flattened["pod_name"]; !ok {
		t.Error("Flattened data should contain pod_name")
	}
}

func TestPolicyStore_Integration(t *testing.T) {
	// 创建策略存储
	store := NewMemoryPolicyStore()

	// 测试创建策略
	policy := &Policy{
		ID:          "test-policy-3",
		Description: "Test policy for store",
		Enabled:     true,
		DSL:         `event.type == "ContainerStart"`,
		DSLEngine:   "cel",
	}

	if err := store.Create(policy); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 测试获取
	retrieved, err := store.Get("test-policy-3")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if retrieved.ID != policy.ID {
		t.Error("Retrieved policy id mismatch")
	}

	// 测试乐观锁更新
	retrieved.Description = "Updated Policy"
	if err := store.Update(retrieved); err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// 测试版本冲突
	retrieved.Version = 0 // 旧版本号
	if err := store.Update(retrieved); err == nil {
		t.Error("Update should fail with version conflict")
	}

	// 测试删除
	if err := store.Delete("test-policy-3"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, err = store.Get("test-policy-3")
	if err == nil {
		t.Error("Policy should not exist after deletion")
	}
}

func TestCachedPolicyStore_Integration(t *testing.T) {
	// 创建底层存储
	baseStore := NewMemoryPolicyStore()

	// 创建带缓存的存储
	cachedStore := NewCachedPolicyStore(baseStore)

	// 添加策略
	policy := &Policy{
		ID:          "test-policy-4",
		Description: "Test policy for cache",
		Enabled:     true,
		DSL:         `event.type == "ContainerStart"`,
		DSLEngine:   "cel",
	}

	if err := cachedStore.Create(policy); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// 第一次获取（缓存未命中）
	_, err := cachedStore.Get("test-policy-4")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 第二次获取（缓存命中）
	_, err = cachedStore.Get("test-policy-4")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	// 检查缓存统计
	stats := cachedStore.GetCacheStats()
	if stats.HitCount == 0 {
		t.Error("Should have cache hits")
	}

	t.Logf("Cache stats: Hits=%d, Misses=%d, Size=%d, HitRate=%.2f%%",
		stats.HitCount, stats.MissCount, stats.Size, stats.HitRate()*100)
}
