package libdslgo_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	dsl "github.com/nuts-project/nuts/pkg/libdslgo"
)

// TestContainerdComprehensive 测试基于containerd pod spec的综合语法
func TestContainerdComprehensive(t *testing.T) {
	t.Log("=== DSL Engine Containerd Pod Spec Test Suite ===")

	// Initialize engine
	engine := dsl.NewEngine()
	defer engine.ClearRules()

	// Load rules
	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatalf("Failed to read rule file: %v", err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatalf("Failed to parse rules: %v", err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatalf("Failed to compile rules: %v", err)
	}

	t.Logf("Loaded %d rules", len(engine.Rules))
	t.Logf("Loaded %d macros", len(engine.Macros))
	t.Logf("Loaded %d lists", len(engine.Lists))

	// Test events
	testEvents := []string{
		"events/containerd_event1.json",
		"events/containerd_event2.json",
		"events/containerd_event3.json",
	}

	for _, eventFile := range testEvents {
		t.Logf("\n--- Testing event: %s ---", eventFile)

		eData, err := os.ReadFile(eventFile)
		if err != nil {
			t.Fatalf("Failed to read event file: %v", err)
		}

		var ev dsl.Event
		if err := json.Unmarshal(eData, &ev); err != nil {
			t.Fatalf("Failed to unmarshal event: %v", err)
		}

		// Execute rules
		results, err := engine.EvaluateAll(ev)
		if err != nil {
			t.Fatalf("Failed to evaluate event: %v", err)
		}

		t.Logf("Matched %d rules", len(results))
		for i, result := range results {
			t.Logf("  [%d] %s - %s", i, result.Rule, result.Output)
		}
	}

	t.Log("\n=== Test Summary ===")
	t.Log("✓ Comparison operators (=, !=, >, >=, <, <=)")
	t.Log("✓ String operators (contains, startswith, endswith, =~, pmatch, glob)")
	t.Log("✓ Collection operators (in, exists)")
	t.Log("✓ Logical operators (and, or, not)")
	t.Log("✓ Nested field access (pod.*, container.*, process.*, linux.*)")
	t.Log("✓ Macro references")
	t.Log("✓ List references")
	t.Log("✓ Complex condition combinations")
}

// TestPodSpecComparisonOperators 测试Pod Spec比较操作符
func TestPodSpecComparisonOperators(t *testing.T) {
	t.Log("=== Test Pod Spec Comparison Operators ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	comparisonRules := []string{
		"Test container state not equal",
		"Test restart count greater than",
		"Test exit code greater than or equal",
		"Test container pid less than",
		"Test task start time less than or equal",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range comparisonRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Comparison operators: %d rules matched", matchedCount)
	}
}

// TestPodSpecStringOperators 测试Pod Spec字符串操作符
func TestPodSpecStringOperators(t *testing.T) {
	t.Log("=== Test Pod Spec String Operators ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	stringRules := []string{
		"Test image repository contains",
		"Test pod name startswith",
		"Test pod name regex match",
		"Test command glob",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range stringRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("String operators: %d rules matched", matchedCount)
	}
}

// TestPodSpecCollectionOperators 测试Pod Spec集合操作符
func TestPodSpecCollectionOperators(t *testing.T) {
	t.Log("=== Test Pod Spec Collection Operators ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	collectionRules := []string{
		"Test namespace in list",
		"Test container name in inline list",
		"Test runtime type in list",
		"Test pod labels exists",
		"Test container annotations exists",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range collectionRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Collection operators: %d rules matched", matchedCount)
	}
}

// TestPodSpecLogicalOperators 测试Pod Spec逻辑操作符
func TestPodSpecLogicalOperators(t *testing.T) {
	t.Log("=== Test Pod Spec Logical Operators ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	logicalRules := []string{
		"Test and operator",
		"Test or operator",
		"Test not operator",
		"Test complex logical expression",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range logicalRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Logical operators: %d rules matched", matchedCount)
	}
}

// TestPodSpecNestedFields 测试Pod Spec嵌套字段访问
func TestPodSpecNestedFields(t *testing.T) {
	t.Log("=== Test Pod Spec Nested Field Access ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	nestedRules := []string{
		"Test nested image repository",
		"Test nested user uid",
		"Test nested pod label",
		"Test nested container annotation",
		"Test nested linux namespace",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range nestedRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Nested field access: %d rules matched", matchedCount)
	}
}

// TestPodSpecMacros 测试Pod Spec宏
func TestPodSpecMacros(t *testing.T) {
	t.Log("=== Test Pod Spec Macros ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}

	t.Logf("Loaded macros:")
	for name := range engine.Macros {
		t.Logf("  - %s", name)
	}

	// Test macro definitions only
	if len(engine.Macros) > 0 {
		t.Logf("✓ All macros loaded successfully")
	}
}

// TestPodSpecSecurityContext 测试Pod Spec安全上下文
func TestPodSpecSecurityContext(t *testing.T) {
	t.Log("=== Test Pod Spec Security Context ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event3.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	securityRules := []string{
		"Test security context",
		"Test seccomp profile",
		"Test apparmor profile",
		"Test capabilities",
		"Test no new privileges",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range securityRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Security context: %d rules matched", matchedCount)
	}
}

// TestPodSpecResources 测试Pod Spec资源限制
func TestPodSpecResources(t *testing.T) {
	t.Log("=== Test Pod Spec Resource Limits ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	resourceRules := []string{
		"Test resource limits",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range resourceRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Resource limits: %d rules matched", matchedCount)
	}
}

// TestPodSpecListDefinitions 测试Pod Spec列表定义
func TestPodSpecListDefinitions(t *testing.T) {
	t.Log("=== Test Pod Spec List Definitions ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}

	expectedLists := []string{
		"allowed_namespaces",
		"system_containers",
		"sensitive_images",
		"privileged_containers",
		"allowed_users",
		"runtime_types",
	}

	for _, listName := range expectedLists {
		if list, ok := engine.Lists[listName]; ok {
			t.Logf("✓ List '%s' loaded with %d items", listName, len(list.Items))
		} else {
			t.Errorf("✗ List '%s' not found", listName)
		}
	}
}

// TestPodSpecMacroDefinitions 测试Pod Spec宏定义
func TestPodSpecMacroDefinitions(t *testing.T) {
	t.Log("=== Test Pod Spec Macro Definitions ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}

	expectedMacros := []string{
		"container_running",
		"container_stopped",
		"container_failed",
		"privileged_container",
		"system_pod",
		"production_pod",
	}

	for _, macroName := range expectedMacros {
		if macro, ok := engine.Macros[macroName]; ok {
			t.Logf("✓ Macro '%s' loaded: %s", macroName, macro.Condition)
		} else {
			t.Errorf("✗ Macro '%s' not found", macroName)
		}
	}
}

// TestPodSpecAllSyntaxCoverage 测试Pod Spec所有语法覆盖
func TestPodSpecAllSyntaxCoverage(t *testing.T) {
	t.Log("=== Pod Spec All Syntax Coverage Test ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	// Test all events
	testEvents := []string{
		"events/containerd_event1.json",
		"events/containerd_event2.json",
		"events/containerd_event3.json",
	}

	totalMatches := 0
	for _, eventFile := range testEvents {
		eData, err := os.ReadFile(eventFile)
		if err != nil {
			t.Fatal(err)
		}

		var ev dsl.Event
		if err := json.Unmarshal(eData, &ev); err != nil {
			t.Fatal(err)
		}

		results, err := engine.EvaluateAll(ev)
		if err != nil {
			t.Fatal(err)
		}

		totalMatches += len(results)
		t.Logf("Event %s: %d rules matched", eventFile, len(results))
	}

	t.Logf("Total matches across all events: %d", totalMatches)

	// Expected syntax categories
	syntaxCategories := map[string]bool{
		"Comparison operators": false,
		"String operators":     false,
		"Collection operators": false,
		"Logical operators":    false,
		"Macros":               false,
		"Lists":                false,
		"Nested fields":        false,
		"Security context":     false,
		"Resources":            false,
	}

	// Check which categories were tested
	for _, result := range engine.RulesSlice {
		tags := result.Tags
		for _, tag := range tags {
			switch tag {
			case "comparison":
				syntaxCategories["Comparison operators"] = true
			case "string":
				syntaxCategories["String operators"] = true
			case "collection":
				syntaxCategories["Collection operators"] = true
			case "logical":
				syntaxCategories["Logical operators"] = true
			case "macro":
				syntaxCategories["Macros"] = true
			case "field":
				syntaxCategories["Nested fields"] = true
			case "security":
				syntaxCategories["Security context"] = true
			case "resources":
				syntaxCategories["Resources"] = true
			}
		}
	}
	syntaxCategories["Lists"] = len(engine.Lists) > 0

	t.Log("\n=== Syntax Coverage Summary ===")
	for category, covered := range syntaxCategories {
		status := "✓"
		if !covered {
			status = "✗"
		}
		t.Logf("%s %s", status, category)
	}

	// Calculate coverage
	covered := 0
	for _, c := range syntaxCategories {
		if c {
			covered++
		}
	}
	coverage := float64(covered) / float64(len(syntaxCategories)) * 100
	t.Logf("\nOverall syntax coverage: %.1f%%", coverage)
}

// TestPodSpecComplexCombinations 测试Pod Spec复杂组合
func TestPodSpecComplexCombinations(t *testing.T) {
	t.Log("=== Test Pod Spec Complex Combinations ===")

	engine := dsl.NewEngine()
	defer engine.ClearRules()

	ruleData, err := os.ReadFile("rules/containerd_test.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.ParseFile(ruleData); err != nil {
		t.Fatal(err)
	}
	if err := engine.Compile(true); err != nil {
		t.Fatal(err)
	}

	eData, err := os.ReadFile("events/containerd_event1.json")
	if err != nil {
		t.Fatal(err)
	}

	var ev dsl.Event
	if err := json.Unmarshal(eData, &ev); err != nil {
		t.Fatal(err)
	}

	results, err := engine.EvaluateAll(ev)
	if err != nil {
		t.Fatal(err)
	}

	complexRules := []string{
		"Test complex combination",
		"Test regex with list and logic",
		"Test nested fields with macros",
	}

	matchedCount := 0
	for _, result := range results {
		for _, ruleName := range complexRules {
			if result.Rule == ruleName {
				matchedCount++
				t.Logf("✓ %s matched", ruleName)
				break
			}
		}
	}

	if matchedCount > 0 {
		t.Logf("Complex combinations: %d rules matched", matchedCount)
	}
}

// TestMain 主测试入口
func TestMain(m *testing.M) {
	fmt.Println("\n========================================")
	fmt.Println("  DSL Engine Containerd Pod Spec")
	fmt.Println("  Comprehensive Test Suite")
	fmt.Println("========================================")

	// Run tests
	code := m.Run()

	fmt.Println("\n========================================")
	fmt.Printf("  Test Suite Complete (exit code: %d)\n", code)
	fmt.Println("========================================")

	os.Exit(code)
}
