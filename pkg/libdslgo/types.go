package libdslgo

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// BoolPtr returns a pointer to a bool value
func BoolPtr(b bool) *bool {
	return &b
}

// Error types for precise error handling and debugging

// ParseError represents an error during expression parsing
type ParseError struct {
	Pos     *Position
	Message string
	Cause   error
}

func (e *ParseError) Error() string {
	if e.Pos != nil {
		return fmt.Sprintf("parse error at line %d, col %d: %s", e.Pos.Line, e.Pos.Column, e.Message)
	}
	return fmt.Sprintf("parse error: %s", e.Message)
}

func (e *ParseError) Unwrap() error {
	return e.Cause
}

// EvalError represents an error during expression evaluation
type EvalError struct {
	Field    string
	Operator string
	Value    string
	Message  string
	Cause    error
}

func (e *EvalError) Error() string {
	if e.Field != "" {
		return fmt.Sprintf("evaluation error for field '%s' with operator '%s': %s", e.Field, e.Operator, e.Message)
	}
	return fmt.Sprintf("evaluation error: %s", e.Message)
}

func (e *EvalError) Unwrap() error {
	return e.Cause
}

// ValidationError represents an error during rule/entity validation
type ValidationError struct {
	Entity  string
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error for %s '%s': %s", e.Entity, e.Field, e.Message)
}

// SecurityError represents a security-related error (e.g., timeout, depth exceeded)
type SecurityError struct {
	Type    string
	Limit   string
	Message string
}

func (e *SecurityError) Error() string {
	return fmt.Sprintf("security error (%s): %s (limit: %s)", e.Type, e.Message, e.Limit)
}

// Rule represents a detection rule
type Rule struct {
	Rule      string   `yaml:"rule"`
	Desc      string   `yaml:"desc"`
	Condition string   `yaml:"condition"`
	Output    string   `yaml:"output"`
	Priority  string   `yaml:"priority"`
	Tags      []string `yaml:"tags"`
	Enabled   *bool    `yaml:"enabled"`
	Expr      Expr     `yaml:"-"`
	Version   int64    `yaml:"-"` // Rule version for tracking updates
}

// Macro represents a reusable condition
type Macro struct {
	Name      string `yaml:"macro"`
	Condition string `yaml:"condition"`
	Expr      Expr   `yaml:"-"` // Cached parsed expression for performance
}

// List represents a list of values
type List struct {
	Name  string          `yaml:"list"`
	Items []string        `yaml:"items"`
	Set   map[string]bool // Hash set for O(1) lookup
}

// Engine holds the DSL rules, macros, and lists
type Engine struct {
	mu                  sync.RWMutex
	Rules               map[string]*Rule // Changed from slice to map for O(1) lookups
	RulesSlice          []Rule           // Keep slice for backward compatibility
	Macros              map[string]*Macro
	Lists               map[string]*List
	Version             int64                     // Engine version for tracking updates
	LastUpdated         time.Time                 // Last update time
	macroRegexCache     map[string]*regexp.Regexp // Cache for compiled macro regex patterns
	macroRegexCacheMu   sync.RWMutex              // Mutex for macro regex cache
	regexCache          map[string]*regexp.Regexp // Cache for compiled regex patterns (thread-safe)
	regexCacheMu        sync.RWMutex              // Mutex for regex cache
	parseCache          map[string]Expr           // Cache for parsed expressions
	parseCacheMu        sync.RWMutex              // Mutex for parse cache
	fieldPathCache      map[string][]string       // Cache for parsed field paths (e.g., "a.b.c" -> ["a", "b", "c"])
	fieldPathCacheMu    sync.RWMutex              // Mutex for field path cache
	Functions           *FunctionRegistry         // Custom function registry
	optimizer           *Optimizer                // Combined optimizer (constant folding + dead code elimination)
	optimizationEnabled bool                      // Whether optimization is enabled
}

