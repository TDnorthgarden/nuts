package libdslgo

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Evaluate evaluates a rule's condition against an event
func (e *Engine) Evaluate(ctx context.Context, rule *Rule, event Event) (bool, error) {
	// Parse the condition directly (parser handles macro references)
	if rule.Enabled != nil && !*rule.Enabled {
		return false, nil
	}

	if rule.Expr == nil {
		return false, fmt.Errorf("rule expression is nil")
	}

	// Evaluate the expression
	return rule.Expr.Evaluate(ctx, event, e)
}

// isRuleEnabled checks if a rule is enabled
func isRuleEnabled(rule *Rule) bool {
	return rule.Enabled == nil || *rule.Enabled
}

// compileRuleCondition compiles a single rule's condition into an expression
func (e *Engine) compileRuleCondition(rule *Rule) error {
	expr, err := e.ParseCompileCondition(rule.Condition)
	if err != nil {
		return fmt.Errorf("failed to compile rule '%s': %w", rule.Rule, err)
	}
	rule.Expr = expr
	return nil
}

// Compile compiles all enabled rules' conditions into expressions
// force: if true, removes rules that fail to compile instead of returning an error
func (e *Engine) Compile(force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	var compileErrors []error
	var rulesToRemoveMap = make(map[string]bool)
	var keptRules []Rule

	for i := range e.RulesSlice {
		rule := &e.RulesSlice[i]

		// Skip disabled rules
		if !isRuleEnabled(rule) {
			keptRules = append(keptRules, *rule)
			continue
		}

		// Compile the rule using the unified compilation function
		if err := e.compileRuleCondition(rule); err != nil {
			if force {
				compileErrors = append(compileErrors, err)
				rulesToRemoveMap[rule.Rule] = true
				continue
			}
			return err
		}

		keptRules = append(keptRules, *rule)
	}

	// Remove failed rules in force mode
	if len(rulesToRemoveMap) > 0 {
		// Remove from map
		for ruleName := range rulesToRemoveMap {
			delete(e.Rules, ruleName)
		}

		// Update slice with kept rules only
		e.RulesSlice = keptRules

		// Update version
		e.Version++
		e.LastUpdated = time.Now()

		// Log warnings for removed rules
		for _, err := range compileErrors {
			fmt.Printf("Warning: removed rule due to compilation error: %v\n", err)
		}
		fmt.Printf("Removed %d rules due to compilation errors\n", len(rulesToRemoveMap))
	}

	return nil
}

// expandMacros replaces macro references with their conditions
func (e *Engine) expandMacros(condition string) (string, error) {
	result := condition
	// Process macros in order, replacing only standalone macro references
	for macroName, macro := range e.Macros {
		// Use regex to match macro references that are:
		// - At the start of the string or preceded by space/paren/operator
		// - At the end of the string or followed by space/paren/operator
		// This prevents matching parts of field paths like container.annotations.prometheus.io
		pattern := fmt.Sprintf(`(^|[\s(=<>!&|])%s([\s)\)=<>!&|]|$)`, regexp.QuoteMeta(macroName))

		// Check cache first with read lock
		e.macroRegexCacheMu.RLock()
		re, ok := e.macroRegexCache[macroName]
		e.macroRegexCacheMu.RUnlock()

		if !ok {
			// Acquire write lock for compilation
			e.macroRegexCacheMu.Lock()
			// Double-check after acquiring write lock
			re, ok = e.macroRegexCache[macroName]
			if !ok {
				var err error
				re, err = regexp.Compile(pattern)
				if err != nil {
					e.macroRegexCacheMu.Unlock()
					return "", fmt.Errorf("failed to compile macro regex: %w", err)
				}
				// Cache the compiled regex
				e.macroRegexCache[macroName] = re
			}
			e.macroRegexCacheMu.Unlock()
		}

		// Replace with the macro condition wrapped in parentheses, preserving surrounding characters
		// Use strings.Builder for efficient string building
		macroCond := macro.Condition
		matches := re.FindAllStringSubmatchIndex(result, -1)

		// Build new string with replacements
		var builder strings.Builder
		builder.Grow(len(result) + len(macroCond)*len(matches))

		lastIndex := 0
		for _, match := range matches {
			// Append text before this match
			builder.WriteString(result[lastIndex:match[0]])

			if len(match) >= 6 {
				prefix := result[match[2]:match[3]]
				suffix := result[match[4]:match[5]]
				builder.WriteString(prefix)
				builder.WriteString("(")
				builder.WriteString(macroCond)
				builder.WriteString(")")
				builder.WriteString(suffix)
			}

			lastIndex = match[1]
		}
		// Append remaining text
		builder.WriteString(result[lastIndex:])

		result = builder.String()
	}
	return result, nil
}

