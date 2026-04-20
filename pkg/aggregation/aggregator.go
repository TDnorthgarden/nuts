package aggregation

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nuts-project/nuts/pkg/storage"
)

// EventAggregatorImpl aggregates events using specified algorithm
type EventAggregatorImpl struct {
	algorithm  AggregationAlgorithm
	eventStore storage.EventStore
	auditStore storage.AuditStore
	mu         sync.RWMutex
}

// NewEventAggregator creates a new event aggregator
func NewEventAggregator(eventStore storage.EventStore, auditStore storage.AuditStore) *EventAggregatorImpl {
	return &EventAggregatorImpl{
		algorithm:  NewSimpleAggregationAlgorithm(),
		eventStore: eventStore,
		auditStore: auditStore,
	}
}

// SetAlgorithm sets the aggregation algorithm
func (a *EventAggregatorImpl) SetAlgorithm(algo AggregationAlgorithm) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.algorithm = algo
	return nil
}

// Aggregate aggregates events for a given cgroup and policy
func (a *EventAggregatorImpl) Aggregate(cgroupID, policyID string) (*AggregatedEvent, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Query events from event store
	query := &storage.EventQuery{
		CgroupID: cgroupID,
		PolicyID: policyID,
	}

	events, err := a.eventStore.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events found for cgroup %s and policy %s", cgroupID, policyID)
	}

	// Aggregate events using the algorithm
	aggregatedEvent, err := a.algorithm.Aggregate(events)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate events: %w", err)
	}

	return aggregatedEvent, nil
}

// AggregateByTimeRange aggregates events within a time range
func (a *EventAggregatorImpl) AggregateByTimeRange(cgroupID, policyID string, start, end time.Time) (*AggregatedEvent, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Query events from event store
	events, err := a.eventStore.QueryByTimeRange(start, end, map[string]string{
		"cgroup_id": cgroupID,
		"policy_id": policyID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query events: %w", err)
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events found for cgroup %s and policy %s in time range", cgroupID, policyID)
	}

	// Aggregate events using the algorithm
	aggregatedEvent, err := a.algorithm.Aggregate(events)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate events: %w", err)
	}

	return aggregatedEvent, nil
}

// AggregateAndSave aggregates events and saves the result to audit store
func (a *EventAggregatorImpl) AggregateAndSave(cgroupID, policyID string) (*storage.Audit, error) {
	// Aggregate events
	aggregatedEvent, err := a.Aggregate(cgroupID, policyID)
	if err != nil {
		return nil, err
	}

	// Create audit record
	audit := &storage.Audit{
		ID:             generateAuditID(cgroupID, policyID, aggregatedEvent.StartTime),
		PolicyID:       policyID,
		CgroupID:       cgroupID,
		StartTime:      aggregatedEvent.StartTime,
		EndTime:        aggregatedEvent.EndTime,
		AggregatedData: aggregatedEvent.Aggregated,
		CreatedAt:      time.Now(),
	}

	// Save to audit store
	if err := a.auditStore.Create(audit); err != nil {
		return nil, fmt.Errorf("failed to save audit: %w", err)
	}

	log.Printf("[Aggregator] Created audit %s for cgroup %s and policy %s", audit.ID, cgroupID, policyID)

	return audit, nil
}

// UpdateAudit updates an existing audit record with new aggregated data
func (a *EventAggregatorImpl) UpdateAudit(auditID string) (*storage.Audit, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	// Get existing audit
	audit, err := a.auditStore.Get(auditID)
	if err != nil {
		return nil, fmt.Errorf("failed to get audit: %w", err)
	}

	// Aggregate events again
	aggregatedEvent, err := a.Aggregate(audit.CgroupID, audit.PolicyID)
	if err != nil {
		return nil, err
	}

	// Update audit record
	audit.AggregatedData = aggregatedEvent.Aggregated
	audit.EndTime = aggregatedEvent.EndTime

	// Save to audit store
	if err := a.auditStore.Update(audit); err != nil {
		return nil, fmt.Errorf("failed to update audit: %w", err)
	}

	log.Printf("[Aggregator] Updated audit %s for cgroup %s and policy %s", audit.ID, audit.CgroupID, audit.PolicyID)

	return audit, nil
}

// generateAuditID generates a unique ID for audit record
func generateAuditID(cgroupID, policyID string, startTime time.Time) string {
	return fmt.Sprintf("%s:%s:%d", cgroupID, policyID, startTime.UnixNano())
}
