package libdslgo

import (
	"context"
	"testing"

	"github.com/nuts-project/nuts/pkg/libdslgo"
)

func TestFunctionCalls(t *testing.T) {
	engine := libdslgo.NewEngine()

	// Test 1: len() function with string field
	t.Run("TestLenFunction", func(t *testing.T) {
		condition := `len(pod.name) > 5`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse len() function: %v", err)
		}

		event := libdslgo.Event{
			"pod": map[string]interface{}{
				"name": "test-pod-name",
			},
		}

		result, err := expr.Evaluate(context.Background(), event, engine)
		if err != nil {
			t.Fatalf("Failed to evaluate len() function: %v", err)
		}

		if !result {
			t.Errorf("Expected len(pod.name) > 5 to be true, got false")
		}
	})

	// Test 2: upper() function
	t.Run("TestUpperFunction", func(t *testing.T) {
		condition := `upper(pod.namespace) = "DEFAULT"`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse upper() function: %v", err)
		}

		event := libdslgo.Event{
			"pod": map[string]interface{}{
				"namespace": "default",
			},
		}

		result, err := expr.Evaluate(context.Background(), event, engine)
		if err != nil {
			t.Fatalf("Failed to evaluate upper() function: %v", err)
		}

		if !result {
			t.Errorf("Expected upper(pod.namespace) = \"DEFAULT\" to be true, got false")
		}
	})

	// Test 3: lower() function
	t.Run("TestLowerFunction", func(t *testing.T) {
		condition := `lower(pod.namespace) = "default"`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse lower() function: %v", err)
		}

		event := libdslgo.Event{
			"pod": map[string]interface{}{
				"namespace": "DEFAULT",
			},
		}

		result, err := expr.Evaluate(context.Background(), event, engine)
		if err != nil {
			t.Fatalf("Failed to evaluate lower() function: %v", err)
		}

		if !result {
			t.Errorf("Expected lower(pod.namespace) = \"default\" to be true, got false")
		}
	})

	// Test 4: trim() function
	t.Run("TestTrimFunction", func(t *testing.T) {
		condition := `trim(pod.name) = "test"`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse trim() function: %v", err)
		}

		event := libdslgo.Event{
			"pod": map[string]interface{}{
				"name": "  test  ",
			},
		}

		result, err := expr.Evaluate(context.Background(), event, engine)
		if err != nil {
			t.Fatalf("Failed to evaluate trim() function: %v", err)
		}

		if !result {
			t.Errorf("Expected trim(pod.name) = \"test\" to be true, got false")
		}
	})

	// Test 5: Function in boolean expression
	t.Run("TestFunctionInBooleanExpression", func(t *testing.T) {
		condition := `len(pod.name) > 5 and len(pod.namespace) = 7`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse function in boolean expression: %v", err)
		}

		event := libdslgo.Event{
			"pod": map[string]interface{}{
				"name":      "test-pod",
				"namespace": "default",
			},
		}

		result, err := expr.Evaluate(context.Background(), event, engine)
		if err != nil {
			t.Fatalf("Failed to evaluate function in boolean expression: %v", err)
		}

		if !result {
			t.Errorf("Expected len(pod.name) > 5 and len(pod.namespace) = 7 to be true, got false")
		}
	})

	// Test 6: Unknown function
	t.Run("TestUnknownFunction", func(t *testing.T) {
		condition := `unknown_func(pod.name) > 5`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse unknown function: %v", err)
		}

		event := libdslgo.Event{
			"pod": map[string]interface{}{
				"name": "test",
			},
		}

		_, err = expr.Evaluate(context.Background(), event, engine)
		if err == nil {
			t.Errorf("Expected error for unknown function, got nil")
		}
	})

	// Test 7: Function with string literal argument
	t.Run("TestFunctionWithStringLiteral", func(t *testing.T) {
		condition := `upper("hello") = "HELLO"`
		expr, err := libdslgo.ParseExpression(condition)
		if err != nil {
			t.Fatalf("Failed to parse function with string literal: %v", err)
		}

		event := libdslgo.Event{}

		result, err := expr.Evaluate(context.Background(), event, engine)
		if err != nil {
			t.Fatalf("Failed to evaluate function with string literal: %v", err)
		}

		if !result {
			t.Errorf("Expected upper(\"hello\") = \"HELLO\" to be true, got false")
		}
	})
}
