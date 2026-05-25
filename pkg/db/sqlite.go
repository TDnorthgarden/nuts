package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteDB is a SQLite-backed key-value store using a configurable table name.
type SQLiteDB struct {
	db        *sql.DB
	tableName string
}

// NewSQLiteDB creates a new SQLite database at the given path with the specified table name.
// If path is empty, it defaults to "data/nuts.db".
// If tableName is empty, it defaults to "kv".
func NewSQLiteDB(path, tableName string) (*SQLiteDB, error) {
	if path == "" {
		path = "data/nuts.db"
	}
	if tableName == "" {
		tableName = "kv"
	}

	// Create parent directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", dir, err)
	}

	// Open SQLite with WAL mode and connection pooling for better concurrency
	database, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Configure connection pool
	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(5 * time.Minute)

	// Use parameterized table creation (table name cannot be parameterized in prepared statements)
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			key TEXT PRIMARY KEY,
			value BLOB
		)
	`, tableName)
	_, err = database.Exec(createSQL)
	if err != nil {
		return nil, fmt.Errorf("create table %s: %w", tableName, err)
	}

	return &SQLiteDB{db: database, tableName: tableName}, nil
}

// Get retrieves a value by key.
func (s *SQLiteDB) Get(key string) ([]byte, error) {
	query := fmt.Sprintf("SELECT value FROM %s WHERE key = ?", s.tableName)
	var value []byte
	err := s.db.QueryRow(query, key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return value, err
}

// Set stores a value for the given key (upsert).
func (s *SQLiteDB) Set(key string, value []byte) error {
	query := fmt.Sprintf(`
		INSERT INTO %s (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value
	`, s.tableName)
	_, err := s.db.Exec(query, key, value)
	return err
}

// Delete removes a key.
func (s *SQLiteDB) Delete(key string) error {
	query := fmt.Sprintf("DELETE FROM %s WHERE key = ?", s.tableName)
	_, err := s.db.Exec(query, key)
	return err
}

// List returns all keys starting with the given prefix.
func (s *SQLiteDB) List(prefix string) ([]string, error) {
	query := fmt.Sprintf("SELECT key FROM %s WHERE key LIKE ?", s.tableName)
	rows, err := s.db.Query(query, prefix+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	keys := make([]string, 0)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			continue
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// Close closes the database connection.
func (s *SQLiteDB) Close() error {
	return s.db.Close()
}

func init() {
	DefaultFactory.Register("sqlite", func(cfg Config) (DB, error) {
		return NewSQLiteDB(cfg.Path, cfg.TableName)
	})
}
