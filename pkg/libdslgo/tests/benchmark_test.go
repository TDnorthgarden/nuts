package libdslgo_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	dsl "github.com/nuts-project/nuts/pkg/libdslgo"
)

func BenchmarkParseCache(b *testing.B) {
	engine := dsl.NewEngine()
	condition := `pod.namespace = "default" and container.state = "running" and container.privileged = true`

	// First parse to populate cache
	_, err := engine.ParseCompileCondition(condition)
	if err != nil {
		b.Fatalf("Failed to parse condition: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ParseCompileCondition(condition)
		if err != nil {
			b.Fatalf("Failed to parse condition: %v", err)
		}
	}
}

func BenchmarkListHashLookup(b *testing.B) {
	engine := dsl.NewEngine()

	// Create a large list
	items := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		items[i] = fmt.Sprintf("item-%d", i)
	}

	list := &dsl.List{
		Name:  "test_list",
		Items: items,
		Set:   make(map[string]bool),
	}
	for _, item := range items {
		list.Set[item] = true
	}

	engine.Lists["test_list"] = list

	condition := `container.name in (test_list)`
	expr, err := engine.ParseCompileCondition(condition)
	if err != nil {
		b.Fatalf("Failed to parse condition: %v", err)
	}

	event := dsl.Event{
		"container": map[string]interface{}{
			"name": "item-500",
		},
	}

	rule := &dsl.Rule{
		Rule: "test",
		Expr: expr,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(context.Background(), rule, event)
		if err != nil {
			b.Fatalf("Failed to evaluate: %v", err)
		}
	}
}

func BenchmarkListLinearLookup(b *testing.B) {
	engine := dsl.NewEngine()

	// Create a large list without hash set
	items := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		items[i] = fmt.Sprintf("item-%d", i)
	}

	list := &dsl.List{
		Name:  "test_list",
		Items: items,
		Set:   nil, // No hash set - will use linear search
	}

	engine.Lists["test_list"] = list

	condition := `container.name in (test_list)`
	expr, err := engine.ParseCompileCondition(condition)
	if err != nil {
		b.Fatalf("Failed to parse condition: %v", err)
	}

	event := dsl.Event{
		"container": map[string]interface{}{
			"name": "item-500",
		},
	}

	rule := &dsl.Rule{
		Rule: "test",
		Expr: expr,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(context.Background(), rule, event)
		if err != nil {
			b.Fatalf("Failed to evaluate: %v", err)
		}
	}
}

func BenchmarkParallelEvaluation(b *testing.B) {
	engine := dsl.NewEngine()

	// Create multiple rules
	conditions := []string{
		`pod.namespace = "default"`,
		`container.state = "running"`,
		`container.privileged = true`,
		`pod.labels.environment = "production"`,
		`container.image contains "nginx"`,
	}

	for i, cond := range conditions {
		rule := dsl.Rule{
			Rule:      fmt.Sprintf("rule-%d", i),
			Condition: cond,
		}
		if err := engine.AddRule(&rule); err != nil {
			b.Fatalf("Failed to add rule: %v", err)
		}
	}

	event := dsl.Event{
		"pod": map[string]interface{}{
			"namespace": "default",
			"labels": map[string]interface{}{
				"environment": "production",
			},
		},
		"container": map[string]interface{}{
			"state":      "running",
			"privileged": true,
			"image":      "nginx:latest",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.EvaluateAllParallel(context.Background(), event, 10)
		if err != nil {
			b.Fatalf("Failed to evaluate: %v", err)
		}
	}
}

func BenchmarkSequentialEvaluation(b *testing.B) {
	engine := dsl.NewEngine()

	// Create multiple rules
	conditions := []string{
		`pod.namespace = "default"`,
		`container.state = "running"`,
		`container.privileged = true`,
		`pod.labels.environment = "production"`,
		`container.image contains "nginx"`,
	}

	for i, cond := range conditions {
		rule := dsl.Rule{
			Rule:      fmt.Sprintf("rule-%d", i),
			Condition: cond,
		}
		if err := engine.AddRule(&rule); err != nil {
			b.Fatalf("Failed to add rule: %v", err)
		}
	}

	event := dsl.Event{
		"pod": map[string]interface{}{
			"namespace": "default",
			"labels": map[string]interface{}{
				"environment": "production",
			},
		},
		"container": map[string]interface{}{
			"state":      "running",
			"privileged": true,
			"image":      "nginx:latest",
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.EvaluateAllParallel(context.Background(), event, 1)
		if err != nil {
			b.Fatalf("Failed to evaluate: %v", err)
		}
	}
}

func BenchmarkOptimizer(b *testing.B) {
	engine := dsl.NewEngine()

	// Create expressions that can be optimized
	conditions := []string{
		`x and x`,       // Duplicate elimination
		`not not x`,     // Double negation elimination
		`x and y and x`, // Partial duplicate
	}

	for _, cond := range conditions {
		_, err := engine.ParseCompileCondition(cond)
		if err != nil {
			b.Fatalf("Failed to parse condition: %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, cond := range conditions {
			_, err := engine.ParseCompileCondition(cond)
			if err != nil {
				b.Fatalf("Failed to parse condition: %v", err)
			}
		}
	}
}

// BenchmarkFieldPathCache tests the performance of field path caching
func BenchmarkFieldPathCache(b *testing.B) {
	engine := dsl.NewEngine()

	// Complex nested field path
	path := "pod.labels.app.kubernetes.io.name"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = engine.GetCachedFieldPath(path)
	}
}

// BenchmarkFieldPathNoCache tests field path parsing without cache
func BenchmarkFieldPathNoCache(b *testing.B) {
	path := "pod.labels.app.kubernetes.io.name"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = strings.Split(path, ".")
	}
}

// BenchmarkRegexTimeout tests regex matching with timeout protection
func BenchmarkRegexTimeout(b *testing.B) {
	engine := dsl.NewEngine()

	condition := `container.name =~ "^nginx-[a-z0-9]+$"`
	expr, err := engine.ParseCompileCondition(condition)
	if err != nil {
		b.Fatalf("Failed to parse condition: %v", err)
	}

	event := dsl.Event{
		"container": map[string]interface{}{
			"name": "nginx-12345",
		},
	}

	rule := &dsl.Rule{
		Rule: "test",
		Expr: expr,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.Evaluate(context.Background(), rule, event)
		if err != nil {
			b.Fatalf("Failed to evaluate: %v", err)
		}
	}
}

// BenchmarkComplexExpression tests parsing complex nested expressions
func BenchmarkComplexExpression(b *testing.B) {
	engine := dsl.NewEngine()

	condition := `(pod.namespace = "default" or pod.namespace = "production") and container.state = "running" and (container.privileged = true or container.image contains "nginx") and not container.name in (pause, sidecar)`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := engine.ParseCompileCondition(condition)
		if err != nil {
			b.Fatalf("Failed to parse condition: %v", err)
		}
	}
}

// BenchmarkSecurityErrorCreation tests the overhead of typed errors vs generic errors
func BenchmarkSecurityErrorCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = &dsl.SecurityError{
			Type:    "recursion_depth_exceeded",
			Limit:   "100",
			Message: "expression too complex",
		}
	}
}

// BenchmarkGenericErrorCreation tests generic error creation for comparison
func BenchmarkGenericErrorCreation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = fmt.Errorf("security error (recursion_depth_exceeded): expression too complex (limit: 100)")
	}
}
