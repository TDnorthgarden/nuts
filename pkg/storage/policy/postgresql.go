package policy

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"
	"github.com/nuts-project/nuts/pkg/storage"
)

// PostgreSQLPolicyStore PostgreSQL策略存储实现
type PostgreSQLPolicyStore struct {
	db *sql.DB
}

// NewPostgreSQLPolicyStore 创建PostgreSQL策略存储
func NewPostgreSQLPolicyStore(host string, port int, dbName, user, password string) (storage.PolicyStore, error) {
	dsn := fmt.Sprintf("host=%s port=%d dbname=%s user=%s password=%s sslmode=disable", host, port, dbName, user, password)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 创建表
	if err := createPostgreSQLTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &PostgreSQLPolicyStore{db: db}, nil
}

// createPostgreSQLTables 创建数据库表
func createPostgreSQLTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS policies (
			id VARCHAR(255) PRIMARY KEY,
			name VARCHAR(255) NOT NULL,
			targets TEXT NOT NULL,
			metrics TEXT NOT NULL,
			duration BIGINT NOT NULL,
			created_at BIGINT NOT NULL,
			updated_at BIGINT NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_policies_name ON policies(name);
	`)
	return err
}

// Create 创建策略
func (s *PostgreSQLPolicyStore) Create(policy *storage.Policy) error {
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
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, policy.ID, policy.Name, targetsJSON, metricsJSON, policy.Duration, policy.CreatedAt.Unix(), policy.UpdatedAt.Unix())

	return err
}

// Update 更新策略
func (s *PostgreSQLPolicyStore) Update(policy *storage.Policy) error {
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
		SET name = $1, targets = $2, metrics = $3, duration = $4, updated_at = $5
		WHERE id = $6
	`, policy.Name, targetsJSON, metricsJSON, policy.Duration, policy.UpdatedAt.Unix(), policy.ID)

	return err
}

// Delete 删除策略
func (s *PostgreSQLPolicyStore) Delete(id string) error {
	_, err := s.db.Exec(`DELETE FROM policies WHERE id = $1`, id)
	return err
}

// Get 获取策略
func (s *PostgreSQLPolicyStore) Get(id string) (*storage.Policy, error) {
	var policy storage.Policy
	var targetsJSON, metricsJSON string
	var createdAt, updatedAt int64

	err := s.db.QueryRow(`
		SELECT id, name, targets, metrics, duration, created_at, updated_at
		FROM policies WHERE id = $1
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
func (s *PostgreSQLPolicyStore) List() ([]*storage.Policy, error) {
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
func (s *PostgreSQLPolicyStore) Query(query *storage.PolicyQuery) ([]*storage.Policy, error) {
	var args []interface{}
	var whereClause string
	argIndex := 1

	if query.Name != "" {
		whereClause += fmt.Sprintf(" AND name LIKE $%d", argIndex)
		args = append(args, "%"+query.Name+"%")
		argIndex++
	}

	if query.Namespace != "" {
		whereClause += fmt.Sprintf(" AND targets LIKE $%d", argIndex)
		args = append(args, "%"+query.Namespace+"%")
		argIndex++
	}

	if query.Target != "" {
		whereClause += fmt.Sprintf(" AND targets LIKE $%d", argIndex)
		args = append(args, "%"+query.Target+"%")
		argIndex++
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
func (s *PostgreSQLPolicyStore) Close() error {
	return s.db.Close()
}