// NewEngine creates a new DSL engine
func NewEngine() *Engine {
	funcRegistry := NewFunctionRegistry()
	funcRegistry.RegisterBuiltinFunctions()

	return &Engine{
		Rules:               make(map[string]*Rule),
		RulesSlice:          make([]Rule, 0),
		Macros:              make(map[string]*Macro),
		Lists:               make(map[string]*List),
		Version:             0,
		macroRegexCache:     make(map[string]*regexp.Regexp),
		regexCache:          make(map[string]*regexp.Regexp),
		parseCache:          make(map[string]Expr),
		fieldPathCache:      make(map[string][]string),
		Functions:           funcRegistry,
		optimizer:           NewOptimizer(),
		optimizationEnabled: true,
	}
}

const (
	MaxRuleNameLength = 256  // Maximum length for rule names
	MinRuleNameLength = 1    // Minimum length for rule names
	MaxFieldDepth     = 32   // Maximum field access depth to prevent stack overflow
	MaxRegexLength    = 1000 // Maximum regex pattern length
	MaxRegexGroups    = 10   // Maximum number of capture groups
)

// Execution limits for security
var (
	MaxRegexMatchTime = 100 * time.Millisecond // Maximum regex execution time to prevent ReDoS
)

// ValidateRuleName validates a rule name for safety
func ValidateRuleName(ruleName string) error {
	// Trim whitespace
	trimmed := strings.TrimSpace(ruleName)

	// Check length
	if len(trimmed) < MinRuleNameLength {
		return &ValidationError{
			Entity:  "rule",
			Field:   ruleName,
			Message: "rule name cannot be empty",
		}
	}
	if len(trimmed) > MaxRuleNameLength {
		return &ValidationError{
			Entity:  "rule",
			Field:   ruleName,
			Message: fmt.Sprintf("rule name too long (max %d characters)", MaxRuleNameLength),
		}
	}

	// Check for non-printable characters
	for _, r := range trimmed {
		if !unicode.IsPrint(r) {
			return &ValidationError{
				Entity:  "rule",
				Field:   ruleName,
				Message: fmt.Sprintf("rule name contains non-printable character: %U", r),
			}
		}
	}

	// Check for only ASCII characters (optional, can be relaxed)
	for _, r := range trimmed {
		if r > unicode.MaxASCII {
			return &ValidationError{
				Entity:  "rule",
				Field:   ruleName,
				Message: fmt.Sprintf("rule name contains non-ASCII character: %U", r),
			}
		}
	}

	return nil
}

// Event represents the event data to evaluate against
type Event map[string]interface{}

// GetField retrieves a field value using dot notation
func (e Event) GetField(path string) (interface{}, error) {
	return getField(e, path, 0)
}

func getField(data interface{}, path string, depth int) (interface{}, error) {
	// Check depth limit to prevent stack overflow
	if depth > MaxFieldDepth {
		return "", &SecurityError{
			Type:    "field_depth_exceeded",
			Limit:   fmt.Sprintf("%d", MaxFieldDepth),
			Message: fmt.Sprintf("field access depth exceeded maximum of %d", MaxFieldDepth),
		}
	}

	if path == "" {
		return data, nil
	}

	// Handle quoted strings
	if len(path) >= 2 && ((path[0] == '"' && path[len(path)-1] == '"') ||
		(path[0] == '\'' && path[len(path)-1] == '\'')) {
		return path[1 : len(path)-1], nil
	}

	// Handle array indexing: proc.aname[2]
	// Extract the field name and index for later application
	arrayIndex := ""
	if bracketIdx := strings.Index(path, "["); bracketIdx != -1 && strings.HasSuffix(path, "]") {
		arrayIndex = path[bracketIdx+1 : len(path)-1]
		path = path[:bracketIdx] // Continue with the field path part (without [N])
	}

	// Convert Event to map[string]interface{} for type switch
	var v map[string]interface{}
	switch data := data.(type) {
	case map[string]interface{}:
		v = data
	case Event:
		v = map[string]interface{}(data)
	default:
		// For non-map types, apply array index if present and return
		if arrayIndex != "" {
			return applyArrayIndex(data, arrayIndex)
		}
		return data, nil
	}

	// First, try exact key match (for flat maps with dot-notation keys)
	if val, ok := v[path]; ok {
		// If we have an array index, apply it
		if arrayIndex != "" {
			return applyArrayIndex(val, arrayIndex)
		}
		return val, nil
	}

	// If not found, try to traverse as nested structure (only if path contains dots)
	// Try each dot position to find a valid nested path
	for i, c := range path {
		if c == '.' {
			key := path[:i]
			rest := path[i+1:]
			if val, ok := v[key]; ok {
				// Append array index back to rest for nested traversal
				if arrayIndex != "" {
					rest = rest + "[" + arrayIndex + "]"
				}
				nestedVal, err := getField(val, rest, depth+1)
				if err != nil {
					return nil, err
				}
				return nestedVal, nil
			}
			// Key not found at this dot position, continue to next dot
		}
	}
	// No match found, return empty string to allow evaluation to continue
	return "", nil
}

