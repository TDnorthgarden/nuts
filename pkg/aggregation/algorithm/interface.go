package algorithm

import (
	"fmt"
	"sort"
	"time"

	"github.com/nuts-project/nuts/pkg/aggregation"
)

// AggregationAlgorithm is the interface for all aggregation algorithms
type AggregationAlgorithm interface {
	// Name returns the name of the algorithm
	Name() string

	// Aggregate aggregates a list of events
	Aggregate(events []*aggregation.Event) (*aggregation.AggregatedEvent, error)

	// Validate validates the events before aggregation
	Validate(events []*aggregation.Event) error
}

// SimpleAggregationAlgorithm implements simple deduplication aggregation
type SimpleAggregationAlgorithm struct{}

// NewSimpleAggregationAlgorithm creates a new simple aggregation algorithm
func NewSimpleAggregationAlgorithm() *SimpleAggregationAlgorithm {
	return &SimpleAggregationAlgorithm{}
}

// Name returns the name of the algorithm
func (a *SimpleAggregationAlgorithm) Name() string {
	return "simple"
}

// Aggregate aggregates events using simple deduplication
func (a *SimpleAggregationAlgorithm) Aggregate(events []*aggregation.Event) (*aggregation.AggregatedEvent, error) {
	if err := a.Validate(events); err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events to aggregate")
	}

	// Find min and max timestamps
	var minTime, maxTime time.Time
	for i, event := range events {
		if i == 0 {
			minTime = event.Timestamp
			maxTime = event.Timestamp
		} else {
			if event.Timestamp.Before(minTime) {
				minTime = event.Timestamp
			}
			if event.Timestamp.After(maxTime) {
				maxTime = event.Timestamp
			}
		}
	}

	// Deduplicate events by type and data
	uniqueEvents := make(map[string]*aggregation.Event)
	for _, event := range events {
		key := fmt.Sprintf("%s:%v", event.Type, event.Data)
		uniqueEvents[key] = event
	}

	// Aggregate data
	aggregatedData := make(map[string]interface{})
	for _, event := range uniqueEvents {
		for key := range event.Data {
			// Count occurrences of each key
			if existing, ok := aggregatedData[key]; ok {
				if count, ok := existing.(int); ok {
					aggregatedData[key] = count + 1
				}
			} else {
				aggregatedData[key] = 1
			}
		}
	}

	// Create aggregated event
	aggregatedEvent := &aggregation.AggregatedEvent{
		ID:         generateAggregatedID(events[0].CgroupID, events[0].PolicyID, minTime),
		CgroupID:   events[0].CgroupID,
		PolicyID:   events[0].PolicyID,
		StartTime:  minTime,
		EndTime:    maxTime,
		EventCount: len(uniqueEvents),
		Aggregated: aggregatedData,
		Algorithm:  a.Name(),
	}

	return aggregatedEvent, nil
}

// Validate validates the events
func (a *SimpleAggregationAlgorithm) Validate(events []*aggregation.Event) error {
	if len(events) == 0 {
		return fmt.Errorf("events list is empty")
	}

	// Check all events belong to same cgroup and policy
	cgroupID := events[0].CgroupID
	policyID := events[0].PolicyID

	for i, event := range events {
		if event.CgroupID != cgroupID {
			return fmt.Errorf("event %d has different cgroup_id: %s (expected %s)", i, event.CgroupID, cgroupID)
		}
		if event.PolicyID != policyID {
			return fmt.Errorf("event %d has different policy_id: %s (expected %s)", i, event.PolicyID, policyID)
		}
		if event.Data == nil {
			return fmt.Errorf("event %d has nil data", i)
		}
	}

	return nil
}

// TimeWindowAggregationAlgorithm implements time window aggregation
type TimeWindowAggregationAlgorithm struct {
	window time.Duration
}

// NewTimeWindowAggregationAlgorithm creates a new time window aggregation algorithm
func NewTimeWindowAggregationAlgorithm(window time.Duration) *TimeWindowAggregationAlgorithm {
	return &TimeWindowAggregationAlgorithm{
		window: window,
	}
}

// Name returns the name of the algorithm
func (a *TimeWindowAggregationAlgorithm) Name() string {
	return "timewindow"
}

