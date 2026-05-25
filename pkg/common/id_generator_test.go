package common

import (
	"testing"
)

func TestUUIDGenerator(t *testing.T) {
	g := &UUIDGenerator{}

	// 生成多个ID，验证唯一性
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := g.Generate()
		if id == "" {
			t.Error("UUIDGenerator.Generate() returned empty string")
		}
		if ids[id] {
			t.Errorf("UUIDGenerator.Generate() returned duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestSnowflakeGenerator(t *testing.T) {
	// 测试正常创建
	g, err := NewSnowflakeGenerator(1)
	if err != nil {
		t.Fatalf("NewSnowflakeGenerator(1) failed: %v", err)
	}

	// 生成多个ID，验证唯一性
	ids := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := g.Generate()
		if id == "" {
			t.Error("SnowflakeGenerator.Generate() returned empty string")
		}
		if ids[id] {
			t.Errorf("SnowflakeGenerator.Generate() returned duplicate ID: %s", id)
		}
		ids[id] = true
	}
}

func TestSnowflakeGeneratorInvalidMachineID(t *testing.T) {
	// 负数
	_, err := NewSnowflakeGenerator(-1)
	if err == nil {
		t.Error("NewSnowflakeGenerator(-1) should return error")
	}

	// 超出范围
	_, err = NewSnowflakeGenerator(1024)
	if err == nil {
		t.Error("NewSnowflakeGenerator(1024) should return error")
	}
}

func TestIDGeneratorFactory_UUID(t *testing.T) {
	cfg := map[string]interface{}{
		"type": "uuid",
	}

	g, err := IDGeneratorFactory.Create(cfg)
	if err != nil {
		t.Fatalf("IDGeneratorFactory.Create(uuid) failed: %v", err)
	}

	id := g.Generate()
	if id == "" {
		t.Error("UUID generator returned empty string")
	}
}

func TestIDGeneratorFactory_Snowflake(t *testing.T) {
	cfg := map[string]interface{}{
		"type": "snowflake",
		"snowflake": map[string]interface{}{
			"machine_id": float64(5), // TOML 解析整数为 float64
		},
	}

	g, err := IDGeneratorFactory.Create(cfg)
	if err != nil {
		t.Fatalf("IDGeneratorFactory.Create(snowflake) failed: %v", err)
	}

	id := g.Generate()
	if id == "" {
		t.Error("Snowflake generator returned empty string")
	}
}

func TestIDGeneratorFactory_DefaultType(t *testing.T) {
	// 不指定 type，应默认使用 uuid
	cfg := map[string]interface{}{}

	g, err := IDGeneratorFactory.Create(cfg)
	if err != nil {
		t.Fatalf("IDGeneratorFactory.Create(default) failed: %v", err)
	}

	id := g.Generate()
	if id == "" {
		t.Error("Default generator returned empty string")
	}
}

func TestIDGeneratorFactory_UnknownType(t *testing.T) {
	cfg := map[string]interface{}{
		"type": "unknown",
	}

	_, err := IDGeneratorFactory.Create(cfg)
	if err == nil {
		t.Error("IDGeneratorFactory.Create(unknown) should return error")
	}
}

func TestIDGeneratorFactory_GetSupportedTypes(t *testing.T) {
	types := IDGeneratorFactory.GetSupportedTypes()
	if len(types) < 2 {
		t.Errorf("Expected at least 2 supported types, got %d: %v", len(types), types)
	}

	hasUUID := false
	hasSnowflake := false
	for _, typ := range types {
		if typ == "uuid" {
			hasUUID = true
		}
		if typ == "snowflake" {
			hasSnowflake = true
		}
	}
	if !hasUUID {
		t.Error("Supported types should include 'uuid'")
	}
	if !hasSnowflake {
		t.Error("Supported types should include 'snowflake'")
	}
}

func TestSetDefaultGenerator(t *testing.T) {
	// 保存原始默认生成器
	original := defaultGenerator
	defer func() { defaultGenerator = original }()

	// 切换到雪花算法
	sg, _ := NewSnowflakeGenerator(1)
	SetDefaultGenerator(sg)

	id := GenerateUUID()
	if id == "" {
		t.Error("GenerateUUID() after switching to snowflake returned empty string")
	}

	// 切换回 UUID
	SetDefaultGenerator(&UUIDGenerator{})
	id2 := GenerateUUID()
	if id2 == "" {
		t.Error("GenerateUUID() after switching back to UUID returned empty string")
	}
}

func TestGenerateUUID_BackwardCompatible(t *testing.T) {
	// 确保 GenerateUUID() 仍然可用（向后兼容）
	id := GenerateUUID()
	if id == "" {
		t.Error("GenerateUUID() returned empty string")
	}

	// 生成多个确保唯一性
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateUUID()
		if ids[id] {
			t.Errorf("GenerateUUID() returned duplicate ID: %s", id)
		}
		ids[id] = true
	}
}
