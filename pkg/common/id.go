package common

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// IDGenerator ID生成器接口
// 所有ID生成器必须实现此接口，支持通过工厂模式注册和切换
type IDGenerator interface {
	// Generate 生成唯一ID字符串
	Generate() string
}

// UUIDGenerator 基于UUID v4的ID生成器
// 内部委托给 GenerateUUID()，使用 github.com/google/uuid
type UUIDGenerator struct{}

// Generate 生成UUID格式的唯一ID
func (g *UUIDGenerator) Generate() string {
	return uuid.New().String()
}

// SnowflakeGenerator 雪花算法ID生成器
// 基于 Twitter Snowflake 算法，生成64位整数ID：
//
//	41 bits: 时间戳（毫秒，自定义epoch）
//	10 bits: 机器ID（0-1023）
//	12 bits: 序列号（0-4095，每毫秒）
type SnowflakeGenerator struct {
	mu        sync.Mutex
	machineID int64
	sequence  int64
	lastTime  int64
}

const (
	snowflakeEpoch = 1700000000000 // 自定义起始时间: 2023-11-14T00:00:00 UTC (ms)
	machineIDBits  = 10
	sequenceBits   = 12
	maxMachineID   = -1 ^ (-1 << machineIDBits) // 1023
	maxSequence    = -1 ^ (-1 << sequenceBits)  // 4095
	timeShift      = machineIDBits + sequenceBits
	machineIDShift = sequenceBits
)

// NewSnowflakeGenerator 创建雪花算法ID生成器
// machineID 必须在 0-1023 范围内
func NewSnowflakeGenerator(machineID int64) (*SnowflakeGenerator, error) {
	if machineID < 0 || machineID > maxMachineID {
		return nil, fmt.Errorf("snowflake machine_id must be between 0 and %d, got %d", maxMachineID, machineID)
	}
	return &SnowflakeGenerator{
		machineID: machineID,
	}, nil
}

// Generate 生成雪花算法唯一ID（返回十进制字符串）
func (s *SnowflakeGenerator) Generate() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UnixMilli()

	if now < s.lastTime {
		// 时钟回拨：等待追上，最多等 10ms，超时则用 lastTime 继续 sequence 递增
		deadline := time.Now().Add(10 * time.Millisecond)
		for now < s.lastTime {
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(time.Microsecond)
			now = time.Now().UnixMilli()
		}
		if now < s.lastTime {
			now = s.lastTime
		}
	}

	if now == s.lastTime {
		s.sequence = (s.sequence + 1) & maxSequence
		if s.sequence == 0 {
			// 当前毫秒序列号耗尽，等待下一毫秒（最多等 10ms）
			deadline := time.Now().Add(10 * time.Millisecond)
			for now <= s.lastTime {
				if time.Now().After(deadline) {
					// 时钟停顿无法推进，主动推进 lastTime 避免 panic
					// ID 中的时间戳会略微超前，但 machineID+sequence 仍保证唯一性
					now = s.lastTime + 1
					break
				}
				time.Sleep(time.Microsecond)
				now = time.Now().UnixMilli()
			}
			s.sequence = 0
		}
	} else {
		s.sequence = 0
	}

	s.lastTime = now

	id := ((now - snowflakeEpoch) << timeShift) |
		(s.machineID << machineIDShift) |
		s.sequence

	return fmt.Sprintf("%d", id)
}

// IDGeneratorFactory ID生成器全局工厂实例
// 使用方式与 eventbus.Factory 一致：通过 Register 注册类型，通过 Create 创建实例
var IDGeneratorFactory = &idGeneratorFactory{
	creators: make(map[string]func(cfg map[string]interface{}) (IDGenerator, error)),
}

type idGeneratorFactory struct {
	mu       sync.RWMutex
	creators map[string]func(cfg map[string]interface{}) (IDGenerator, error)
}

// Register 注册ID生成器类型
func (f *idGeneratorFactory) Register(name string, creator func(cfg map[string]interface{}) (IDGenerator, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[name] = creator
}

// Create 根据配置创建ID生成器实例
// config 必须包含 "type" 字段（"uuid" 或 "snowflake"），默认为 "uuid"
func (f *idGeneratorFactory) Create(config map[string]interface{}) (IDGenerator, error) {
	idType, _ := config["type"].(string)
	if idType == "" {
		idType = "uuid"
	}

	f.mu.RLock()
	creator, ok := f.creators[idType]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown id generator type: %s (supported: uuid, snowflake)", idType)
	}

	return creator(config)
}

// GetSupportedTypes 获取支持的ID生成器类型列表
func (f *idGeneratorFactory) GetSupportedTypes() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()

	types := make([]string, 0, len(f.creators))
	for t := range f.creators {
		types = append(types, t)
	}
	return types
}

// defaultGenerator 包级默认生成器，初始为 UUIDGenerator
// 通过 SetDefaultGenerator() 在应用启动时切换
var defaultGenerator IDGenerator = &UUIDGenerator{}

// SetDefaultGenerator 设置全局默认ID生成器
// 在 core 初始化时根据配置调用，切换为雪花算法等
func SetDefaultGenerator(g IDGenerator) {
	if g != nil {
		defaultGenerator = g
	}
}

// GenerateUUID 生成UUID（便捷函数，向后兼容）
// 内部委托给 defaultGenerator，默认使用 UUID，可通过 SetDefaultGenerator 切换
func GenerateUUID() string {
	return defaultGenerator.Generate()
}

func init() {
	// 注册 UUID 生成器
	IDGeneratorFactory.Register("uuid", func(cfg map[string]interface{}) (IDGenerator, error) {
		return &UUIDGenerator{}, nil
	})

	// 注册雪花算法生成器
	IDGeneratorFactory.Register("snowflake", func(cfg map[string]interface{}) (IDGenerator, error) {
		machineID := int64(1) // 默认机器ID
		if snowflakeMap, ok := cfg["snowflake"].(map[string]interface{}); ok {
			if mid, ok := snowflakeMap["machine_id"].(int64); ok {
				machineID = mid
			} else if mid, ok := snowflakeMap["machine_id"].(float64); ok {
				// TOML 解析整数为 float64
				machineID = int64(mid)
			}
		}
		return NewSnowflakeGenerator(machineID)
	})
}