// EvaluateAll evaluates all rules against an event and returns matching rules
func (e *Engine) EvaluateAll(event Event) ([]*Rule, error) {
	return e.EvaluateAllWithTimeout(event, 0)
}

// EvaluateAllWithTimeout evaluates all rules against an event with a timeout
func (e *Engine) EvaluateAllWithTimeout(event Event, timeout time.Duration) ([]*Rule, error) {
	var ctx context.Context
	var cancel context.CancelFunc

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), timeout)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	return e.EvaluateAllWithContext(ctx, event)
}

// EvaluateAllWithContext evaluates all rules against an event with a context
func (e *Engine) EvaluateAllWithContext(ctx context.Context, event Event) ([]*Rule, error) {
	return e.EvaluateAllParallel(ctx, event, 1)
}

// EvaluateAllParallel evaluates all rules in parallel using a worker pool
func (e *Engine) EvaluateAllParallel(ctx context.Context, event Event, workers int) ([]*Rule, error) {
	if workers <= 0 {
		workers = 1
	}

	e.mu.RLock()
	rules := make([]*Rule, len(e.RulesSlice))
	for i := range e.RulesSlice {
		rules[i] = &e.RulesSlice[i]
	}
	e.mu.RUnlock()

	// If only one worker, use sequential evaluation
	if workers == 1 {
		var matching []*Rule
		var errors []error
		for _, rule := range rules {
			if rule.Enabled != nil && !*rule.Enabled {
				continue
			}
			matched, err := e.Evaluate(ctx, rule, event)
			if err != nil {
				errors = append(errors, fmt.Errorf("failed to evaluate rule '%s': %w", rule.Rule, err))
				continue
			}
			if matched {
				matching = append(matching, rule)
			}
		}
		if len(errors) > 0 {
			return matching, errors[0]
		}
		return matching, nil
	}

	// Parallel evaluation with worker pool
	var mu sync.Mutex
	var matching []*Rule
	var errors []error

	ruleChan := make(chan *Rule, len(rules))
	for _, rule := range rules {
		ruleChan <- rule
	}
	close(ruleChan)

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rule := range ruleChan {
				if rule.Enabled != nil && !*rule.Enabled {
					continue
				}
				matched, err := e.Evaluate(ctx, rule, event)
				mu.Lock()
				if err != nil {
					errors = append(errors, fmt.Errorf("failed to evaluate rule '%s': %w", rule.Rule, err))
				}
				if matched {
					matching = append(matching, rule)
				}
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if len(errors) > 0 {
		return matching, errors[0]
	}
	return matching, nil
}

// placeholderRegex is a package-level compiled regex for FormatOutput
var placeholderRegex = regexp.MustCompile(`%([a-zA-Z_][a-zA-Z0-9_\.]*)`)

// FormatOutput formats the rule output with event data
// Supports field references like %field.name or %container.image.repository
func (e *Engine) FormatOutput(rule *Rule, event Event) (string, error) {
	output := rule.Output

	// Use pre-compiled regex to find all placeholders like %field.name
	matches := placeholderRegex.FindAllStringSubmatchIndex(output, -1)

	// Replace from end to start to preserve indices
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		if len(match) >= 4 {
			placeholderStart := match[0]
			placeholderEnd := match[1]
			fieldNameStart := match[2]
			fieldNameEnd := match[3]

			fieldName := output[fieldNameStart:fieldNameEnd]
			val, err := event.GetField(fieldName)
			if err != nil {
				continue
			}

			// Replace the placeholder with the value
			output = output[:placeholderStart] + fmt.Sprintf("%v", val) + output[placeholderEnd:]
		}
	}

	return output, nil
}

