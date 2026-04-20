package audit

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nuts-project/nuts/pkg/storage"
)

// SQLiteAuditStore SQLite审计存储实现
type SQLiteAuditStore struct {
	db *sql.DB
}

// NewSQLiteAuditStore 创建SQLite审计存储
func NewSQLiteAuditStore(dbPath string) (storage.AuditStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 创建表
	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &SQLiteAuditStore{db: db}, nil
}

// createTables 创建数据库表
func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS audits (
			id TEXT PRIMARY KEY,
			policy_id TEXT NOT NULL,
			cgroup_id TEXT NOT NULL,
			start_time INTEGER NOT NULL,
			end_time INTEGER NOT NULL,
			aggregated_data TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_audits_policy_id ON audits(policy_id);
		CREATE INDEX IF NOT EXISTS idx_audits_cgroup_id ON audits(cgroup_id);
	`)
	return err
}

// Create 创建审计记录
func (s *SQLiteAuditStore) Create(audit *storage.Audit) error {
	now := time.Now().Unix()
	audit.CreatedAt = time.Unix(now, 0)

	dataJSON, err := json.Marshal(audit.AggregatedData)
	if err != nil {
		return fmt.Errorf("failed to marshal aggregated data: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO audits (id, policy_id, cgroup_id, start_time, end_time, aggregated_data, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, audit.ID, audit.PolicyID, audit.CgroupID, audit.StartTime.Unix(), audit.EndTime.Unix(), dataJSON, audit.CreatedAt.Unix())

	return err
}

// Get 获取审计记录
func (s *SQLiteAuditStore) Get(id string) (*storage.Audit, error) {
	var audit storage.Audit
	var dataJSON string
	var createdAt int64

	err := s.db.QueryRow(`
		SELECT id, policy_id, cgroup_id, start_time, end_time, aggregated_data, created_at
		FROM audits WHERE id = ?
	`, id).Scan(&audit.ID, &audit.PolicyID, &audit.CgroupID, &audit.StartTime, &audit.EndTime, &dataJSON, &createdAt)

	if err != nil {
		return nil, err
	}

	audit.CreatedAt = time.Unix(createdAt, 0)

	if err := json.Unmarshal([]byte(dataJSON), &audit.AggregatedData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal aggregated data: %w", err)
	}

	return &audit, nil
}

// ListByPolicy 按策略列出审计记录
func (s *SQLiteAuditStore) ListByPolicy(policyID string) ([]*storage.Audit, error) {
	rows, err := s.db.Query(`
		SELECT id, policy_id, cgroup_id, start_time, end_time, aggregated_data, created_at
		FROM audits WHERE policy_id = ? ORDER BY created_at DESC
	`, policyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanAudits(rows)
}

// ListByCgroup 按cgroup列出审计记录
func (s *SQLiteAuditStore) ListByCgroup(cgroupID string) ([]*storage.Audit, error) {
	rows, err := s.db.Query(`
		SELECT id, policy_id, cgroup_id, start_time, end_time, aggregated_data, created_at
		FROM audits WHERE cgroup_id = ? ORDER BY created_at DESC
	`, cgroupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanAudits(rows)
}

// Update 更新审计记录
func (s *SQLiteAuditStore) Update(audit *storage.Audit) error {
	dataJSON, err := json.Marshal(audit.AggregatedData)
	if err != nil {
		return fmt.Errorf("failed to marshal aggregated data: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE audits
		SET start_time = ?, end_time = ?, aggregated_data = ?
		WHERE id = ?
	`, audit.StartTime.Unix(), audit.EndTime.Unix(), dataJSON, audit.ID)

	return err
}

// scanAudits 扫描审计记录
func (s *SQLiteAuditStore) scanAudits(rows *sql.Rows) ([]*storage.Audit, error) {
	var audits []*storage.Audit
	for rows.Next() {
		var audit storage.Audit
		var dataJSON string
		var createdAt int64

		if err := rows.Scan(&audit.ID, &audit.PolicyID, &audit.CgroupID, &audit.StartTime, &audit.EndTime, &dataJSON, &createdAt); err != nil {
			return nil, err
		}

		audit.CreatedAt = time.Unix(createdAt, 0)

		if err := json.Unmarshal([]byte(dataJSON), &audit.AggregatedData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal aggregated data: %w", err)
		}

		audits = append(audits, &audit)
	}

	return audits, nil
}

// Close 关闭数据库连接
func (s *SQLiteAuditStore) Close() error {
	return s.db.Close()
}
