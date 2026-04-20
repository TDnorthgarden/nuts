# 数据库接口设计分析

## 1. 概述

Nuts系统需要支持多种数据库类型，针对不同的数据特性和使用场景选择合适的存储方案。本文档分析各模块的数据库接口设计。

## 2. 数据分类与特性分析

### 2.1 策略数据 (Policy Data)

**数据特性：**
- 结构化数据，关系型
- 数据量相对较小（策略数量有限）
- 需要事务支持（ACID）
- 需要复杂查询（按名称、命名空间、目标等查询）
- 读写频率：中等（创建、更新、删除、查询）

**适用数据库：**
- **SQLite**：适合单机部署，零配置
- **MySQL/PostgreSQL**：适合生产环境，支持高并发
- **LevelDB**：适合嵌入式场景，但不支持复杂查询

**推荐方案：**
- 开发/测试环境：SQLite
- 生产环境：MySQL/PostgreSQL

### 2.2 事件数据 (Event Data)

**数据特性：**
- 时序数据，高写入频率
- 数据量大（持续采集）
- 写多读少（主要是聚合引擎读取）
- 需要按时间范围查询
- 需要按cgroup、policy等维度过滤
- 不需要复杂事务

**适用数据库：**
- **InfluxDB**：专业的时序数据库，写入性能高
- **ClickHouse**：列式存储，适合大规模时序数据
- **TimescaleDB**：PostgreSQL扩展，支持时序特性
- **LevelDB**：适合小规模场景，但查询能力有限

**推荐方案：**
- 小规模/开发环境：LevelDB
- 中等规模：InfluxDB
- 大规模生产环境：ClickHouse

### 2.3 审计数据 (Audit Data)

**数据特性：**
- 结构化数据，关系型
- 数据量中等（基于事件聚合）
- 需要按policy、cgroup查询
- 需要更新操作（聚合过程中更新）
- 读写频率：中等

**适用数据库：**
- **SQLite**：适合单机部署
- **MySQL/PostgreSQL**：适合生产环境

**推荐方案：**
- 开发/测试环境：SQLite
- 生产环境：MySQL/PostgreSQL

### 2.4 诊断数据 (Diagnosis Data)

**数据特性：**
- 结构化数据，关系型
- 数据量小（基于审计数据生成）
- 需要按audit查询
- 需要更新操作
- 读写频率：低

**适用数据库：**
- **SQLite**：适合所有场景
- **MySQL/PostgreSQL**：适合需要集中管理的场景

**推荐方案：**
- 所有环境：SQLite（数据量小，SQLite足够）

## 3. 接口设计

### 3.1 统一存储接口

```go
// pkg/storage/interface.go

// PolicyStore 策略存储接口
type PolicyStore interface {
    Create(policy *Policy) error
    Update(policy *Policy) error
    Delete(id string) error
    Get(id string) (*Policy, error)
    List() ([]*Policy, error)
    Query(query *PolicyQuery) ([]*Policy, error)
}

// EventStore 事件存储接口（时序数据）
type EventStore interface {
    Write(event *Event) error
    WriteBatch(events []*Event) error
    Query(query *EventQuery) ([]*Event, error)
    QueryByTimeRange(start, end time.Time, filters map[string]string) ([]*Event, error)
    Delete(cgroupID string, policyID string) error
}

// AuditStore 审计存储接口
type AuditStore interface {
    Create(audit *Audit) error
    Get(id string) (*Audit, error)
    ListByPolicy(policyID string) ([]*Audit, error)
    ListByCgroup(cgroupID string) ([]*Audit, error)
    Update(audit *Audit) error
}

// DiagnosisStore 诊断结果存储接口
type DiagnosisStore interface {
    Create(diagnosis *Diagnosis) error
    Get(id string) (*Diagnosis, error)
    ListByAudit(auditID string) ([]*Diagnosis, error)
    Update(diagnosis *Diagnosis) error
}
```

### 3.2 存储工厂模式

