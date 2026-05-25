package task

import (
	"testing"
)

func TestValidateStateMachineConfig_Valid(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed", "failed"},
		States: map[string]StateConfig{
			"pending":   {},
			"validating": {},
			"processing": {},
			"completed": {},
			"failed":    {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "validating", Allowed: true},
			{From: "validating", To: "processing", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
			{From: "processing", To: "failed", Allowed: true},
			{From: "failed", To: "pending", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err != nil {
		t.Fatalf("ValidateStateMachineConfig() error = %v", err)
	}
}

func TestValidateStateMachineConfig_MissingInitialState(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"completed": {},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for missing initial_state")
	}
}

func TestValidateStateMachineConfig_InitialStateNotInStates(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "nonexistent",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"completed": {},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for initial_state not in states")
	}
}

func TestValidateStateMachineConfig_NoTerminalStates(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: nil,
		States: map[string]StateConfig{
			"pending": {},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for no terminal_states")
	}
}

func TestValidateStateMachineConfig_TerminalStateNotInStates(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending": {},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for terminal_state not in states")
	}
}

func TestValidateStateMachineConfig_TransitionFromNotInStates(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending":   {},
			"completed": {},
		},
		Transitions: []TransitionConfig{
			{From: "nonexistent", To: "completed", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for transition from unknown state")
	}
}

func TestValidateStateMachineConfig_UnreachableState(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending":   {},
			"orphan":    {},
			"completed": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "completed", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for unreachable state")
	}
}

func TestValidateStateMachineConfig_NonTerminalCannotReachTerminal(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending":   {},
			"stuck":     {},
			"completed": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "stuck", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for non-terminal state that cannot reach terminal")
	}
}

func TestValidateStateMachineConfig_NonTerminalCycle(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "a",
		TerminalStates: []string{"terminal"},
		States: map[string]StateConfig{
			"a":        {},
			"b":        {},
			"terminal": {},
		},
		Transitions: []TransitionConfig{
			{From: "a", To: "b", Allowed: true},
			{From: "b", To: "a", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for non-terminal cycle")
	}
}

func TestValidateStateMachineConfig_AutoRetryToSelf(t *testing.T) {
	// Self-loop auto-retry (retry_to_state == current state) should be valid
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed", "failed"},
		States: map[string]StateConfig{
			"pending":   {},
			"processing": {AutoRetry: true, MaxRetries: 3, RetryToState: "processing"},
			"completed": {},
			"failed":    {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
			{From: "processing", To: "failed", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err != nil {
		t.Fatalf("ValidateStateMachineConfig() should accept self-retry, got error = %v", err)
	}
}

func TestValidateStateMachineConfig_AutoRetryToInvalidState(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending":   {},
			"processing": {AutoRetry: true, RetryToState: "nonexistent"},
			"completed": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for auto_retry retry_to_state not in states")
	}
}

func TestValidateStateMachineConfig_AutoRetryCreatesNonTerminalCycle(t *testing.T) {
	// auto_retry=true + retry_to_state loops back creating a cycle without terminals
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed"},
		States: map[string]StateConfig{
			"pending":   {},
			"processing": {AutoRetry: true, RetryToState: "pending"},
			"completed": {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "processing", Allowed: true},
			{From: "processing", To: "completed", Allowed: true},
		},
	}
	// pending -> processing -> completed is a valid terminal path.
	// But auto-retry adds edge processing -> pending, creating cycle [pending, processing].
	// The cycle has no terminal state, so validation should reject it.
	if err := ValidateStateMachineConfig(cfg); err == nil {
		t.Fatal("expected error for auto-retry creating non-terminal cycle")
	}
}

func TestValidateStateMachineConfig_SimpleLinearFlow(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed", "failed", "abandoned"},
		States: map[string]StateConfig{
			"pending":    {},
			"validating": {AutoRetry: true, MaxRetries: 3, RetryToState: "validating"},
			"processing": {AutoRetry: true, MaxRetries: 3, RetryToState: "processing"},
			"failover":   {},
			"completed":  {},
			"failed":     {},
			"abandoned":  {},
		},
		Transitions: []TransitionConfig{
			{From: "pending", To: "validating", Allowed: true},
			{From: "validating", To: "processing", Allowed: true},
			{From: "validating", To: "failed", Allowed: true},
			{From: "processing", To: "failover", Allowed: true},
			{From: "processing", To: "failed", Allowed: true},
			{From: "failover", To: "completed", Allowed: true},
			{From: "failover", To: "failed", Allowed: true},
			{From: "failed", To: "abandoned", Allowed: true},
		},
	}
	if err := ValidateStateMachineConfig(cfg); err != nil {
		t.Fatalf("ValidateStateMachineConfig() error = %v", err)
	}
}
