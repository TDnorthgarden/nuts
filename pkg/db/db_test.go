package db

import (
	"os"
	"testing"
)

func TestMemoryDB(t *testing.T) {
	db := NewMemoryDB()
	defer db.Close()

	// Test Set/Get
	if err := db.Set("test:key", []byte("value")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := db.Get("test:key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "value" {
		t.Fatalf("unexpected value: %s", string(val))
	}

	// Test non-existent key
	_, err = db.Get("test:missing")
	if err == nil {
		t.Fatal("expected error for missing key")
	}

	// Test Delete
	if err := db.Delete("test:key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = db.Get("test:key")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestMemoryDBList(t *testing.T) {
	db := NewMemoryDB()
	defer db.Close()

	db.Set("task:1", []byte("a"))
	db.Set("task:2", []byte("b"))
	db.Set("policy:1", []byte("c"))

	keys, err := db.List("task:")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestFactory(t *testing.T) {
	// Test memory creation
	db1, err := DefaultFactory.Create(Config{Type: "memory"})
	if err != nil {
		t.Fatalf("create memory db: %v", err)
	}
	defer db1.Close()

	// Test unsupported type
	_, err = DefaultFactory.Create(Config{Type: "redis"})
	if err == nil {
		t.Fatal("expected error for unsupported type")
	}
}

func TestSQLiteDB(t *testing.T) {
	path := "test.db"
	defer os.Remove(path)

	db, err := NewSQLiteDB(path, "")
	if err != nil {
		t.Fatalf("create sqlite: %v", err)
	}
	defer db.Close()

	// Test Set/Get
	if err := db.Set("test:key", []byte("sqlite-value")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, err := db.Get("test:key")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if string(val) != "sqlite-value" {
		t.Fatalf("unexpected value: %s", string(val))
	}

	// Test List
	db.Set("task:1", []byte("a"))
	db.Set("task:2", []byte("b"))
	keys, err := db.List("task:")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	// Test Delete
	if err := db.Delete("test:key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = db.Get("test:key")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestFactorySQLite(t *testing.T) {
	path := "factory_test.db"
	defer os.Remove(path)

	db, err := DefaultFactory.Create(Config{Type: "sqlite", Path: path})
	if err != nil {
		t.Fatalf("create sqlite via factory: %v", err)
	}
	defer db.Close()

	if err := db.Set("key", []byte("val")); err != nil {
		t.Fatalf("Set failed: %v", err)
	}
}

func BenchmarkMemoryDBSet(b *testing.B) {
	db := NewMemoryDB()
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Set("bench:key", []byte("value"))
	}
}

func BenchmarkMemoryDBGet(b *testing.B) {
	db := NewMemoryDB()
	defer db.Close()
	db.Set("bench:key", []byte("value"))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Get("bench:key")
	}
}

func BenchmarkSQLiteDBSet(b *testing.B) {
	path := "bench_sqlite.db"
	defer os.Remove(path)

	db, _ := NewSQLiteDB(path, "")
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db.Set("bench:key", []byte("value"))
	}
}