// Aggregate aggregates events using time window
func (a *TimeWindowAggregationAlgorithm) Aggregate(events []*aggregation.Event) (*aggregation.AggregatedEvent, error) {
	if err := a.Validate(events); err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events to aggregate")
	}

	// Sort events by timestamp
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	// Group events into time windows
	windows := make(map[int64][]*aggregation.Event)
	for _, event := range events {
		windowStart := event.Timestamp.Truncate(a.window).Unix()
		windows[windowStart] = append(windows[windowStart], event)
	}

	// Aggregate each window
	aggregatedData := make(map[string]interface{})
	windowCount := 0
	var minTime, maxTime time.Time

	for windowStart, windowEvents := range windows {
		windowCount++
		windowTime := time.Unix(windowStart, 0)
		if windowCount == 1 {
			minTime = windowTime
		}
		maxTime = windowTime.Add(a.window)

		// Aggregate events in this window
		for _, event := range windowEvents {
			for key, value := range event.Data {
				// Sum numeric values, count non-numeric
				if num, ok := toFloat64(value); ok {
					if existing, ok := aggregatedData[key]; ok {
						if sum, ok := existing.(float64); ok {
							aggregatedData[key] = sum + num
						}
					} else {
						aggregatedData[key] = num
					}
				} else {
					if existing, ok := aggregatedData[key]; ok {
						if count, ok := existing.(int); ok {
							aggregatedData[key] = count + 1
						}
					} else {
						aggregatedData[key] = 1
					}
				}
			}
		}
	}

	// Create aggregated event
	aggregatedEvent := &aggregation.AggregatedEvent{
		ID:         generateAggregatedID(events[0].CgroupID, events[0].PolicyID, minTime),
		CgroupID:   events[0].CgroupID,
		PolicyID:   events[0].PolicyID,
		StartTime:  minTime,
		EndTime:    maxTime,
		EventCount: len(events),
		Aggregated: aggregatedData,
		Algorithm:  a.Name(),
	}

	return aggregatedEvent, nil
}

// Validate validates the events
func (a *TimeWindowAggregationAlgorithm) Validate(events []*aggregation.Event) error {
	if len(events) == 0 {
		return fmt.Errorf("events list is empty")
	}

	// Check all events belong to same cgroup and policy
	cgroupID := events[0].CgroupID
	policyID := events[0].PolicyID

	for i, event := range events {
		if event.CgroupID != cgroupID {
			return fmt.Errorf("event %d has different cgroup_id: %s (expected %s)", i, event.CgroupID, cgroupID)
		}
		if event.PolicyID != policyID {
			return fmt.Errorf("event %d has different policy_id: %s (expected %s)", i, event.PolicyID, policyID)
		}
		if event.Data == nil {
			return fmt.Errorf("event %d has nil data", i)
		}
	}

	return nil
}

// StatisticalAggregationAlgorithm implements statistical aggregation
type StatisticalAggregationAlgorithm struct {
	metrics []string
}

// NewStatisticalAggregationAlgorithm creates a new statistical aggregation algorithm
func NewStatisticalAggregationAlgorithm(metrics []string) *StatisticalAggregationAlgorithm {
	return &StatisticalAggregationAlgorithm{
		metrics: metrics,
	}
}

// Name returns the name of the algorithm
func (a *StatisticalAggregationAlgorithm) Name() string {
	return "statistical"
}

// Aggregate aggregates events using statistical methods
func (a *StatisticalAggregationAlgorithm) Aggregate(events []*aggregation.Event) (*aggregation.AggregatedEvent, error) {
	if err := a.Validate(events); err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events to aggregate")
	}

	// Find min and max timestamps
	var minTime, maxTime time.Time
	for i, event := range events {
		if i == 0 {
			minTime = event.Timestamp
			maxTime = event.Timestamp
		} else {
			if event.Timestamp.Before(minTime) {
				minTime = event.Timestamp
			}
			if event.Timestamp.After(maxTime) {
				maxTime = event.Timestamp
			}
		}
	}

	// Collect numeric values for each metric
	metricValues := make(map[string][]float64)
	for _, event := range events {
		for key, value := range event.Data {
			if num, ok := toFloat64(value); ok {
				metricValues[key] = append(metricValues[key], num)
			}
		}
	}

	// Calculate statistics for each metric
	aggregatedData := make(map[string]interface{})
	for metric, values := range metricValues {
		if len(values) == 0 {
			continue
		}

		// Calculate statistics
		stats := calculateStatistics(values)
		aggregatedData[metric] = stats
	}

	// Create aggregated event
	aggregatedEvent := &aggregation.AggregatedEvent{
		ID:         generateAggregatedID(events[0].CgroupID, events[0].PolicyID, minTime),
		CgroupID:   events[0].CgroupID,
		PolicyID:   events[0].PolicyID,
		StartTime:  minTime,
		EndTime:    maxTime,
		EventCount: len(events),
		Aggregated: aggregatedData,
		Algorithm:  a.Name(),
	}

	return aggregatedEvent, nil
}

// Validate validates the events
func (a *StatisticalAggregationAlgorithm) Validate(events []*aggregation.Event) error {
	if len(events) == 0 {
		return fmt.Errorf("events list is empty")
	}

	// Check all events belong to same cgroup and policy
	cgroupID := events[0].CgroupID
	policyID := events[0].PolicyID

	for i, event := range events {
		if event.CgroupID != cgroupID {
			return fmt.Errorf("event %d has different cgroup_id: %s (expected %s)", i, event.CgroupID, cgroupID)
		}
		if event.PolicyID != policyID {
			return fmt.Errorf("event %d has different policy_id: %s (expected %s)", i, event.PolicyID, policyID)
		}
		if event.Data == nil {
			return fmt.Errorf("event %d has nil data", i)
		}
	}

	return nil
}

// FrequencyAggregationAlgorithm implements frequency aggregation
type FrequencyAggregationAlgorithm struct {
	threshold int
}

