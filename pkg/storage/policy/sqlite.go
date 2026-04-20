package policy

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nuts-project/nuts/pkg/storage"
)

// SQLitePolicyStore SQLite策略存储实现
type SQLitePolicyStore struct {
	db *sql.DB
}

// NewSQLitePolicyStore 创建SQLite策略存储
func NewSQLitePolicyStore(dbPath string) (storage.PolicyStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 创建表
	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &SQLitePolicyStore{db: db}, nil
}

// createTables 创建数据库表
func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS policies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			targets TEXT NOT NULL,
			metrics TEXT NOT NULL,
			duration INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_policies_name ON policies(name);
	`)
	return err
}

// Create 创建策略
func (s *SQLitePolicyStore) Create(policy *storage.Policy) error {
	now := time.Now().Unix()
	policy.CreatedAt = time.Unix(now, 0)
	policy.UpdatedAt = time.Unix(now, 0)

	targetsJSON, err := jsonMarshal(policy.Targets)
	if err != nil {
		return fmt.Errorf("failed to marshal targets: %w", err)
	}

	metricsJSON, err := jsonMarshal(policy.Metrics)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO policies (id, name, targets, metrics, duration, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, policy.ID, policy.Name, targetsJSON, metricsJSON, policy.Duration, policy.CreatedAt.Unix(), policy.UpdatedAt.Unix())

	return err
}

// Update 更新策略
func (s *SQLitePolicyStore) Update(policy *storage.Policy) error {
	policy.UpdatedAt = time.Now()

	targetsJSON, err := jsonMarshal(policy.Targets)
	if err != nil {
		return fmt.Errorf("failed to marshal targets: %w", err)
	}

	metricsJSON, err := jsonMarshal(policy.Metrics)
	if err != nil {
		return fmt.Errorf("failed to marshal metrics: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE policies
		SET name = ?, targets = ?, metrics = ?, duration = ?, updated_at = ?
		WHERE id = ?
	`, policy.Name, targetsJSON, metricsJSON, policy.Duration, policy.UpdatedAt.Unix(), policy.ID)

	return err
}

// Delete 删除策略
func (s *SQLitePolicyStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM policies WHERE id = ?`, id)
	return err
}

// Get 获取策略
func (s *SQLitePolicyStore) Get(id string) (*storage.Policy, error) {
	var policy storage.Policy
	var targetsJSON, metricsJSON string
	var createdAt, updatedAt int64

	err := s.db.QueryRow(`
		SELECT id, name, targets, metrics, duration, created_at, updated_at
		FROM policies WHERE id = ?
	`, id).Scan(&policy.ID, &policy.Name, &targetsJSON, &metricsJSON, &policy.Duration, &createdAt, &updatedAt)

	if err != nil {
		return nil, err
	}

	policy.CreatedAt = time.Unix(createdAt, 0)
	policy.UpdatedAt = time.Unix(updatedAt, 0)

	if err := jsonUnmarshal(targetsJSON, &policy.Targets); err != nil {
		return nil, fmt.Errorf("failed to unmarshal targets: %w", err)
	}

	if err := jsonUnmarshal(metricsJSON, &policy.Metrics); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
	}

	return &policy, nil
}

// List 列出所有策略
func (s *SQLitePolicyStore) List() ([]*storage.Policy, error) {
	rows, err := s.db.Query(`
		SELECT id, name, targets, metrics, duration, created_at, updated_at
		FROM policies ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []*storage.Policy
	for rows.Next() {
		var policy storage.Policy
		var targetsJSON, metricsJSON string
		var createdAt, updatedAt int64

		if err := rows.Scan(&policy.ID, &policy.Name, &targetsJSON, &metricsJSON, &policy.Duration, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		policy.CreatedAt = time.Unix(createdAt, 0)
		policy.UpdatedAt = time.Unix(updatedAt, 0)

		if err := jsonUnmarshal(targetsJSON, &policy.Targets); err != nil {
			return nil, fmt.Errorf("failed to unmarshal targets: %w", err)
		}

		if err := jsonUnmarshal(metricsJSON, &policy.Metrics); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
		}

		policies = append(policies, &policy)
	}

	return policies, nil
}

// Query 查询策略
func (s *SQLitePolicyStore) Query(query *storage.PolicyQuery) ([]*storage.Policy, error) {
	var args []interface{}
	var whereClause string

	if query.Name != "" {
		whereClause += " AND name LIKE ?"
		args = append(args, "%"+query.Name+"%")
	}

	if query.Namespace != "" {
		whereClause += " AND targets LIKE ?"
		args = append(args, "%"+query.Namespace+"%")
	}

	if query.Target != "" {
		whereClause += " AND targets LIKE ?"
		args = append(args, "%"+query.Target+"%")
	}

	sqlQuery := `
		SELECT id, name, targets, metrics, duration, created_at, updated_at
		FROM policies WHERE 1=1` + whereClause + ` ORDER BY created_at DESC
	`

	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []*storage.Policy
	for rows.Next() {
		var policy storage.Policy
		var targetsJSON, metricsJSON string
		var createdAt, updatedAt int64

		if err := rows.Scan(&policy.ID, &policy.Name, &targetsJSON, &metricsJSON, &policy.Duration, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		policy.CreatedAt = time.Unix(createdAt, 0)
		policy.UpdatedAt = time.Unix(updatedAt, 0)

		if err := jsonUnmarshal(targetsJSON, &policy.Targets); err != nil {
			return nil, fmt.Errorf("failed to unmarshal targets: %w", err)
		}

		if err := jsonUnmarshal(metricsJSON, &policy.Metrics); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metrics: %w", err)
		}

		policies = append(policies, &policy)
	}

	return policies, nil
}

// Close 关闭数据库连接
func (s *SQLitePolicyStore) Close() error {
	return s.db.Close()
}

// jsonMarshal JSON序列化辅助函数
func jsonMarshal(v interface{}) (string, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// jsonUnmarshal JSON反序列化辅助函数
func jsonUnmarshal(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}