```go
// pkg/storage/factory.go

package storage

import (
    "fmt"
    "os"
)

// StoreConfig 存储配置
type StoreConfig struct {
    PolicyStoreType   string `yaml:"policy_store_type"`   // sqlite, mysql, postgresql
    EventStoreType    string `yaml:"event_store_type"`    // leveldb, influxdb, clickhouse
    AuditStoreType    string `yaml:"audit_store_type"`    // sqlite, mysql, postgresql
    DiagnosisStoreType string `yaml:"diagnosis_store_type"` // sqlite, mysql, postgresql
    
    // PolicyStore配置
    PolicyDBPath     string `yaml:"policy_db_path"`      // SQLite路径
    PolicyDBHost     string `yaml:"policy_db_host"`      // MySQL/PostgreSQL主机
    PolicyDBPort     int    `yaml:"policy_db_port"`      // MySQL/PostgreSQL端口
    PolicyDBName     string `yaml:"policy_db_name"`      // 数据库名
    PolicyDBUser     string `yaml:"policy_db_user"`      // 用户名
    PolicyDBPassword string `yaml:"policy_db_password"`  // 密码
    
    // EventStore配置
    EventDBPath      string `yaml:"event_db_path"`       // LevelDB路径
    InfluxDBURL      string `yaml:"influxdb_url"`        // InfluxDB URL
    InfluxDBToken    string `yaml:"influxdb_token"`      // InfluxDB Token
    InfluxDBOrg      string `yaml:"influxdb_org"`        // InfluxDB Org
    InfluxDBBucket   string `yaml:"influxdb_bucket"`     // InfluxDB Bucket
    ClickHouseURL    string `yaml:"clickhouse_url"`      // ClickHouse URL
    ClickHouseDB     string `yaml:"clickhouse_db"`       // ClickHouse Database
    
    // AuditStore配置
    AuditDBPath      string `yaml:"audit_db_path"`       // SQLite路径
    AuditDBHost      string `yaml:"audit_db_host"`       // MySQL/PostgreSQL主机
    AuditDBPort      int    `yaml:"audit_db_port"`       // MySQL/PostgreSQL端口
    AuditDBName      string `yaml:"audit_db_name"`       // 数据库名
    AuditDBUser      string `yaml:"audit_db_user"`       // 用户名
    AuditDBPassword  string `yaml:"audit_db_password"`   // 密码
    
    // DiagnosisStore配置
    DiagnosisDBPath  string `yaml:"diagnosis_db_path"`   // SQLite路径
    DiagnosisDBHost  string `yaml:"diagnosis_db_host"`   // MySQL/PostgreSQL主机
    DiagnosisDBPort  int    `yaml:"diagnosis_db_port"`   // MySQL/PostgreSQL端口
    DiagnosisDBName  string `yaml:"diagnosis_db_name"`   // 数据库名
    DiagnosisDBUser  string `yaml:"diagnosis_db_user"`   // 用户名
    DiagnosisDBPassword string `yaml:"diagnosis_db_password"` // 密码
}

// NewStoreFactory 创建存储工厂
func NewStoreFactory(config *StoreConfig) (*StoreFactory, error) {
    return &StoreFactory{config: config}, nil
}

// StoreFactory 存储工厂
type StoreFactory struct {
    config *StoreConfig
}

// CreatePolicyStore 创建策略存储
func (f *StoreFactory) CreatePolicyStore() (PolicyStore, error) {
    switch f.config.PolicyStoreType {
    case "sqlite":
        return NewSQLitePolicyStore(f.config.PolicyDBPath)
    case "mysql":
        return NewMySQLPolicyStore(
            f.config.PolicyDBHost,
            f.config.PolicyDBPort,
            f.config.PolicyDBName,
            f.config.PolicyDBUser,
            f.config.PolicyDBPassword,
        )
    case "postgresql":
        return NewPostgreSQLPolicyStore(
            f.config.PolicyDBHost,
            f.config.PolicyDBPort,
            f.config.PolicyDBName,
            f.config.PolicyDBUser,
            f.config.PolicyDBPassword,
        )
    default:
        return nil, fmt.Errorf("unsupported policy store type: %s", f.config.PolicyStoreType)
    }
}

// CreateEventStore 创建事件存储
func (f *StoreFactory) CreateEventStore() (EventStore, error) {
    switch f.config.EventStoreType {
    case "leveldb":
        return NewLevelDBEventStore(f.config.EventDBPath)
    case "influxdb":
        return NewInfluxDBEventStore(
            f.config.InfluxDBURL,
            f.config.InfluxDBToken,
            f.config.InfluxDBOrg,
            f.config.InfluxDBBucket,
        )
    case "clickhouse":
        return NewClickHouseEventStore(
            f.config.ClickHouseURL,
            f.config.ClickHouseDB,
        )
    default:
        return nil, fmt.Errorf("unsupported event store type: %s", f.config.EventStoreType)
    }
}

// CreateAuditStore 创建审计存储
func (f *StoreFactory) CreateAuditStore() (AuditStore, error) {
    switch f.config.AuditStoreType {
    case "sqlite":
        return NewSQLiteAuditStore(f.config.AuditDBPath)
    case "mysql":
        return NewMySQLAuditStore(
            f.config.AuditDBHost,
            f.config.AuditDBPort,
            f.config.AuditDBName,
            f.config.AuditDBUser,
            f.config.AuditDBPassword,
        )
    case "postgresql":
        return NewPostgreSQLAuditStore(
            f.config.AuditDBHost,
            f.config.AuditDBPort,
            f.config.AuditDBName,
            f.config.AuditDBUser,
            f.config.AuditDBPassword,
        )
    default:
        return nil, fmt.Errorf("unsupported audit store type: %s", f.config.AuditStoreType)
    }
}

// CreateDiagnosisStore 创建诊断存储
func (f *StoreFactory) CreateDiagnosisStore() (DiagnosisStore, error) {
    switch f.config.DiagnosisStoreType {
    case "sqlite":
        return NewSQLiteDiagnosisStore(f.config.DiagnosisDBPath)
    case "mysql":
        return NewMySQLDiagnosisStore(
            f.config.DiagnosisDBHost,
            f.config.DiagnosisDBPort,
            f.config.DiagnosisDBName,
            f.config.DiagnosisDBUser,
            f.config.DiagnosisDBPassword,
        )
    case "postgresql":
        return NewPostgreSQLDiagnosisStore(
            f.config.DiagnosisDBHost,
            f.config.DiagnosisDBPort,
            f.config.DiagnosisDBName,
            f.config.DiagnosisDBUser,
            f.config.DiagnosisDBPassword,
        )
    default:
        return nil, fmt.Errorf("unsupported diagnosis store type: %s", f.config.DiagnosisStoreType)
    }
}

// CreateAllStores 创建所有存储
func (f *StoreFactory) CreateAllStores() (*Stores, error) {
    policyStore, err := f.CreatePolicyStore()
    if err != nil {
        return nil, fmt.Errorf("failed to create policy store: %w", err)
    }
    
    eventStore, err := f.CreateEventStore()
    if err != nil {
        return nil, fmt.Errorf("failed to create event store: %w", err)
    }
    
    auditStore, err := f.CreateAuditStore()
    if err != nil {
        return nil, fmt.Errorf("failed to create audit store: %w", err)
    }
    
    diagnosisStore, err := f.CreateDiagnosisStore()
    if err != nil {
        return nil, fmt.Errorf("failed to create diagnosis store: %w", err)
    }
    
    return &Stores{
        PolicyStore:    policyStore,
        EventStore:     eventStore,
        AuditStore:     auditStore,
        DiagnosisStore: diagnosisStore,
    }, nil
}

// Stores 所有存储的集合
type Stores struct {
    PolicyStore    PolicyStore
    EventStore     EventStore
    AuditStore     AuditStore
    DiagnosisStore DiagnosisStore
}

// Close 关闭所有存储
func (s *Stores) Close() error {
    var errs []error
    
    if closer, ok := s.PolicyStore.(io.Closer); ok {
        if err := closer.Close(); err != nil {
            errs = append(errs, fmt.Errorf("policy store close error: %w", err))
        }
    }
    
    if closer, ok := s.EventStore.(io.Closer); ok {
        if err := closer.Close(); err != nil {
            errs = append(errs, fmt.Errorf("event store close error: %w", err))
        }
    }
    
    if closer, ok := s.AuditStore.(io.Closer); ok {
        if err := closer.Close(); err != nil {
            errs = append(errs, fmt.Errorf("audit store close error: %w", err))
        }
    }
    
    if closer, ok := s.DiagnosisStore.(io.Closer); ok {
        if err := closer.Close(); err != nil {
            errs = append(errs, fmt.Errorf("diagnosis store close error: %w", err))
        }
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("multiple errors occurred: %v", errs)
    }
    
    return nil
}
```

