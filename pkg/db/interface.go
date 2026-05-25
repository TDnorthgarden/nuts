// Package db provides a generic key-value database abstraction layer
// with factory pattern support for different storage engines.
package db

import (
	"fmt"
	"sync"
)

// DB is a generic key-value database interface.
// Implementations include in-memory and SQLite backends.
type DB interface {
	// Get retrieves the value for the given key.
	Get(key string) ([]byte, error)

	// Set stores the value for the given key.
	Set(key string, value []byte) error

	// Delete removes the key and its value.
	Delete(key string) error

	// List returns all keys that have the given prefix.
	List(prefix string) ([]string, error)

	// Close closes the database connection.
	Close() error
}

// Config holds database configuration parameters.
type Config struct {
	Type      string
	Path      string
	TableName string // optional, defaults to "kv"
}

// Factory creates DB instances by registered type.
type Factory struct {
	creators map[string]func(Config) (DB, error)
	mu       sync.RWMutex
}

// DefaultFactory is the global database factory instance.
var DefaultFactory = &Factory{
	creators: make(map[string]func(Config) (DB, error)),
}

// Register adds a new DB type creator to the factory.
func (f *Factory) Register(name string, creator func(Config) (DB, error)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creators[name] = creator
}

// Create builds a DB instance based on the configuration Type field.
func (f *Factory) Create(cfg Config) (DB, error) {
	f.mu.RLock()
	creator, ok := f.creators[cfg.Type]
	f.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unsupported db type: %s", cfg.Type)
	}

	return creator(cfg)
}