func applyArrayIndex(val interface{}, indexStr string) (interface{}, error) {
	// Parse the index
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return "", fmt.Errorf("invalid array index '%s': %w", indexStr, err)
	}

	// Reject negative indices
	if index < 0 {
		return "", fmt.Errorf("negative array index not allowed: %d", index)
	}

	// Handle different types
	switch v := val.(type) {
	case []interface{}:
		if index < len(v) {
			return v[index], nil
		}
		return "", fmt.Errorf("array index %d out of bounds (length: %d)", index, len(v))
	case []string:
		if index < len(v) {
			return v[index], nil
		}
		return "", fmt.Errorf("array index %d out of bounds (length: %d)", index, len(v))
	case map[string]interface{}:
		// If it's a map, try to convert to array index by string key
		key := strconv.Itoa(index)
		if val, ok := v[key]; ok {
			return val, nil
		}
		return "", fmt.Errorf("map key '%s' not found", key)
	}
	return "", fmt.Errorf("cannot apply array index to type %T", val)
}

// GetRule retrieves a rule by name
func (e *Engine) GetRule(ruleName string) (*Rule, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Validate rule name
	if err := ValidateRuleName(ruleName); err != nil {
		return nil, false
	}

	rule, ok := e.Rules[ruleName]
	return rule, ok
}

// UpdateRule updates an existing rule
func (e *Engine) UpdateRule(ruleName string, newRule *Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate rule name
	if err := ValidateRuleName(newRule.Rule); err != nil {
		return fmt.Errorf("invalid rule name: %w", err)
	}

	// Check if rule exists
	if _, ok := e.Rules[ruleName]; !ok {
		return fmt.Errorf("rule '%s' not found", ruleName)
	}

	// Validate rule name matches
	if newRule.Rule != ruleName {
		return fmt.Errorf("rule name mismatch: expected '%s', got '%s'", ruleName, newRule.Rule)
	}

	// Compile the new rule condition using the unified compilation function
	if err := e.compileRuleCondition(newRule); err != nil {
		return err
	}

	// Update the map
	e.Rules[ruleName] = newRule

	// Update the slice
	found := false
	for i, r := range e.RulesSlice {
		if r.Rule == ruleName {
			e.RulesSlice[i] = *newRule
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("rule '%s' found in map but not in slice - data inconsistency", ruleName)
	}

	// Update version
	e.Version++
	e.LastUpdated = time.Now()

	return nil
}

// DeleteRule deletes a rule by name
func (e *Engine) DeleteRule(ruleName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate rule name
	if err := ValidateRuleName(ruleName); err != nil {
		return fmt.Errorf("invalid rule name: %w", err)
	}

	// Check if rule exists
	if _, ok := e.Rules[ruleName]; !ok {
		return fmt.Errorf("rule '%s' not found", ruleName)
	}

	// Delete from map
	delete(e.Rules, ruleName)

	// Delete from slice
	found := false
	for i, r := range e.RulesSlice {
		if r.Rule == ruleName {
			e.RulesSlice = append(e.RulesSlice[:i], e.RulesSlice[i+1:]...)
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("rule '%s' found in map but not in slice - data inconsistency", ruleName)
	}

	// Update version
	e.Version++
	e.LastUpdated = time.Now()

	return nil
}

// AddRule adds a new rule
func (e *Engine) AddRule(rule *Rule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Validate rule name
	if err := ValidateRuleName(rule.Rule); err != nil {
		return fmt.Errorf("invalid rule name: %w", err)
	}

	// Check if rule already exists
	if _, ok := e.Rules[rule.Rule]; ok {
		return fmt.Errorf("rule '%s' already exists", rule.Rule)
	}

	// Compile the rule condition using the unified compilation function
	if err := e.compileRuleCondition(rule); err != nil {
		return err
	}

	// Add to map
	e.Rules[rule.Rule] = rule

	// Add to slice
	e.RulesSlice = append(e.RulesSlice, *rule)

	// Update version
	e.Version++
	e.LastUpdated = time.Now()

	return nil
}

// ListRules returns all rules
func (e *Engine) ListRules() []*Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := make([]*Rule, 0, len(e.RulesSlice))
	for i := range e.RulesSlice {
		rules = append(rules, &e.RulesSlice[i])
	}
	return rules
}

// GetVersion returns the engine version
func (e *Engine) GetVersion() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.Version
}