## 4. 具体实现建议

### 4.1 目录结构

```
pkg/storage/
├── interface.go              # 存储接口定义
├── factory.go                # 存储工厂
├── policy/                   # 策略存储实现
│   ├── sqlite.go            # SQLite实现
│   ├── mysql.go             # MySQL实现
│   └── postgresql.go        # PostgreSQL实现
├── event/                    # 事件存储实现
│   ├── leveldb.go           # LevelDB实现
│   ├── influxdb.go          # InfluxDB实现
│   └── clickhouse.go        # ClickHouse实现
├── audit/                    # 审计存储实现
│   ├── sqlite.go            # SQLite实现
│   ├── mysql.go             # MySQL实现
│   └── postgresql.go        # PostgreSQL实现
└── diagnosis/                # 诊断存储实现
    ├── sqlite.go            # SQLite实现
    ├── mysql.go             # MySQL实现
    └── postgresql.go        # PostgreSQL实现
```

### 4.2 实现优先级

**第一阶段（当前）：**
1. 实现SQLite PolicyStore（策略存储）
2. 实现LevelDB EventStore（事件存储）
3. 实现SQLite AuditStore（审计存储）
4. 实现SQLite DiagnosisStore（诊断存储）

**第二阶段：**
1. 实现MySQL/PostgreSQL PolicyStore
2. 实现InfluxDB EventStore

**第三阶段：**
1. 实现ClickHouse EventStore
2. 实现MySQL/PostgreSQL AuditStore
3. 实现MySQL/PostgreSQL DiagnosisStore

