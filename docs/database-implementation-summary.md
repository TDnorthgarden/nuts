# 数据库接口实现总结

## 概述

本文档总结了Nuts系统数据库接口的设计和实现方案。系统采用接口抽象和工厂模式，支持多种数据库类型，可以根据不同的部署环境和规模选择合适的存储方案。

## 设计原则

1. **接口抽象**：通过接口定义统一的存储操作，屏蔽底层实现差异
2. **工厂模式**：使用工厂模式创建存储实例，支持配置化选择数据库类型
3. **渐进实现**：按优先级逐步实现各种数据库支持
4. **易于扩展**：新增数据库实现只需实现接口并注册到工厂

## 存储接口

### 1. PolicyStore（策略存储）

**接口定义**：[`pkg/storage/interface.go`](pkg/storage/interface.go:6)

**数据特性**：
- 结构化数据，关系型
- 数据量相对较小
- 需要事务支持（ACID）
- 需要复杂查询

**实现方案**：
- [`pkg/storage/policy/sqlite.go`](pkg/storage/policy/sqlite.go) - SQLite实现
- [`pkg/storage/policy/mysql.go`](pkg/storage/policy/mysql.go) - MySQL实现
- [`pkg/storage/policy/postgresql.go`](pkg/storage/policy/postgresql.go) - PostgreSQL实现

**推荐使用**：
- 开发/测试环境：SQLite
- 生产环境：MySQL/PostgreSQL

### 2. EventStore（事件存储）

**接口定义**：[`pkg/storage/interface.go`](pkg/storage/interface.go:51)

**数据特性**：
- 时序数据，高写入频率
- 数据量大
- 写多读少
- 需要按时间范围查询

**实现方案**：
- [`pkg/storage/event/leveldb.go`](pkg/storage/event/leveldb.go) - LevelDB实现
- [`pkg/storage/event/influxdb.go`](pkg/storage/event/influxdb.go) - InfluxDB实现
- 待实现：ClickHouse

**推荐使用**：
- 小规模/开发环境：LevelDB
- 中等规模：InfluxDB
- 大规模生产环境：ClickHouse

### 3. AuditStore（审计存储）

**接口定义**：[`pkg/storage/interface.go`](pkg/storage/interface.go:89)

**数据特性**：
- 结构化数据，关系型
- 数据量中等
- 需要按policy、cgroup查询
- 需要更新操作

**实现方案**：
- [`pkg/storage/audit/sqlite.go`](pkg/storage/audit/sqlite.go) - SQLite实现
- 待实现：MySQL、PostgreSQL

**推荐使用**：
- 开发/测试环境：SQLite
- 生产环境：MySQL/PostgreSQL

### 4. DiagnosisStore（诊断存储）

**接口定义**：[`pkg/storage/interface.go`](pkg/storage/interface.go:118)

**数据特性**：
- 结构化数据，关系型
- 数据量小
- 需要按audit查询
- 需要更新操作

**实现方案**：
- [`pkg/storage/diagnosis/sqlite.go`](pkg/storage/diagnosis/sqlite.go) - SQLite实现
- 待实现：MySQL、PostgreSQL

**推荐使用**：
- 所有环境：SQLite（数据量小，SQLite足够）

## 工厂模式

**工厂实现**：[`pkg/storage/factory.go`](pkg/storage/factory.go)

**配置结构**：
```go
type StoreConfig struct {
    PolicyStoreType   string  // sqlite, mysql, postgresql
    EventStoreType    string  // leveldb, influxdb, clickhouse
    AuditStoreType    string  // sqlite, mysql, postgresql
    DiagnosisStoreType string // sqlite, mysql, postgresql
    
    // 各数据库的连接配置...
}
```

**使用示例**：
```go
// 创建工厂
factory, err := storage.NewStoreFactory(&storage.StoreConfig{
    PolicyStoreType: "sqlite",
    PolicyDBPath:    "/data/policies.db",
    EventStoreType:  "leveldb",
    EventDBPath:     "/data/events",
    AuditStoreType:  "sqlite",
    AuditDBPath:     "/data/audits.db",
    DiagnosisStoreType: "sqlite",
    DiagnosisDBPath:   "/data/diagnoses.db",
})

// 创建所有存储
stores, err := factory.CreateAllStores()
defer stores.Close()

// 使用存储
policy, err := stores.PolicyStore.Get("policy-id")
```

