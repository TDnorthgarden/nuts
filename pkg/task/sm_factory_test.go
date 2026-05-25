package task

import (
	"testing"
)

func TestStateMachineConfig_IsTerminalState(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:           "test",
		InitialState:   "pending",
		TerminalStates: []string{"completed", "failed"},
		States: map[string]StateConfig{
			"pending":   {},
			"cancelled": {},
		},
	}

	tests := []struct {
		name     string
		state    string
		expected bool
	}{
		{"终态在全局列表", "completed", true},
		{"终态在全局列表", "failed", true},
		{"不在终态列表中", "cancelled", false},
		{"非终态", "pending", false},
		{"未知状态", "unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.IsTerminalState(tt.state)
			if result != tt.expected {
				t.Errorf("IsTerminalState(%s) = %v, want %v", tt.state, result, tt.expected)
			}
		})
	}
}

func TestStateMachineConfig_IsTransitionAllowed(t *testing.T) {
	cfg := &StateMachineConfig{
		Name:         "test",
		InitialState: "pending",
		Transitions: []TransitionConfig{
			{From: "pending", To: "running", Allowed: true},
			{From: "running", To: "completed", Allowed: true},
			{From: "running", To: "failed", Allowed: true},
			{From: "pending", To: "failed", Allowed: false},
		},
	}

	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		{"允许的转换", "pending", "running", true},
		{"允许的转换", "running", "completed", true},
		{"允许的转换", "running", "failed", true},
		{"不允许的转换", "pending", "failed", false},
		{"未定义的转换", "completed", "pending", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.IsTransitionAllowed(tt.from, tt.to)
			if result != tt.expected {
				t.Errorf("IsTransitionAllowed(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.expected)
			}
		})
	}
}
