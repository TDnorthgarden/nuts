package libdslgo_test

import (
	"testing"

	dsl "github.com/nuts-project/nuts/pkg/libdslgo"
)

// TestCRUDCompile 测试CRUD接口的编译功能
func TestCRUDCompile(t *testing.T) {
	t.Log("=== Test CRUD Compile ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	// Test AddRule with compilation
	t.Log("Testing AddRule with compilation")
	rule1 := &dsl.Rule{
		Rule:      "test_rule_1",
		Condition: `container.state = "running"`,
		Output:    "Container is running",
		Priority:  "INFO",
		Enabled:   dsl.BoolPtr(true),
	}

	err := engine.AddRule(rule1)
	if err != nil {
		t.Fatalf("AddRule failed: %v", err)
	}

	// Verify the rule was compiled
	addedRule, ok := engine.GetRule("test_rule_1")
	if !ok {
		t.Fatal("Rule not found after AddRule")
	}
	if addedRule.Expr == nil {
		t.Fatal("Rule expression is nil after AddRule (compilation failed)")
	}
	t.Log("✓ AddRule compiled the rule successfully")

	// Test event validation for rule1
	t.Log("Testing event validation for rule1")
	testEvent1 := dsl.Event{
		"container.state": "running",
	}
	results1, err := engine.EvaluateAll(testEvent1)
	if err != nil {
		t.Fatalf("EvaluateAll failed: %v", err)
	}
	if len(results1) != 1 {
		t.Fatalf("Expected 1 rule to match, got %d", len(results1))
	}
	if results1[0].Rule != "test_rule_1" {
		t.Fatalf("Expected rule 'test_rule_1' to match, got '%s'", results1[0].Rule)
	}
	t.Log("✓ Rule1 correctly matched event with container.state=running")

	// Test mismatch event
	testEvent1Mismatch := dsl.Event{
		"container.state": "stopped",
	}
	results1Mismatch, err := engine.EvaluateAll(testEvent1Mismatch)
	if err != nil {
		t.Fatalf("EvaluateAll failed: %v", err)
	}
	if len(results1Mismatch) != 0 {
		t.Fatalf("Expected 0 rules to match, got %d", len(results1Mismatch))
	}
	t.Log("✓ Rule1 correctly mismatched event with container.state=stopped")

	// Test UpdateRule with compilation
	t.Log("Testing UpdateRule with compilation")
	rule1Updated := &dsl.Rule{
		Rule:      "test_rule_1",
		Condition: `container.state = "stopped"`,
		Output:    "Container is stopped",
		Priority:  "WARNING",
		Enabled:   dsl.BoolPtr(true),
	}

	err = engine.UpdateRule("test_rule_1", rule1Updated)
	if err != nil {
		t.Fatalf("UpdateRule failed: %v", err)
	}

	// Verify the rule was recompiled
	updatedRule, ok := engine.GetRule("test_rule_1")
	if !ok {
		t.Fatal("Rule not found after UpdateRule")
	}
	if updatedRule.Expr == nil {
		t.Fatal("Rule expression is nil after UpdateRule (recompilation failed)")
	}
	t.Log("✓ UpdateRule recompiled the rule successfully")

	// Test event validation for updated rule
	t.Log("Testing event validation for updated rule")
	testEventUpdated := dsl.Event{
		"container.state": "stopped",
	}
	resultsUpdated, err := engine.EvaluateAll(testEventUpdated)
	if err != nil {
		t.Fatalf("EvaluateAll failed: %v", err)
	}
	if len(resultsUpdated) != 1 {
		t.Fatalf("Expected 1 rule to match, got %d", len(resultsUpdated))
	}
	if resultsUpdated[0].Rule != "test_rule_1" {
		t.Fatalf("Expected rule 'test_rule_1' to match, got '%s'", resultsUpdated[0].Rule)
	}
	if resultsUpdated[0].Output != "Container is stopped" {
		t.Fatalf("Expected output 'Container is stopped', got '%s'", resultsUpdated[0].Output)
	}
	t.Log("✓ Updated rule correctly matched event with container.state=stopped")

	// Test AddRule with macro reference
	t.Log("Testing AddRule with macro reference")
	engine.Macros["test_macro"] = &dsl.Macro{
		Name:      "test_macro",
		Condition: `container.state = "running"`,
	}

	ruleWithMacro := &dsl.Rule{
		Rule:      "test_rule_2",
		Condition: `test_macro and pod.namespace = "default"`,
		Output:    "Test macro rule",
		Priority:  "INFO",
		Enabled:   dsl.BoolPtr(true),
	}

	err = engine.AddRule(ruleWithMacro)
	if err != nil {
		t.Fatalf("AddRule with macro failed: %v", err)
	}

	// Verify the rule with macro was compiled
	rule2, ok := engine.GetRule("test_rule_2")
	if !ok {
		t.Fatal("Rule not found after AddRule with macro")
	}
	if rule2.Expr == nil {
		t.Fatal("Rule expression is nil after AddRule with macro (compilation failed)")
	}
	t.Log("✓ AddRule with macro compiled successfully")

	// Test event validation for rule with macro
	t.Log("Testing event validation for rule with macro")
	testEventMacro := dsl.Event{
		"container.state": "running",
		"pod.namespace":   "default",
	}
	resultsMacro, err := engine.EvaluateAll(testEventMacro)
	if err != nil {
		t.Fatalf("EvaluateAll failed: %v", err)
	}
	if len(resultsMacro) != 1 {
		t.Fatalf("Expected 1 rule to match (rule2 only, since rule1 was updated to match stopped), got %d", len(resultsMacro))
	}
	if resultsMacro[0].Rule != "test_rule_2" {
		t.Fatalf("Expected rule 'test_rule_2' to match, got '%s'", resultsMacro[0].Rule)
	}
	t.Log("✓ Rule with macro correctly matched event")

	// Test macro mismatch
	testEventMacroMismatch := dsl.Event{
		"container.state": "running",
		"pod.namespace":   "kube-system",
	}
	resultsMacroMismatch, err := engine.EvaluateAll(testEventMacroMismatch)
	if err != nil {
		t.Fatalf("EvaluateAll failed: %v", err)
	}
	if len(resultsMacroMismatch) != 0 {
		t.Fatalf("Expected 0 rules to match (rule1 was updated to match stopped, rule2 requires pod.namespace=default), got %d", len(resultsMacroMismatch))
	}
	t.Log("✓ Rule with macro correctly mismatched event when pod.namespace doesn't match")

	t.Log("=== CRUD Compile Test Passed ===")
}
