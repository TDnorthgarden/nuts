package policy

import (
	"encoding/json"
	"fmt"

	"github.com/sig-cloudnative/nuts/pkg/db"
)

// enginePolicyStore implements PolicyStore using a generic db.DB backend.
type enginePolicyStore struct {
	db     db.DB
	prefix string
}

// NewPolicyStore creates a PolicyStore backed by the given db.DB.
func NewPolicyStore(database db.DB) PolicyStore {
	return &enginePolicyStore{
		db:     database,
		prefix: "policy:",
	}
}

// Get retrieves a policy by ID.
func (s *enginePolicyStore) Get(id string) (*Policy, error) {
	data, err := s.db.Get(s.prefix + id)
	if err != nil {
		return nil, fmt.Errorf("get policy %s: %w", id, err)
	}

	var policy Policy
	if err := json.Unmarshal(data, &policy); err != nil {
		return nil, fmt.Errorf("unmarshal policy %s: %w", id, err)
	}
	return &policy, nil
}

// List lists all policies.
func (s *enginePolicyStore) List() ([]*Policy, error) {
	keys, err := s.db.List(s.prefix)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}

	policies := make([]*Policy, 0, len(keys))
	for _, key := range keys {
		data, err := s.db.Get(key)
		if err != nil {
			continue
		}
		var policy Policy
		if err := json.Unmarshal(data, &policy); err != nil {
			continue
		}
		policies = append(policies, &policy)
	}
	return policies, nil
}

// ListEnabled lists only enabled policies.
func (s *enginePolicyStore) ListEnabled() ([]*Policy, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}

	enabled := make([]*Policy, 0)
	for _, p := range all {
		if p.Enabled {
			enabled = append(enabled, p)
		}
	}
	return enabled, nil
}

// Create adds a new policy.
func (s *enginePolicyStore) Create(policy *Policy) error {
	if policy.ID == "" {
		return fmt.Errorf("policy id is required")
	}
	if policy.Version == 0 {
		policy.Version = 1
	}

	// Check existence
	_, err := s.db.Get(s.prefix + policy.ID)
	if err == nil {
		return fmt.Errorf("policy already exists: %s", policy.ID)
	}

	data, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}

	return s.db.Set(s.prefix+policy.ID, data)
}

// Update modifies an existing policy (optimistic locking).
func (s *enginePolicyStore) Update(policy *Policy) error {
	existing, err := s.Get(policy.ID)
	if err != nil {
		return fmt.Errorf("policy not found: %s", policy.ID)
	}

	if policy.Version != existing.Version {
		return fmt.Errorf("version conflict: expected %d, got %d", existing.Version, policy.Version)
	}

	policy.Version = existing.Version + 1

	data, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("marshal policy: %w", err)
	}

	return s.db.Set(s.prefix+policy.ID, data)
}

// Delete removes a policy.
func (s *enginePolicyStore) Delete(id string) error {
	return s.db.Delete(s.prefix + id)
}

// Count returns the number of policies.
func (s *enginePolicyStore) Count() (int, error) {
	keys, err := s.db.List(s.prefix)
	if err != nil {
		return 0, err
	}
	return len(keys), nil
}
