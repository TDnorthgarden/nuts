package policy

import (
	"testing"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

func TestEnginePolicyStore(t *testing.T) {
	store := NewPolicyStore(db.NewMemoryDB())

	// Test Create
	p := &Policy{
		ID:          "rule-1",
		Description: "test policy",
		Enabled:     true,
		DSL:         "event.type == 'ContainerStart'",
		DSLEngine:   "cel",
		Version:     1,
	}
	if err := store.Create(p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Test Get
	got, err := store.Get("rule-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != "rule-1" {
		t.Fatalf("unexpected id: %s", got.ID)
	}
	if got.Description != "test policy" {
		t.Fatalf("unexpected description: %s", got.Description)
	}

	// Test duplicate Create
	if err := store.Create(p); err == nil {
		t.Fatal("expected error for duplicate create")
	}

	// Test Update
	p2 := &Policy{
		ID:          "rule-1",
		Description: "updated",
		Enabled:     true,
		DSL:         p.DSL,
		DSLEngine:   p.DSLEngine,
		Version:     1,
	}
	if err := store.Update(p2); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	got2, _ := store.Get("rule-1")
	if got2.Description != "updated" {
		t.Fatalf("expected updated description, got: %s", got2.Description)
	}
	if got2.Version != 2 {
		t.Fatalf("expected version 2, got: %d", got2.Version)
	}

	// Test version conflict
	p3 := &Policy{ID: "rule-1", Version: 1}
	if err := store.Update(p3); err == nil {
		t.Fatal("expected version conflict")
	}

	// Test List
	list, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 policy, got %d", len(list))
	}

	// Test ListEnabled
	enabled, _ := store.ListEnabled()
	if len(enabled) != 1 {
		t.Fatalf("expected 1 enabled policy, got %d", len(enabled))
	}

	// Test Count
	cnt, _ := store.Count()
	if cnt != 1 {
		t.Fatalf("expected count 1, got %d", cnt)
	}

	// Test Delete
	if err := store.Delete("rule-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	_, err = store.Get("rule-1")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestEnginePolicyStoreWithSQLite(t *testing.T) {
	dbInst, err := db.NewSQLiteDB(t.TempDir()+"/test.db", "")
	if err != nil {
		t.Fatalf("create sqlite: %v", err)
	}
	defer dbInst.Close()

	store := NewPolicyStore(dbInst)

	p := &Policy{
		ID:        "sqlite-policy",
		Enabled:   true,
		DSL:       "true",
		DSLEngine: "cel",
	}
	if err := store.Create(p); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	got, err := store.Get("sqlite-policy")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ID != "sqlite-policy" {
		t.Fatalf("unexpected id: %s", got.ID)
	}
}
