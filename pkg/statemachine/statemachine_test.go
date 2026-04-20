package statemachine

import (
	"testing"
)

func TestStateMachine_Transitions(t *testing.T) {
	sm := NewStateMachine(StateCreated)

	// Test valid transitions
	tests := []struct {
		name    string
		from    State
		to      State
		reason  string
		wantErr bool
	}{
		{
			name:    "Created to Collecting",
			from:    StateCreated,
			to:      StateStopped,
			reason:  "Starting collection",
			wantErr: false,
		},
		{
			name:    "Collecting to Aggregating",
			from:    StateCreated,
			to:      StateAggregating,
			reason:  "Collection complete",
			wantErr: false,
		},
		{
			name:    "Aggregating to Analyzing",
			from:    StateAggregating,
			to:      StateAnalyzing,
			reason:  "Aggregation complete",
			wantErr: false,
		},
		{
			name:    "Analyzing to Reporting",
			from:    StateAnalyzing,
			to:      StateReporting,
			reason:  "Analysis complete",
			wantErr: false,
		},
		{
			name:    "Reporting to Completed",
			from:    StateReporting,
			to:      StateCompleted,
			reason:  "Report generated",
			wantErr: false,
		},
		{
			name:    "Created to Failed",
			from:    StateCreated,
			to:      StateFailed,
			reason:  "Error occurred",
			wantErr: false,
		},
		{
			name:    "Collecting to Failed",
			from:    StateCollecting,
			to:      StateFailed,
			reason:  "Collection failed",
			wantErr: false,
		},
		{
			name:    "Aggregating to Failed",
			from:    StateAggregating,
			to:      StateFailed,
			reason:  "Aggregation failed",
			wantErr: false,
		},
		{
			name:    "Analyzing to Failed",
			from:    StateAnalyzing,
			to:      StateFailed,
			reason:  "Analysis failed",
			wantErr: false,
		},
		{
			name:    "Reporting to Failed",
			from:    StateReporting,
			to:      StateFailed,
			reason:  "Reporting failed",
			wantErr: false,
		},
		{
			name:    "Invalid transition: Created to Completed",
			from:    StateCreated,
			to:      StateCompleted,
			reason:  "Skip to completed",
			wantErr: true,
		},
		{
			name:    "Invalid transition: Collecting to Created",
			from:    StateCollecting,
			to:      StateCreated,
			reason:  "Go back",
			wantErr: true,
		},
		{
			name:    "Invalid transition: Completed to Collecting",
			from:    StateCompleted,
			to:      StateCollecting,
			reason:  "Restart",
			wantErr: true,
		},
		{
			name:    "Invalid transition: Failed to Collecting",
			from:    StateFailed,
			to:      StateCollecting,
			reason:  "Retry",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset state machine to initial state
			sm.Reset(tt.from)

			err := sm.Transition(tt.to, tt.reason)
			if (err != nil) != tt.wantErr {
				t.Errorf("Transition() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if sm.Current() != tt.to {
					t.Errorf("Current state = %v, want %v", sm.Current(), tt.to)
				}
			}
		})
	}
}

func TestStateMachine_History(t *testing.T) {
	sm := NewStateMachine(StateCreated)

	// Perform multiple transitions
	sm.Transition(StateCollecting, "Start collection")
	sm.Transition(StateAggregating, "Start aggregation")
	sm.Transition(StateAnalyzing, "Start analysis")
	sm.Transition(StateReporting, "Start reporting")
	sm.Transition(StateCompleted, "Complete")

	history := sm.History()

	if len(history) != 5 {
		t.Errorf("History length = %d, want 5", len(history))
	}

	expectedTransitions := []struct {
		from State
		to   State
	}{
		{StateCreated, StateCollecting},
		{StateCollecting, StateAggregating},
		{StateAggregating, StateAnalyzing},
		{StateAnalyzing, StateReporting},
		{StateReporting, StateCompleted},
	}

	for i, expected := range expectedTransitions {
		if history[i].From != expected.from {
			t.Errorf("History[%d].From = %v, want %v", i, history[i].From, expected.from)
		}
		if history[i].To != expected.to {
			t.Errorf("History[%d].To = %v, want %v", i, history[i].To, expected.to)
		}
		if history[i].Reason == "" {
			t.Errorf("History[%d].Reason should not be empty", i)
		}
		if history[i].Timestamp.IsZero() {
			t.Errorf("History[%d].Timestamp should not be zero", i)
		}
	}
}

func TestStateMachine_TransitionHandler(t *testing.T) {
	sm := NewStateMachine(StateCreated)

	handlerCalled := false
	handler := func(from, to State, reason string) error {
		handlerCalled = true
		if from != StateCreated {
			t.Errorf("Handler called with from = %v, want %v", from, StateCreated)
		}
		if to != StateCollecting {
			t.Errorf("Handler called with to = %v, want %v", to, StateCollecting)
		}
		if reason != "Test reason" {
			t.Errorf("Handler called with reason = %v, want %v", reason, "Test reason")
		}
		return nil
	}

	sm.RegisterHandler(StateCreated, handler)
	sm.Transition(StateCollecting, "Test reason")

	if !handlerCalled {
		t.Error("Handler was not called")
	}
}

func TestStateMachine_IsTerminal(t *testing.T) {
	sm := NewStateMachine(StateCreated)

	if sm.IsTerminal() {
		t.Error("StateCreated should not be terminal")
	}

	sm.Transition(StateCollecting, "Start")
	if sm.IsTerminal() {
		t.Error("StateCollecting should not be terminal")
	}

	sm.Transition(StateAggregating, "Start")
	if sm.IsTerminal() {
		t.Error("StateAggregating should not be terminal")
	}

	sm.Transition(StateAnalyzing, "Start")
	if sm.IsTerminal() {
		t.Error("StateAnalyzing should not be terminal")
	}

	sm.Transition(StateReporting, "Start")
	if sm.IsTerminal() {
		t.Error("StateReporting should not be terminal")
	}

	sm.Transition(StateCompleted, "Complete")
	if !sm.IsTerminal() {
		t.Error("StateCompleted should be terminal")
	}

	// Test failed state
	sm2 := NewStateMachine(StateCreated)
	sm2.Transition(StateFailed, "Fail")
	if !sm2.IsTerminal() {
		t.Error("StateFailed should be terminal")
	}
}

func TestStateMachine_CanTransition(t *testing.T) {
	sm := NewStateMachine(StateCreated)

	// Valid transition
	if !sm.CanTransition(StateCollecting) {
		t.Error("Should be able to transition from Created to Collecting")
	}

	// Invalid transition
	if sm.CanTransition(StateCompleted) {
		t.Error("Should not be able to transition from Created to Completed")
	}

	// Perform transition
	sm.Transition(StateCollecting, "Start")

	// Check next valid transition
	if !sm.CanTransition(StateAggregating) {
		t.Error("Should be able to transition from Collecting to Aggregating")
	}

	if sm.CanTransition(StateCreated) {
		t.Error("Should not be able to transition from Collecting to Created")
	}
}

func TestInvalidTransitionError(t *testing.T) {
	err := &InvalidTransitionError{
		From: StateCreated,
		To:   StateCompleted,
	}

	expected := "invalid state transition from 'created' to 'completed'"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}