## 实现优先级

### 第一阶段（当前）
- ✅ SQLite PolicyStore
- ✅ LevelDB EventStore
- ✅ SQLite AuditStore
- ✅ SQLite DiagnosisStore
- ✅ Storage Factory

### 第二阶段
- ✅ MySQL PolicyStore
- ✅ PostgreSQL PolicyStore
- ✅ InfluxDB EventStore

### 第三阶段
- ⏳ ClickHouse EventStore
- ⏳ MySQL AuditStore
- ⏳ PostgreSQL AuditStore
- ⏳ MySQL DiagnosisStore
- ⏳ PostgreSQL DiagnosisStore

## 依赖管理

当前实现需要以下Go依赖（需要在go.mod中添加）：

```go
// SQLite
github.com/mattn/go-sqlite3

// MySQL
github.com/go-sql-driver/mysql

// PostgreSQL
github.com/lib/pq

// LevelDB
github.com/syndtr/goleveldb

// InfluxDB
github.com/influxdata/influxdb-client-go/v2

// ClickHouse (待实现)
github.com/ClickHouse/clickhouse-go/v2
```

## 配置示例

### 开发环境配置
```yaml
storage:
  policy_store_type: "sqlite"
  policy_db_path: "/data/policies.db"
  
  event_store_type: "leveldb"
  event_db_path: "/data/events"
  
  audit_store_type: "sqlite"
  audit_db_path: "/data/audits.db"
  
  diagnosis_store_type: "sqlite"
  diagnosis_db_path: "/data/diagnoses.db"
```

### 生产环境配置
```yaml
storage:
  policy_store_type: "postgresql"
  policy_db_host: "postgres-service"
  policy_db_port: 5432
  policy_db_name: "nuts_policies"
  policy_db_user: "nuts"
  policy_db_password: "${POSTGRES_PASSWORD}"
  
  event_store_type: "influxdb"
  influxdb_url: "http://influxdb-service:8086"
  influxdb_token: "${INFLUXDB_TOKEN}"
  influxdb_org: "nuts"
  influxdb_bucket: "events"
  
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

## 性能优化建议

### EventStore优化
1. **批量写入**：使用`WriteBatch`接口批量写入事件
2. **异步写入**：对于高写入场景，使用异步写入队列
3. **数据压缩**：ClickHouse和InfluxDB都支持数据压缩
4. **分区策略**：按时间分区，提高查询性能

### PolicyStore优化
1. **缓存策略**：对频繁访问的策略进行缓存
2. **连接池**：使用数据库连接池
3. **索引优化**：为ID、名称等字段创建索引

### AuditStore优化
1. **批量更新**：聚合过程中批量更新审计数据
2. **索引优化**：为policyID、cgroupID创建索引

## 数据迁移

当需要从一种数据库迁移到另一种数据库时：

1. 实现数据导出工具（从源数据库导出）
2. 实现数据导入工具（导入到目标数据库）
3. 支持增量迁移（只迁移新增数据）

## 监控与告警

### 监控指标
1. **写入延迟**：监控数据写入延迟
2. **查询延迟**：监控数据查询延迟
3. **连接数**：监控数据库连接数
4. **存储空间**：监控存储空间使用情况

### 告警规则
1. 写入延迟超过阈值
2. 查询延迟超过阈值
3. 连接数超过阈值
4. 存储空间不足

## 总结

Nuts系统的数据库接口设计遵循以下原则：

1. **接口抽象**：通过接口抽象，支持多种数据库实现
2. **工厂模式**：使用工厂模式创建存储实例
3. **灵活配置**：通过配置文件选择数据库类型
4. **渐进实现**：按优先级逐步实现各种数据库支持
5. **性能优化**：针对不同数据特性进行优化
6. **易于迁移**：支持数据迁移和配置迁移

这种设计使得系统可以根据不同的部署环境和规模选择合适的数据库，同时保持代码的可维护性和可扩展性。