### 4.3 配置示例

```yaml
# configs/storage.yaml

# 开发环境配置
storage:
  policy_store_type: "sqlite"
  policy_db_path: "/data/policies.db"
  
  event_store_type: "leveldb"
  event_db_path: "/data/events"
  
  audit_store_type: "sqlite"
  audit_db_path: "/data/audits.db"
  
  diagnosis_store_type: "sqlite"
  diagnosis_db_path: "/data/diagnoses.db"

# 生产环境配置
storage:
  policy_store_type: "postgresql"
  policy_db_host: "postgres-service"
  policy_db_port: 5432
  policy_db_name: "nuts_policies"
  policy_db_user: "nuts"
  policy_db_password: "${POSTGRES_PASSWORD}"
  
  event_store_type: "clickhouse"
  clickhouse_url: "http://clickhouse-service:8123"
  clickhouse_db: "nuts_events"
  
  audit_store_type: "postgresql"
  audit_db_host: "postgres-service"
  audit_db_port: 5432
  audit_db_name: "nuts_audits"
  audit_db_user: "nuts"
  audit_db_password: "${POSTGRES_PASSWORD}"
  
  diagnosis_store_type: "postgresql"
  diagnosis_db_host: "postgres-service"
  diagnosis_db_port: 5432
  diagnosis_db_name: "nuts_diagnoses"
  diagnosis_db_user: "nuts"
  diagnosis_db_password: "${POSTGRES_PASSWORD}"
```

## 5. 数据库选型建议

### 5.1 开发/测试环境

| 存储类型 | 数据库 | 原因 |
|---------|--------|------|
| PolicyStore | SQLite | 零配置，易于测试 |
| EventStore | LevelDB | 轻量级，适合小规模测试 |
| AuditStore | SQLite | 零配置，易于测试 |
| DiagnosisStore | SQLite | 零配置，数据量小 |

### 5.2 小规模生产环境

| 存储类型 | 数据库 | 原因 |
|---------|--------|------|
| PolicyStore | SQLite | 策略数量少，SQLite足够 |
| EventStore | InfluxDB | 时序数据，写入性能好 |
| AuditStore | SQLite | 审计数据量中等 |
| DiagnosisStore | SQLite | 诊断数据量小 |

### 5.3 大规模生产环境

| 存储类型 | 数据库 | 原因 |
|---------|--------|------|
| PolicyStore | PostgreSQL | 支持高并发，事务支持好 |
| EventStore | ClickHouse | 大规模时序数据，查询性能好 |
| AuditStore | PostgreSQL | 支持复杂查询，事务支持好 |
| DiagnosisStore | PostgreSQL | 集中管理，便于查询 |

## 6. 性能优化建议

### 6.1 EventStore优化

1. **批量写入**：使用`WriteBatch`接口批量写入事件
2. **异步写入**：对于高写入场景，使用异步写入队列
3. **数据压缩**：ClickHouse和InfluxDB都支持数据压缩
4. **分区策略**：按时间分区，提高查询性能
5. **索引优化**：为常用查询字段创建索引

### 6.2 PolicyStore优化

1. **缓存策略**：对频繁访问的策略进行缓存
2. **连接池**：使用数据库连接池
3. **索引优化**：为ID、名称等字段创建索引

### 6.3 AuditStore优化

1. **批量更新**：聚合过程中批量更新审计数据
2. **索引优化**：为policyID、cgroupID创建索引

## 7. 迁移策略

### 7.1 数据迁移

当需要从一种数据库迁移到另一种数据库时：

1. 实现数据导出工具（从源数据库导出）
2. 实现数据导入工具（导入到目标数据库）
3. 支持增量迁移（只迁移新增数据）

### 7.2 配置迁移

1. 修改配置文件中的数据库类型
2. 重启服务
3. 验证数据完整性

## 8. 监控与告警

### 8.1 监控指标

1. **写入延迟**：监控数据写入延迟
2. **查询延迟**：监控数据查询延迟
3. **连接数**：监控数据库连接数
4. **存储空间**：监控存储空间使用情况

### 8.2 告警规则

1. 写入延迟超过阈值
2. 查询延迟超过阈值
3. 连接数超过阈值
4. 存储空间不足

## 9. 总结

Nuts系统的数据库接口设计遵循以下原则：

1. **接口抽象**：通过接口抽象，支持多种数据库实现
2. **工厂模式**：使用工厂模式创建存储实例
3. **灵活配置**：通过配置文件选择数据库类型
4. **渐进实现**：按优先级逐步实现各种数据库支持
5. **性能优化**：针对不同数据特性进行优化
6. **易于迁移**：支持数据迁移和配置迁移

这种设计使得系统可以根据不同的部署环境和规模选择合适的数据库，同时保持代码的可维护性和可扩展性。
