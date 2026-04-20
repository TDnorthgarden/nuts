package diagnosis

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nuts-project/nuts/pkg/storage"
)

// SQLiteDiagnosisStore SQLite诊断存储实现
type SQLiteDiagnosisStore struct {
	db *sql.DB
}

// NewSQLiteDiagnosisStore 创建SQLite诊断存储
func NewSQLiteDiagnosisStore(dbPath string) (storage.DiagnosisStore, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// 创建表
	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	return &SQLiteDiagnosisStore{db: db}, nil
}

// createTables 创建数据库表
func createTables(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS diagnoses (
			id TEXT PRIMARY KEY,
			audit_id TEXT NOT NULL,
			bottlenecks TEXT NOT NULL,
			summary TEXT NOT NULL,
			severity TEXT NOT NULL,
			created_at INTEGER NOT NULL
		);

		CREATE INDEX IF NOT EXISTS idx_diagnoses_audit_id ON diagnoses(audit_id);
	`)
	return err
}

// Create 创建诊断记录
func (s *SQLiteDiagnosisStore) Create(diagnosis *storage.Diagnosis) error {
	now := time.Now().Unix()
	diagnosis.CreatedAt = time.Unix(now, 0)

	bottlenecksJSON, err := json.Marshal(diagnosis.Bottlenecks)
	if err != nil {
		return fmt.Errorf("failed to marshal bottlenecks: %w", err)
	}

	_, err = s.db.Exec(`
		INSERT INTO diagnoses (id, audit_id, bottlenecks, summary, severity, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, diagnosis.ID, diagnosis.AuditID, bottlenecksJSON, diagnosis.Summary, diagnosis.Severity, diagnosis.CreatedAt.Unix())

	return err
}

// Get 获取诊断记录
func (s *SQLiteDiagnosisStore) Get(id string) (*storage.Diagnosis, error) {
	var diagnosis storage.Diagnosis
	var bottlenecksJSON string
	var createdAt int64

	err := s.db.QueryRow(`
		SELECT id, audit_id, bottlenecks, summary, severity, created_at
		FROM diagnoses WHERE id = ?
	`, id).Scan(&diagnosis.ID, &diagnosis.AuditID, &bottlenecksJSON, &diagnosis.Summary, &diagnosis.Severity, &createdAt)

	if err != nil {
		return nil, err
	}

	diagnosis.CreatedAt = time.Unix(createdAt, 0)

	if err := json.Unmarshal([]byte(bottlenecksJSON), &diagnosis.Bottlenecks); err != nil {
		return nil, fmt.Errorf("failed to unmarshal bottlenecks: %w", err)
	}

	return &diagnosis, nil
}

// ListByAudit 按审计ID列出诊断记录
func (s *SQLiteDiagnosisStore) ListByAudit(auditID string) ([]*storage.Diagnosis, error) {
	rows, err := s.db.Query(`
		SELECT id, audit_id, bottlenecks, summary, severity, created_at
		FROM diagnoses WHERE audit_id = ? ORDER BY created_at DESC
	`, auditID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanDiagnoses(rows)
}

// Update 更新诊断记录
func (s *SQLiteDiagnosisStore) Update(diagnosis *storage.Diagnosis) error {
	bottlenecksJSON, err := json.Marshal(diagnosis.Bottlenecks)
	if err != nil {
		return fmt.Errorf("failed to marshal bottlenecks: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE diagnoses
		SET bottlenecks = ?, summary = ?, severity = ?
		WHERE id = ?
	`, bottlenecksJSON, diagnosis.Summary, diagnosis.Severity, diagnosis.ID)

	return err
}

// scanDiagnoses 扫描诊断记录
func (s *SQLiteDiagnosisStore) scanDiagnoses(rows *sql.Rows) ([]*storage.Diagnosis, error) {
	var diagnoses []*storage.Diagnosis
	for rows.Next() {
		var diagnosis storage.Diagnosis
		var bottlenecksJSON string
		var createdAt int64

		if err := rows.Scan(&diagnosis.ID, &diagnosis.AuditID, &bottlenecksJSON, &diagnosis.Summary, &diagnosis.Severity, &createdAt); err != nil {
			return nil, err
		}

		diagnosis.CreatedAt = time.Unix(createdAt, 0)

		if err := json.Unmarshal([]byte(bottlenecksJSON), &diagnosis.Bottlenecks); err != nil {
			return nil, fmt.Errorf("failed to unmarshal bottlenecks: %w", err)
		}

		diagnoses = append(diagnoses, &diagnosis)
	}

	return diagnoses, nil
}

// Close 关闭数据库连接
func (s *SQLiteDiagnosisStore) Close() error {
	return s.db.Close()
}