// GetLastUpdated returns the last update time
func (e *Engine) GetLastUpdated() time.Time {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.LastUpdated
}

// GetCachedFieldPath parses a field path with caching for better performance
func (e *Engine) GetCachedFieldPath(path string) []string {
	// Fast path: check cache
	e.fieldPathCacheMu.RLock()
	if parts, ok := e.fieldPathCache[path]; ok {
		e.fieldPathCacheMu.RUnlock()
		return parts
	}
	e.fieldPathCacheMu.RUnlock()

	// Parse path
	parts := parseFieldPath(path)

	// Cache result
	e.fieldPathCacheMu.Lock()
	e.fieldPathCache[path] = parts
	e.fieldPathCacheMu.Unlock()

	return parts
}

// parseFieldPath splits a dot-notation path into parts
func parseFieldPath(path string) []string {
	if path == "" {
		return nil
	}
	return strings.Split(path, ".")
}

// ClearRules clears all rules from the engine
func (e *Engine) ClearRules() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear map
	e.Rules = make(map[string]*Rule)

	// Clear slice
	e.RulesSlice = make([]Rule, 0)

	// Clear macro regex cache to prevent memory leaks
	e.macroRegexCache = make(map[string]*regexp.Regexp)

	// Clear regex cache to prevent memory leaks
	e.regexCacheMu.Lock()
	e.regexCache = make(map[string]*regexp.Regexp)
	e.regexCacheMu.Unlock()

	// Clear parse cache to prevent memory leaks
	e.parseCacheMu.Lock()
	e.parseCache = make(map[string]Expr)
	e.parseCacheMu.Unlock()

	// Clear field path cache to prevent memory leaks
	e.fieldPathCacheMu.Lock()
	e.fieldPathCache = make(map[string][]string)
	e.fieldPathCacheMu.Unlock()

	// Update version
	e.Version++
	e.LastUpdated = time.Now()

	return nil
}

// Reset resets the engine to its initial state
func (e *Engine) Reset() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Clear rules
	e.Rules = make(map[string]*Rule)
	e.RulesSlice = make([]Rule, 0)

	// Clear macros
	e.Macros = make(map[string]*Macro)

	// Clear lists
	e.Lists = make(map[string]*List)

	// Clear macro regex cache to prevent memory leaks
	e.macroRegexCache = make(map[string]*regexp.Regexp)

	// Clear regex cache to prevent memory leaks
	e.regexCacheMu.Lock()
	e.regexCache = make(map[string]*regexp.Regexp)
	e.regexCacheMu.Unlock()

	// Clear parse cache to prevent memory leaks
	e.parseCacheMu.Lock()
	e.parseCache = make(map[string]Expr)
	e.parseCacheMu.Unlock()

	// Clear field path cache to prevent memory leaks
	e.fieldPathCacheMu.Lock()
	e.fieldPathCache = make(map[string][]string)
	e.fieldPathCacheMu.Unlock()

	// Reset version
	e.Version = 0
	e.LastUpdated = time.Time{}

	return nil
}
