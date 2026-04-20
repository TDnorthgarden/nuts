package task

import (
	"fmt"
	"sync"
	"time"

	"github.com/nuts-project/nuts/pkg/statemachine"
)

// MemoryTaskStore implements TaskStore interface using in-memory storage
type MemoryTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*Task
}

// NewMemoryTaskStore creates a new in-memory task store
func NewMemoryTaskStore() *MemoryTaskStore {
	return &MemoryTaskStore{
		tasks: make(map[string]*Task),
	}
}

// Create creates a new task
func (s *MemoryTaskStore) Create(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; exists {
		return fmt.Errorf("task %s already exists", task.ID)
	}

	s.tasks[task.ID] = task
	return nil
}

// Update updates an existing task
func (s *MemoryTaskStore) Update(task *Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[task.ID]; !exists {
		return fmt.Errorf("task %s not found", task.ID)
	}

	s.tasks[task.ID] = task
	return nil
}

// Get retrieves a task by ID
func (s *MemoryTaskStore) Get(id string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	task, exists := s.tasks[id]
	if !exists {
		return nil, fmt.Errorf("task %s not found", id)
	}

	return task, nil
}

// GetByCgroupAndPolicy retrieves a task by cgroup ID and policy ID
func (s *MemoryTaskStore) GetByCgroupAndPolicy(cgroupID, policyID string) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, task := range s.tasks {
		if task.CgroupID == cgroupID && task.PolicyID == policyID {
			// Only return non-terminal tasks
			if !task.StateMachine.IsTerminal() {
				return task, nil
			}
		}
	}

	return nil, fmt.Errorf("no active task found for cgroup %s and policy %s", cgroupID, policyID)
}

// GetByState retrieves all tasks with a specific state
func (s *MemoryTaskStore) GetByState(state TaskState) ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*Task, 0)
	for _, task := range s.tasks {
		// Convert statemachine.State to TaskState for comparison
		taskState := convertStateMachineStateToTaskState(task.StateMachine.Current())
		if taskState == state {
			tasks = append(tasks, task)
		}
	}

	return tasks, nil
}

// Delete deletes a task
func (s *MemoryTaskStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tasks[id]; !exists {
		return fmt.Errorf("task %s not found", id)
	}

	delete(s.tasks, id)
	return nil
}

// List returns all tasks
func (s *MemoryTaskStore) List() ([]*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		tasks = append(tasks, task)
	}

	return tasks, nil
}

// CleanupOldTasks removes tasks older than specified duration
func (s *MemoryTaskStore) CleanupOldTasks(olderThan time.Duration) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	count := 0

	for id, task := range s.tasks {
		if task.StateMachine.IsTerminal() && !task.EndTime.IsZero() {
			if now.Sub(task.EndTime) > olderThan {
				delete(s.tasks, id)
				count++
			}
		}
	}

	return count, nil
}

// convertStateMachineStateToTaskState converts statemachine.State to TaskState
func convertStateMachineStateToTaskState(state statemachine.State) TaskState {
	switch state {
	case "created":
		return TaskStateIdle
	case "collecting":
		return TaskStateRunning
	case "aggregating":
		return TaskStateRunning
	case "diagnosing":
		return TaskStateRunning
	case "stopped":
		return TaskStateStopped
	case "completed":
		return TaskStateCompleted
	case "failed":
		return TaskStateFailed
	default:
		return TaskStateIdle
	}
}