// EvaluateCondition evaluates a condition string against an event
func (e *Engine) EvaluateCondition(ctx context.Context, expr Expr, event Event) (bool, error) {
	// Evaluate
	return expr.Evaluate(ctx, event, e)
}

func (e *Engine) ParseCompileCondition(condition string) (Expr, error) {
	// Check cache first
	cacheKey := condition
	e.parseCacheMu.RLock()
	if expr, ok := e.parseCache[cacheKey]; ok {
		e.parseCacheMu.RUnlock()
		return expr, nil
	}
	e.parseCacheMu.RUnlock()

	// Expand macros
	expandedCondition, err := e.expandMacros(condition)
	if err != nil {
		return nil, err
	}

	// Parse the condition
	expr, err := ParseExpression(expandedCondition)
	if err != nil {
		return nil, err
	}

	// Apply dead code elimination if enabled
	if e.optimizationEnabled && e.optimizer != nil {
		expr = e.optimizer.Optimize(expr)
	}

	// Cache the result
	e.parseCacheMu.Lock()
	e.parseCache[cacheKey] = expr
	e.parseCacheMu.Unlock()

	return expr, nil
}

// GetList retrieves a list by name
func (e *Engine) GetList(name string) ([]string, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if list, ok := e.Lists[name]; ok {
		return list.Items, true
	}
	return nil, false
}

// validateRegex validates a regex pattern for security
func validateRegex(pattern string) error {
	// Check pattern length
	if len(pattern) > MaxRegexLength {
		return fmt.Errorf("regex pattern too long (max %d characters)", MaxRegexLength)
	}

	// Check for dangerous patterns that could cause ReDoS
	dangerousPatterns := []string{
		".*.*.*", // Multiple consecutive wildcards
		"(.*)+",  // Nested repetition
		"(.*)*",  // Nested repetition
		".*\\1",  // Backreference (can cause exponential backtracking)
		"(.+)*",  // Nested repetition with +
	}

	for _, dangerous := range dangerousPatterns {
		if strings.Contains(pattern, dangerous) {
			return fmt.Errorf("regex pattern contains dangerous pattern: %s", dangerous)
		}
	}

	return nil
}

// getCompiledRegex retrieves or compiles a regex pattern with thread-safe caching
func (e *Engine) getCompiledRegex(pattern string) (*regexp.Regexp, error) {
	// Validate pattern for security
	if err := validateRegex(pattern); err != nil {
		return nil, err
	}

	// Try read lock first for cache hit
	e.regexCacheMu.RLock()
	if re, ok := e.regexCache[pattern]; ok {
		e.regexCacheMu.RUnlock()
		return re, nil
	}
	e.regexCacheMu.RUnlock()

	// Acquire write lock for compilation
	e.regexCacheMu.Lock()
	defer e.regexCacheMu.Unlock()

	// Double-check after acquiring write lock
	if re, ok := e.regexCache[pattern]; ok {
		return re, nil
	}

	// Compile the regex
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern '%s': %w", pattern, err)
	}

	// Check number of capture groups
	if re.NumSubexp() > MaxRegexGroups {
		return nil, fmt.Errorf("regex pattern has too many capture groups (max %d)", MaxRegexGroups)
	}

	// Cache the compiled regex
	e.regexCache[pattern] = re
	return re, nil
}
