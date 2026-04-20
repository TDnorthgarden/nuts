package service

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nuts-project/nuts/pkg/datasource"
	"github.com/nuts-project/nuts/pkg/policy"
	"github.com/nuts-project/nuts/pkg/policyengine"
)

// Service represents the main service
type Service struct {
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	dataSource   *datasource.NRIDataSource
	policyEngine *policyengine.Engine
	notifier     policy.PolicyNotifier
	running      bool
	// Track active tasks by unique key to avoid duplicates
	// Key format: "policyid:namespace:pod_name:event_id" for pod-level tasks
	// Key format: "policyid:namespace:pod_name:container:event_id" for container-level tasks
	activeTasks map[string]string // key -> taskID
}

// NewService creates a new service instance
func NewService() *Service {
	return &Service{
		dataSource:   datasource.NewNRIDataSource(),
		policyEngine: policyengine.NewEngine(),
		activeTasks:  make(map[string]string),
		notifier:     &serviceNotifier{},
	}
}

// serviceNotifier implements PolicyNotifier interface
type serviceNotifier struct{}

func (n *serviceNotifier) NotifyCollectorStart(cgroupID string, policyID string, metrics map[string][]string) error {
	log.Printf("[ServiceNotifier] NotifyCollectorStart: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyCollectorStop(cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyCollectorStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyAggregationStart(cgroupID string, policyID string, duration time.Duration) error {
	log.Printf("[ServiceNotifier] NotifyAggregationStart: cgroupID=%s, policyID=%s, duration=%v", cgroupID, policyID, duration)
	return nil
}

func (n *serviceNotifier) NotifyAggregationStop(cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyAggregationStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyAnalysisStart(cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyAnalysisStart: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyAnalysisStop(cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyAnalysisStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyReportStart(cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyReportStart: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyReportStop(cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyReportStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyTaskCompleted(taskID string, cgroupID string, policyID string) error {
	log.Printf("[ServiceNotifier] NotifyTaskCompleted: taskID=%s, cgroupID=%s, policyID=%s", taskID, cgroupID, policyID)
	return nil
}

func (n *serviceNotifier) NotifyTaskFailed(taskID string, cgroupID string, policyID string, err error) error {
	log.Printf("[ServiceNotifier] NotifyTaskFailed: taskID=%s, cgroupID=%s, policyID=%s, error=%v", taskID, cgroupID, policyID, err)
	return nil
}

// GetPolicyEngine returns the policy engine
func (s *Service) GetPolicyEngine() *policyengine.Engine {
	return s.policyEngine
}

// Start starts the service
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("service is already running")
	}

	// Create context
	s.ctx, s.cancel = context.WithCancel(context.Background())

	// Start policy engine
	if err := s.policyEngine.Start(); err != nil {
		return fmt.Errorf("failed to start policy engine: %w", err)
	}

	// Set service as policy notifier
	s.policyEngine.SetNotifier(s)

	// Register event handler for NRI events
	s.dataSource.RegisterEventHandler(s)

	// Start data source (NRI) in a goroutine since it's blocking
	go func() {
		if err := s.dataSource.Start(); err != nil {
			log.Printf("Failed to start data source: %v", err)
		}
	}()

	s.running = true
	log.Println("Service started successfully")
	return nil
}

// Stop stops the service
func (s *Service) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	// Stop data source
	if err := s.dataSource.Stop(); err != nil {
		log.Printf("Error stopping data source: %v", err)
	}

	// Cancel context
	s.cancel()

	s.running = false
	log.Println("Service stopped")
	return nil
}

// HandleEvent implements EventHandler interface
func (s *Service) HandleEvent(event *datasource.Event) error {
	log.Printf("[Service] Received event: Type=%s, Pod=%s, Container=%s, Cgroup=%s, PID=%d",
		event.Type, event.PodName, event.Container, event.CgroupID, event.PID)

	// Convert datasource.Event to policy.Event
	policyEvent := &policy.Event{
		ID:        event.ID,
		Type:      event.Type,
		CgroupID:  event.CgroupID,
		PodName:   event.PodName,
		Namespace: event.Namespace,
		Container: event.Container,
		PID:       event.PID,
		Timestamp: event.Timestamp,
		Metadata:  event.Metadata,
	}

	// Forward event to PolicyEngine for matching
	result, err := s.policyEngine.Match(policyEvent)
	if err != nil {
		log.Printf("[Service] Error matching event against policies: %v", err)
		return fmt.Errorf("failed to match event: %w", err)
	}

	// Log match result
	if result != nil {
		if result.Matched {
			log.Printf("[Service] ✓ Policy MATCHED: PolicyID=%s, PolicyName=%s, Cgroup=%s, Duration=%ds, Metrics=%v",
				result.PolicyID, result.Reason, event.CgroupID, result.Duration, result.Metrics)

			// Handle task state transitions based on event type
			if err := s.handleTaskStateTransition(event, result.PolicyID, result.Duration, result.Metrics); err != nil {
				log.Printf("[Service] Error handling task state transition: %v", err)
				return fmt.Errorf("failed to handle task state transition: %w", err)
			}
		} else {
			log.Printf("[Service] ✗ No policy matched: Cgroup=%s, Reason=%s",
				event.CgroupID, result.Reason)
		}
	}

	return nil
}

// getTaskKey generates a unique key for a task based on event and policy ID
// For container-level events: "policyid:namespace:pod_name:container:event_id"
// For pod-level events: "policyid:namespace:pod_name:event_id"
func (s *Service) getTaskKey(event *datasource.Event, policyID string) string {
	if event.Container != "" {
		// Container-level task
		return fmt.Sprintf("%s:%s:%s:%s:%s", policyID, event.Namespace, event.PodName, event.Container, event.ID)
	}
	// Pod-level task
	return fmt.Sprintf("%s:%s:%s:%s", policyID, event.Namespace, event.PodName, event.ID)
}

// handleTaskStateTransition manages task state transitions based on event type
func (s *Service) handleTaskStateTransition(event *datasource.Event, policyID string, duration int64, metrics map[string][]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	eventType := datasource.EventType(event.Type)
	taskKey := s.getTaskKey(event, policyID)

	// Check if there's an active task for this pod/container
	taskID, exists := s.activeTasks[taskKey]

	switch eventType {
	case datasource.EventTypeRunPodSandbox, datasource.EventTypeStartContainer:
		// Create new task if not exists
		if !exists {
			// Generate unique task ID
			newTaskID := fmt.Sprintf("%s-%d", taskKey, time.Now().Unix())

			// Create task in policy engine with policy duration and metrics
			taskDuration := time.Duration(duration) * time.Second
			task := s.policyEngine.GetTaskManager().CreateTask(newTaskID, policyID, event.CgroupID, metrics, taskDuration)

			// Transition to running state - this will automatically trigger notifications
			if err := task.StartRunning(fmt.Sprintf("Event %s triggered task creation", event.Type)); err != nil {
				return fmt.Errorf("failed to start running: %w", err)
			}

			// Track active task
			s.activeTasks[taskKey] = newTaskID

			log.Printf("[Service] Created new task %s for key %s on event %s", newTaskID, taskKey, event.Type)
		} else {
			log.Printf("[Service] Task %s already exists for key %s, skipping", taskID, taskKey)
		}

	case datasource.EventTypeStopContainer, datasource.EventTypeStopPodSandbox:
		// Stop task and delete it
		if exists {
			if task, ok := s.policyEngine.GetTask(taskID); ok {
				// Transition to stopped state - this will automatically trigger notifications
				if err := task.Stop(fmt.Sprintf("Event %s triggered task stop", event.Type)); err != nil {
					log.Printf("[Service] Failed to stop task: %v", err)
				}

				log.Printf("[Service] Stopped task %s on event %s", taskID, event.Type)
			}
			// Delete task from task manager
			s.policyEngine.GetTaskManager().DeleteTask(taskID)
			// Remove from active tasks
			delete(s.activeTasks, taskKey)
		}

	default:
		// Other events are not handled
		log.Printf("[Service] Event type %s not handled for task state transition", event.Type)
	}

	return nil
}

// HandleNRIEvent implements NRIEventHandler interface
func (s *Service) HandleNRIEvent(event *datasource.NRIEvent) error {
	// Convert NRI event to generic event
	genericEvent, err := datasource.ConvertNRIEventToEvent(event)
	if err != nil {
		return fmt.Errorf("failed to convert NRI event: %w", err)
	}

	// Handle the generic event
	return s.HandleEvent(genericEvent)
}

// NotifyCollectorStart implements PolicyNotifier interface
func (s *Service) NotifyCollectorStart(cgroupID string, policyID string, metrics map[string][]string) error {
	log.Printf("NotifyCollectorStart: cgroupID=%s, policyID=%s, metrics=%v", cgroupID, policyID, metrics)
	// TODO: Implement actual collector notification
	// This will be implemented when the Collector module is ready
	return nil
}

// NotifyCollectorStop implements PolicyNotifier interface
func (s *Service) NotifyCollectorStop(cgroupID string, policyID string) error {
	log.Printf("NotifyCollectorStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	// TODO: Implement actual collector notification
	// This will be implemented when the Collector module is ready
	return nil
}

// NotifyAggregationStart implements PolicyNotifier interface
func (s *Service) NotifyAggregationStart(cgroupID string, policyID string, duration time.Duration) error {
	log.Printf("NotifyAggregationStart: cgroupID=%s, policyID=%s, duration=%v", cgroupID, policyID, duration)
	// TODO: Implement actual aggregation engine notification
	// This will be implemented when the AggregationEngine module is ready
	return nil
}

// NotifyAggregationStop implements PolicyNotifier interface
func (s *Service) NotifyAggregationStop(cgroupID string, policyID string) error {
	log.Printf("NotifyAggregationStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	// TODO: Implement actual aggregation engine notification
	// This will be implemented when the AggregationEngine module is ready
	return nil
}

// NotifyAnalysisStart implements PolicyNotifier interface
func (s *Service) NotifyAnalysisStart(cgroupID string, policyID string) error {
	log.Printf("NotifyAnalysisStart: cgroupID=%s, policyID=%s", cgroupID, policyID)
	// TODO: Implement actual analysis engine notification
	// This will be implemented when the AnalysisEngine module is ready
	return nil
}

// NotifyAnalysisStop implements PolicyNotifier interface
func (s *Service) NotifyAnalysisStop(cgroupID string, policyID string) error {
	log.Printf("NotifyAnalysisStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	// TODO: Implement actual analysis engine notification
	// This will be implemented when the AnalysisEngine module is ready
	return nil
}

// NotifyReportStart implements PolicyNotifier interface
func (s *Service) NotifyReportStart(cgroupID string, policyID string) error {
	log.Printf("NotifyReportStart: cgroupID=%s, policyID=%s", cgroupID, policyID)
	// TODO: Implement actual reporting engine notification
	// This will be implemented when the ReportingEngine module is ready
	return nil
}

// NotifyReportStop implements PolicyNotifier interface
func (s *Service) NotifyReportStop(cgroupID string, policyID string) error {
	log.Printf("NotifyReportStop: cgroupID=%s, policyID=%s", cgroupID, policyID)
	// TODO: Implement actual reporting engine notification
	// This will be implemented when the ReportingEngine module is ready
	return nil
}

// NotifyTaskCompleted implements PolicyNotifier interface
func (s *Service) NotifyTaskCompleted(taskID string, cgroupID string, policyID string) error {
	log.Printf("NotifyTaskCompleted: taskID=%s, cgroupID=%s, policyID=%s", taskID, cgroupID, policyID)
	// TODO: Implement actual task completion notification
	return nil
}

// NotifyTaskFailed implements PolicyNotifier interface
func (s *Service) NotifyTaskFailed(taskID string, cgroupID string, policyID string, err error) error {
	log.Printf("NotifyTaskFailed: taskID=%s, cgroupID=%s, policyID=%s, error=%v", taskID, cgroupID, policyID, err)
	// TODO: Implement actual task failure notification
	return nil
}
