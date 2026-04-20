package statemachine

import (
	"fmt"
	"sync"
	"time"
)

// State represents the state of a policy task
type State string

const (
	// StateCreated is the initial state when a task is created
	StateCreated State = "created"
	// StateRunning is when the task is running (collecting, aggregating, analyzing, reporting)
	StateRunning State = "running"
	// StateStopped is when the task is stopped
	StateStopped State = "stopped"
	// StateCompleted is when the task completed successfully
	StateCompleted State = "completed"
	// StateFailed is when the task failed
	StateFailed State = "failed"
)

// ValidTransitions defines valid state transitions
var ValidTransitions = map[State][]State{
	StateCreated:   {StateRunning, StateFailed},
	StateRunning:   {StateStopped, StateFailed},
	StateStopped:   {StateRunning, StateFailed},
	StateCompleted: {}, // Terminal state
	StateFailed:    {}, // Terminal state
}

// StateMachine manages state transitions for policy tasks
type StateMachine struct {
	mu         sync.RWMutex
	current    State
	history    []StateTransition
	transition map[State][]TransitionHandler
}

// StateTransition represents a state transition
type StateTransition struct {
	From      State
	To        State
	Timestamp time.Time
	Reason    string
}

// TransitionHandler is called when a state transition occurs
type TransitionHandler func(from, to State, reason string) error

// NewStateMachine creates a new state machine
func NewStateMachine(initialState State) *StateMachine {
	return &StateMachine{
		current:    initialState,
		history:    []StateTransition{},
		transition: make(map[State][]TransitionHandler),
	}
}

// Current returns the current state
func (sm *StateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

// CanTransition checks if a transition is valid
func (sm *StateMachine) CanTransition(to State) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	validStates, ok := ValidTransitions[sm.current]
	if !ok {
		return false
	}

	for _, validState := range validStates {
		if validState == to {
			return true
		}
	}
	return false
}

// Transition performs a state transition
func (sm *StateMachine) Transition(to State, reason string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if transition is valid
	if !sm.isValidTransition(sm.current, to) {
		return &InvalidTransitionError{
			From: sm.current,
			To:   to,
		}
	}

	// Record transition
	transition := StateTransition{
		From:      sm.current,
		To:        to,
		Timestamp: time.Now(),
		Reason:    reason,
	}
	sm.history = append(sm.history, transition)

	// Call transition handlers
	if handlers, ok := sm.transition[sm.current]; ok {
		for _, handler := range handlers {
			if err := handler(sm.current, to, reason); err != nil {
				return fmt.Errorf("transition handler error: %w", err)
			}
		}
	}

	// Update current state
	sm.current = to

	return nil
}

// isValidTransition checks if a transition is valid (internal, must hold lock)
func (sm *StateMachine) isValidTransition(from, to State) bool {
	validStates, ok := ValidTransitions[from]
	if !ok {
		return false
	}

	for _, validState := range validStates {
		if validState == to {
			return true
		}
	}
	return false
}

// History returns the transition history
func (sm *StateMachine) History() []StateTransition {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return append([]StateTransition{}, sm.history...)
}

// RegisterHandler registers a handler for state transitions from a specific state
func (sm *StateMachine) RegisterHandler(from State, handler TransitionHandler) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.transition[from] = append(sm.transition[from], handler)
}

// Reset resets the state machine to the initial state
func (sm *StateMachine) Reset(initialState State) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.current = initialState
	sm.history = []StateTransition{}
}

// IsTerminal checks if the current state is terminal
func (sm *StateMachine) IsTerminal() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current == StateCompleted || sm.current == StateFailed
}

// InvalidTransitionError represents an invalid state transition
type InvalidTransitionError struct {
	From State
	To   State
}

func (e *InvalidTransitionError) Error() string {
	return fmt.Sprintf("invalid state transition from '%s' to '%s'", e.From, e.To)
}
