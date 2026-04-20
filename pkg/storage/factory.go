package storage

import (
	"fmt"
	"io"
)

// StoreConfig 存储配置
type StoreConfig struct {
	// PolicyStore配置
	PolicyStoreType  string `yaml:"policy_store_type"`  // sqlite, mysql, postgresql
	PolicyDBPath     string `yaml:"policy_db_path"`     // SQLite路径
	PolicyDBHost     string `yaml:"policy_db_host"`     // MySQL/PostgreSQL主机
	PolicyDBPort     int    `yaml:"policy_db_port"`     // MySQL/PostgreSQL端口
	PolicyDBName     string `yaml:"policy_db_name"`     // 数据库名
	PolicyDBUser     string `yaml:"policy_db_user"`     // 用户名
	PolicyDBPassword string `yaml:"policy_db_password"` // 密码

	// EventStore配置
	EventStoreType string `yaml:"event_store_type"` // leveldb, influxdb, clickhouse
	EventDBPath    string `yaml:"event_db_path"`    // LevelDB路径
	InfluxDBURL    string `yaml:"influxdb_url"`     // InfluxDB URL
	InfluxDBToken  string `yaml:"influxdb_token"`   // InfluxDB Token
	InfluxDBOrg    string `yaml:"influxdb_org"`     // InfluxDB Org
	InfluxDBBucket string `yaml:"influxdb_bucket"`  // InfluxDB Bucket
	ClickHouseURL  string `yaml:"clickhouse_url"`   // ClickHouse URL
	ClickHouseDB   string `yaml:"clickhouse_db"`    // ClickHouse Database

	// AuditStore配置
	AuditStoreType  string `yaml:"audit_store_type"`  // sqlite, mysql, postgresql
	AuditDBPath     string `yaml:"audit_db_path"`     // SQLite路径
	AuditDBHost     string `yaml:"audit_db_host"`     // MySQL/PostgreSQL主机
	AuditDBPort     int    `yaml:"audit_db_port"`     // MySQL/PostgreSQL端口
	AuditDBName     string `yaml:"audit_db_name"`     // 数据库名
	AuditDBUser     string `yaml:"audit_db_user"`     // 用户名
	AuditDBPassword string `yaml:"audit_db_password"` // 密码

	// DiagnosisStore配置
	DiagnosisStoreType  string `yaml:"diagnosis_store_type"`  // sqlite, mysql, postgresql
	DiagnosisDBPath     string `yaml:"diagnosis_db_path"`     // SQLite路径
	DiagnosisDBHost     string `yaml:"diagnosis_db_host"`     // MySQL/PostgreSQL主机
	DiagnosisDBPort     int    `yaml:"diagnosis_db_port"`     // MySQL/PostgreSQL端口
	DiagnosisDBName     string `yaml:"diagnosis_db_name"`     // 数据库名
	DiagnosisDBUser     string `yaml:"diagnosis_db_user"`     // 用户名
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
