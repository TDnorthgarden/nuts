package db

import (
	"fmt"
	"strings"
	"sync"
)

// MemoryDB is an in-memory key-value store backed by a map.
type MemoryDB struct {
	data map[string][]byte
	mu   sync.RWMutex
}

// NewMemoryDB creates a new in-memory database.
func NewMemoryDB() *MemoryDB {
	return &MemoryDB{data: make(map[string][]byte)}
}

// Get retrieves a value by key.
func (m *MemoryDB) Get(key string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return nil, fmt.Errorf("key not found: %s", key)
	}
	return v, nil
}

// Set stores a value for the given key.
func (m *MemoryDB) Set(key string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

// Delete removes a key.
func (m *MemoryDB) Delete(key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

// List returns all keys starting with the given prefix.
func (m *MemoryDB) List(prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys := make([]string, 0)
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

// Close is a no-op for in-memory store.
func (m *MemoryDB) Close() error { return nil }

func init() {
	DefaultFactory.Register("memory", func(cfg Config) (DB, error) {
		return NewMemoryDB(), nil
	})
}