// NewFrequencyAggregationAlgorithm creates a new frequency aggregation algorithm
func NewFrequencyAggregationAlgorithm(threshold int) *FrequencyAggregationAlgorithm {
	return &FrequencyAggregationAlgorithm{
		threshold: threshold,
	}
}

// Name returns the name of the algorithm
func (a *FrequencyAggregationAlgorithm) Name() string {
	return "frequency"
}

// Aggregate aggregates events using frequency analysis
func (a *FrequencyAggregationAlgorithm) Aggregate(events []*aggregation.Event) (*aggregation.AggregatedEvent, error) {
	if err := a.Validate(events); err != nil {
		return nil, err
	}

	if len(events) == 0 {
		return nil, fmt.Errorf("no events to aggregate")
	}

	// Find min and max timestamps
	var minTime, maxTime time.Time
	for i, event := range events {
		if i == 0 {
			minTime = event.Timestamp
			maxTime = event.Timestamp
		} else {
			if event.Timestamp.Before(minTime) {
				minTime = event.Timestamp
			}
			if event.Timestamp.After(maxTime) {
				maxTime = event.Timestamp
			}
		}
	}

	// Count frequency of each event type and data
	frequency := make(map[string]int)
	for _, event := range events {
		key := fmt.Sprintf("%s:%v", event.Type, event.Data)
		frequency[key]++
	}

	// Filter events above threshold
	filteredEvents := make(map[string]interface{})
	for key, count := range frequency {
		if count >= a.threshold {
			filteredEvents[key] = count
		}
	}

	// Create aggregated event
	aggregatedEvent := &aggregation.AggregatedEvent{
		ID:         generateAggregatedID(events[0].CgroupID, events[0].PolicyID, minTime),
		CgroupID:   events[0].CgroupID,
		PolicyID:   events[0].PolicyID,
		StartTime:  minTime,
		EndTime:    maxTime,
		EventCount: len(events),
		Aggregated: filteredEvents,
		Algorithm:  a.Name(),
	}

	return aggregatedEvent, nil
}

// Validate validates the events
func (a *FrequencyAggregationAlgorithm) Validate(events []*aggregation.Event) error {
	if len(events) == 0 {
		return fmt.Errorf("events list is empty")
	}

	// Check all events belong to same cgroup and policy
	cgroupID := events[0].CgroupID
	policyID := events[0].PolicyID

	for i, event := range events {
		if event.CgroupID != cgroupID {
			return fmt.Errorf("event %d has different cgroup_id: %s (expected %s)", i, event.CgroupID, cgroupID)
		}
		if event.PolicyID != policyID {
			return fmt.Errorf("event %d has different policy_id: %s (expected %s)", i, event.PolicyID, policyID)
		}
		if event.Data == nil {
			return fmt.Errorf("event %d has nil data", i)
		}
	}

	return nil
}

// CustomAggregationAlgorithm implements custom aggregation
type CustomAggregationAlgorithm struct {
	// Custom fields to be defined
}

// NewCustomAggregationAlgorithm creates a new custom aggregation algorithm
func NewCustomAggregationAlgorithm() *CustomAggregationAlgorithm {
	return &CustomAggregationAlgorithm{}
}

// Name returns the name of the algorithm
func (a *CustomAggregationAlgorithm) Name() string {
	return "custom"
}

// Aggregate aggregates events using custom logic
func (a *CustomAggregationAlgorithm) Aggregate(events []*aggregation.Event) (*aggregation.AggregatedEvent, error) {
	// Implementation to be added by user
	return &aggregation.AggregatedEvent{}, fmt.Errorf("custom aggregation not implemented")
}

// Validate validates the events
func (a *CustomAggregationAlgorithm) Validate(events []*aggregation.Event) error {
	if len(events) == 0 {
		return fmt.Errorf("events list is empty")
	}
	return nil
}

// Helper functions

// generateAggregatedID generates a unique ID for the aggregated event
func generateAggregatedID(cgroupID, policyID string, startTime time.Time) string {
	return fmt.Sprintf("%s:%s:%d", cgroupID, policyID, startTime.UnixNano())
}

// toFloat64 converts interface{} to float64
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}

// calculateStatistics calculates statistics for a slice of float64 values
func calculateStatistics(values []float64) map[string]interface{} {
	if len(values) == 0 {
		return nil
	}

	// Sort values for median calculation
	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	// Calculate sum
	sum := 0.0
	for _, v := range sorted {
		sum += v
	}

	// Calculate mean
	mean := sum / float64(len(sorted))

	// Calculate median
	var median float64
	n := len(sorted)
	if n%2 == 0 {
		median = (sorted[n/2-1] + sorted[n/2]) / 2
	} else {
		median = sorted[n/2]
	}

	// Calculate variance and standard deviation
	variance := 0.0
	for _, v := range sorted {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(len(sorted))
	stdDev := variance

	// Calculate min and max
	minVal := sorted[0]
	maxVal := sorted[n-1]

	return map[string]interface{}{
		"count":    len(sorted),
		"sum":      sum,
		"mean":     mean,
		"median":   median,
		"std_dev":  stdDev,
		"variance": variance,
		"min":      minVal,
		"max":      maxVal,
	}
}
