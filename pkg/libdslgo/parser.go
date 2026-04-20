package libdslgo

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ParseFile parses a YAML file containing rules, macros, and lists
func (e *Engine) ParseFile(data []byte) error {
	var items []map[string]interface{}
	if err := yaml.Unmarshal(data, &items); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}

	for _, item := range items {
		if rule, ok := item["rule"]; ok {
			ruleStr, ok := rule.(string)
			if !ok {
				return fmt.Errorf("rule name must be a string, got %T", rule)
			}
			r := Rule{
				Rule:      ruleStr,
				Desc:      getString(item, "desc"),
				Condition: getString(item, "condition"),
				Output:    getString(item, "output"),
				Priority:  getString(item, "priority"),
			}
			if tags, ok := item["tags"].([]interface{}); ok {
				for _, tag := range tags {
					if tagStr, ok := tag.(string); ok {
						r.Tags = append(r.Tags, tagStr)
					}
				}
			}
			// Handle enabled field
			if enabled, ok := item["enabled"]; ok {
				switch v := enabled.(type) {
				case bool:
					enabled := v
					r.Enabled = &enabled
				case string:
					enabled := v == "true"
					r.Enabled = &enabled
				}
			}
			// Store in both map and slice for backward compatibility
			e.mu.Lock()
			e.Rules[r.Rule] = &r
			e.RulesSlice = append(e.RulesSlice, r)
			e.mu.Unlock()
		}
		if macro, ok := item["macro"]; ok {
			macroStr, ok := macro.(string)
			if !ok {
				return fmt.Errorf("macro name must be a string, got %T", macro)
			}
			m := &Macro{
				Name:      macroStr,
				Condition: getString(item, "condition"),
			}
			// Pre-compile the macro expression for performance
			expr, err := ParseExpression(m.Condition)
			if err != nil {
				return fmt.Errorf("failed to compile macro '%s': %w", m.Name, err)
			}
			m.Expr = expr
			e.mu.Lock()
			e.Macros[m.Name] = m
			// Clear regex cache since macros have changed
			e.macroRegexCache = make(map[string]*regexp.Regexp)
			e.mu.Unlock()
		}
		if list, ok := item["list"]; ok {
			listStr, ok := list.(string)
			if !ok {
				return fmt.Errorf("list name must be a string, got %T", list)
			}
			l := &List{
				Name: listStr,
				Set:  make(map[string]bool),
			}
			if items, ok := item["items"].([]interface{}); ok {
				for _, item := range items {
					// Handle both string and integer values
					var itemStr string
					switch v := item.(type) {
					case string:
						itemStr = v
					case int:
						itemStr = fmt.Sprintf("%d", v)
					case float64:
						itemStr = fmt.Sprintf("%.0f", v)
					default:
						itemStr = fmt.Sprintf("%v", v)
					}
					l.Items = append(l.Items, itemStr)
					l.Set[itemStr] = true // Build hash set for O(1) lookup
				}
			}
			e.mu.Lock()
			e.Lists[l.Name] = l
			e.mu.Unlock()
		}
	}

	return nil
}

func getString(m map[string]interface{}, key string) string {
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return ""
}

// Token types for expression parsing
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenString
	TokenLParen
	TokenRParen
	TokenLBracket
	TokenRBracket
	TokenComma
	TokenEq
	TokenNotEq
	TokenGt
	TokenLt
	TokenGtEq
	TokenLtEq
	TokenRegexMatch
)

// Token represents a lexical token
type Token struct {
	Type  TokenType
	Value string
	Pos   *Position
}

type Position struct {
	Line   int
	Column int
	Offset int
}

// Lexer tokenizes the condition expression
type Lexer struct {
	input string
	pos   int
	line  int
	col   int
}

func NewLexer(input string) *Lexer {
	// Use strings.Builder for efficient string processing
	var builder strings.Builder
	builder.Grow(len(input))

	for i := 0; i < len(input); i++ {
		c := input[i]
		if c == '\n' || c == '\r' {
			builder.WriteByte(' ')
		} else {
			builder.WriteByte(c)
		}
	}

	cleaned := strings.TrimSpace(builder.String())
	return &Lexer{
		input: cleaned,
		line:  1,
		col:   1,
	}
}

func (l *Lexer) NextToken() Token {
	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF, Pos: &Position{Line: l.line, Column: l.col, Offset: l.pos}}
	}

	// Skip whitespace and newlines
	for l.pos < len(l.input) && (l.input[l.pos] == ' ' || l.input[l.pos] == '\n' || l.input[l.pos] == '\t' || l.input[l.pos] == '\r') {
		if l.input[l.pos] == '\n' {
			l.line++
			l.col = 1
		} else {
			l.col++
		}
		l.pos++
	}

	if l.pos >= len(l.input) {
		return Token{Type: TokenEOF}
	}

	// Check for operators
	remaining := l.input[l.pos:]
	if strings.HasPrefix(remaining, "and ") || remaining == "and" {
		l.pos += 3
		return Token{Type: TokenIdent, Value: "and", Pos: &Position{Line: l.line, Column: l.col - 3, Offset: l.pos - 3}}
	}
	if strings.HasPrefix(remaining, "or ") || remaining == "or" {
		l.pos += 2
		return Token{Type: TokenIdent, Value: "or", Pos: &Position{Line: l.line, Column: l.col - 2, Offset: l.pos - 2}}
	}
	if strings.HasPrefix(remaining, "not ") || remaining == "not" {
		l.pos += 3
		return Token{Type: TokenIdent, Value: "not", Pos: &Position{Line: l.line, Column: l.col - 3, Offset: l.pos - 3}}
	}
	if strings.HasPrefix(remaining, "contains ") {
		l.pos += 9 // "contains " including space
		return Token{Type: TokenIdent, Value: "contains", Pos: &Position{Line: l.line, Column: l.col - 9, Offset: l.pos - 9}}
	}
	if strings.HasPrefix(remaining, "endswith ") {
		l.pos += 9 // "endswith " including space
		return Token{Type: TokenIdent, Value: "endswith", Pos: &Position{Line: l.line, Column: l.col - 9, Offset: l.pos - 9}}
	}
	if strings.HasPrefix(remaining, "startswith ") {
		l.pos += 10 // "startswith " including space
		return Token{Type: TokenIdent, Value: "startswith", Pos: &Position{Line: l.line, Column: l.col - 10, Offset: l.pos - 10}}
	}
	if strings.HasPrefix(remaining, "in ") {
		l.pos += 3 // "in " including space
		return Token{Type: TokenIdent, Value: "in", Pos: &Position{Line: l.line, Column: l.col - 3, Offset: l.pos - 3}}
	}
	if strings.HasPrefix(remaining, "pmatch ") {
		l.pos += 7
		return Token{Type: TokenIdent, Value: "pmatch", Pos: &Position{Line: l.line, Column: l.col - 7, Offset: l.pos - 7}}
	}
	if strings.HasPrefix(remaining, "glob ") || remaining == "glob" {
		l.pos += 4
		return Token{Type: TokenIdent, Value: "glob", Pos: &Position{Line: l.line, Column: l.col - 4, Offset: l.pos - 4}}
	}
	if strings.HasPrefix(remaining, "exists ") || remaining == "exists" {
		l.pos += 6
		return Token{Type: TokenIdent, Value: "exists", Pos: &Position{Line: l.line, Column: l.col - 6, Offset: l.pos - 6}}
	}
	if strings.HasPrefix(remaining, "!=") {
		l.pos += 2
		return Token{Type: TokenNotEq, Value: "!=", Pos: &Position{Line: l.line, Column: l.col - 2, Offset: l.pos - 2}}
	}
	if strings.HasPrefix(remaining, "=~") {
		l.pos += 2
		return Token{Type: TokenRegexMatch, Value: "=~", Pos: &Position{Line: l.line, Column: l.col - 2, Offset: l.pos - 2}}
	}
	if strings.HasPrefix(remaining, ">=") {
		l.pos += 2
		return Token{Type: TokenGtEq, Value: ">=", Pos: &Position{Line: l.line, Column: l.col - 2, Offset: l.pos - 2}}
	}
	if strings.HasPrefix(remaining, "<=") {
		l.pos += 2
		return Token{Type: TokenLtEq, Value: "<=", Pos: &Position{Line: l.line, Column: l.col - 2, Offset: l.pos - 2}}
	}
	if strings.HasPrefix(remaining, "=") {
		l.pos++
		return Token{Type: TokenEq, Value: "=", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}
	if strings.HasPrefix(remaining, "<") {
		l.pos++
		return Token{Type: TokenLt, Value: "<", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}
	if strings.HasPrefix(remaining, ">") {
		l.pos++
		return Token{Type: TokenGt, Value: ">", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}
	if l.input[l.pos] == ',' {
		l.pos++
		return Token{Type: TokenComma, Value: ",", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}

	// Check for brackets for array indexing
	if l.input[l.pos] == '[' {
		l.pos++
		return Token{Type: TokenLBracket, Value: "[", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}
	if l.input[l.pos] == ']' {
		l.pos++
		return Token{Type: TokenRBracket, Value: "]", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}

	// Check for parentheses
	if l.input[l.pos] == '(' {
		l.pos++
		return Token{Type: TokenLParen, Value: "(", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}
	if l.input[l.pos] == ')' {
		l.pos++
		return Token{Type: TokenRParen, Value: ")", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}

	// Check for quoted strings
	if l.input[l.pos] == '"' || l.input[l.pos] == '\'' {
		quote := l.input[l.pos]
		l.pos++
		start := l.pos
		for l.pos < len(l.input) && l.input[l.pos] != quote {
			l.pos++
		}
		if l.pos >= len(l.input) {
			// Unclosed quote - return error token
			return Token{Type: TokenEOF, Value: "unclosed string quote"}
		}
		value := l.input[start:l.pos]
		l.pos++ // skip closing quote
		return Token{Type: TokenString, Value: value, Pos: &Position{Line: l.line, Column: l.col - len(value) - 2, Offset: l.pos - len(value) - 2}}
	}

	// Check for = operator (must be before path check for cases like fd.name=/path)
	if l.input[l.pos] == '=' {
		l.pos++
		return Token{Type: TokenEq, Value: "=", Pos: &Position{Line: l.line, Column: l.col - 1, Offset: l.pos - 1}}
	}

	// Check for unquoted paths starting with /
	if l.input[l.pos] == '/' {
		start := l.pos
		for l.pos < len(l.input) {
			c := l.input[l.pos]
			if c == ' ' || c == '\n' || c == '\t' || c == ')' || c == '(' {
				break
			}
			l.pos++
		}
		value := l.input[start:l.pos]
		return Token{Type: TokenString, Value: value, Pos: &Position{Line: l.line, Column: l.col - len(value), Offset: l.pos - len(value)}}
	}

	// Parse identifier or string - support dot notation for nested field access
	// Examples: container.privileged, evt.arg.mode, k8s.ns.name
	// Also support array indexing: proc.aname[2]
	start := l.pos
	startCol := l.col
	for l.pos < len(l.input) {
		c := l.input[l.pos]
		if c == ' ' || c == '\n' || c == '\r' || c == '\t' || c == '(' || c == ')' || c == ',' || c == '=' || c == '!' || c == '>' || c == '<' || c == '[' || c == ']' {
			break
		}
		// Allow dot for nested field access and brackets for array indexing
		l.pos++
		l.col++
	}

	value := l.input[start:l.pos]
	return Token{Type: TokenIdent, Value: value, Pos: &Position{Line: l.line, Column: startCol, Offset: start}}
}

// ExprType represents the type of expression node
type ExprType int

const (
	ExprTypeBoolean ExprType = iota
	ExprTypeComparison
	ExprTypeExists
	ExprTypeMacroRef
	ExprTypeFunction
	ExprTypeFieldRef
	ExprTypeStringLiteral
)

// Expression node types
type Expr interface {
	Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error)
	String() string
	Type() ExprType
	Clone() Expr
}

// BooleanExpr represents and/or/not operations
type BooleanExpr struct {
	Pos      *Position
	Operator string
	Left     Expr
	Right    Expr
}

func (e *BooleanExpr) String() string {
	if e.Right == nil {
		return fmt.Sprintf("not (%s)", e.Left.String())
	}
	return fmt.Sprintf("(%s %s %s)", e.Left.String(), e.Operator, e.Right.String())
}

func (e *BooleanExpr) Type() ExprType {
	return ExprTypeBoolean
}

func (e *BooleanExpr) Clone() Expr {
	return &BooleanExpr{
		Pos:      e.Pos,
		Operator: e.Operator,
		Left:     e.Left.Clone(),
		Right:    e.Right.Clone(),
	}
}

func (e *BooleanExpr) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	switch e.Operator {
	case "and":
		left, err := e.Left.Evaluate(ctx, event, engine)
		if err != nil {
			return false, err
		}
		if !left {
			return false, nil
		}
		return e.Right.Evaluate(ctx, event, engine)
	case "or":
		left, err := e.Left.Evaluate(ctx, event, engine)
		if err != nil {
			return false, err
		}
		if left {
			return true, nil
		}
		return e.Right.Evaluate(ctx, event, engine)
	case "not":
		val, err := e.Left.Evaluate(ctx, event, engine)
		if err != nil {
			return false, err
		}
		return !val, nil
	}
	return false, fmt.Errorf("unknown boolean operator: %s", e.Operator)
}

// ComparisonExpr represents comparison operations
type ComparisonExpr struct {
	Pos      *Position
	Operator string
	Left     string
	Right    interface{}
}

func (e *ComparisonExpr) String() string {
	return fmt.Sprintf("%s %s %v", e.Left, e.Operator, e.Right)
}

func (e *ComparisonExpr) Type() ExprType {
	return ExprTypeComparison
}

func (e *ComparisonExpr) Clone() Expr {
	return &ComparisonExpr{
		Pos:      e.Pos,
		Operator: e.Operator,
		Left:     e.Left,
		Right:    e.Right,
	}
}

func (e *ComparisonExpr) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	leftVal, err := event.GetField(e.Left)
	if err != nil {
		return false, err
	}

	leftStr := fmt.Sprintf("%v", leftVal)

	switch e.Operator {
	case "contains":
		rightStr := fmt.Sprintf("%v", e.Right)
		return strings.Contains(leftStr, rightStr), nil
	case "endswith":
		rightStr := fmt.Sprintf("%v", e.Right)
		return strings.HasSuffix(leftStr, rightStr), nil
	case "startswith":
		rightStr := fmt.Sprintf("%v", e.Right)
		return strings.HasPrefix(leftStr, rightStr), nil
	case "!=":
		rightStr := fmt.Sprintf("%v", e.Right)
		return leftStr != rightStr, nil
	case "=":
		rightStr := fmt.Sprintf("%v", e.Right)
		return leftStr == rightStr, nil
	case ">=":
		return compareNumeric(leftStr, e.Right, func(a, b float64) bool { return a >= b })
	case "<=":
		return compareNumeric(leftStr, e.Right, func(a, b float64) bool { return a <= b })
	case ">":
		return compareNumeric(leftStr, e.Right, func(a, b float64) bool { return a > b })
	case "<":
		return compareNumeric(leftStr, e.Right, func(a, b float64) bool { return a < b })
	case "pmatch":
		// Pattern match - similar to in but with wildcard support
		if listName, ok := e.Right.(string); ok && engine != nil {
			if listItems, exists := engine.GetList(listName); exists {
				for _, pattern := range listItems {
					if patternMatch(leftStr, pattern) {
						return true, nil
					}
				}
				return false, nil
			}
		}
		if list, ok := e.Right.([]string); ok {
			for _, pattern := range list {
				if patternMatch(leftStr, pattern) {
					return true, nil
				}
			}
			return false, nil
		}
		return false, fmt.Errorf("right side of 'pmatch' must be a list or list reference")
	case "in":
		// Check if right side is a list reference (string) or actual list
		if listName, ok := e.Right.(string); ok && engine != nil {
			// Try to get the list from the engine
			engine.mu.RLock()
			list, exists := engine.Lists[listName]
			engine.mu.RUnlock()
			if exists {
				// Use hash set for O(1) lookup if available
				if list.Set != nil {
					return list.Set[leftStr], nil
				}
				// Fall back to linear search
				for _, item := range list.Items {
					if leftStr == item {
						return true, nil
					}
				}
				return false, nil
			}
		}
		// Fall back to direct list comparison
		if list, ok := e.Right.([]string); ok {
			for _, item := range list {
				if leftStr == item {
					return true, nil
				}
			}
			return false, nil
		}
		return false, fmt.Errorf("right side of 'in' must be a list or list reference")
	case "glob":
		// Glob pattern matching
		rightStr := fmt.Sprintf("%v", e.Right)
		return patternMatch(leftStr, rightStr), nil
	case "=~":
		// Regex match using Go's regexp package with Engine-level caching and timeout protection
		if engine == nil {
			// Fallback to direct compilation if no engine
			rightStr := fmt.Sprintf("%v", e.Right)
			re, err := regexp.Compile(rightStr)
			if err != nil {
				return false, fmt.Errorf("invalid regex pattern '%s': %w", rightStr, err)
			}
			return matchWithTimeout(re, leftStr, MaxRegexMatchTime)
		}
		rightStr := fmt.Sprintf("%v", e.Right)
		re, err := engine.getCompiledRegex(rightStr)
		if err != nil {
			return false, err
		}
		return matchWithTimeout(re, leftStr, MaxRegexMatchTime)
	case "exists":
		// Check if field exists (not empty)
		return leftStr != "", nil
	}
	return false, fmt.Errorf("unknown comparison operator: %s", e.Operator)
}

// matchWithTimeout executes regex match with a timeout to prevent ReDoS attacks
func matchWithTimeout(re *regexp.Regexp, str string, timeout time.Duration) (bool, error) {
	resultChan := make(chan bool, 1)

	// Execute match in a goroutine
	go func() {
		resultChan <- re.MatchString(str)
	}()

	// Wait for result or timeout
	select {
	case result := <-resultChan:
		return result, nil
	case <-time.After(timeout):
		return false, fmt.Errorf("regex match timeout exceeded (%v)", timeout)
	}
}

// compareNumeric compares two numeric values
func compareNumeric(leftStr string, right interface{}, cmp func(a, b float64) bool) (bool, error) {
	leftFloat, err := parseFloat(leftStr)
	if err != nil {
		return false, err
	}

	rightStr := fmt.Sprintf("%v", right)
	rightFloat, err := parseFloat(rightStr)
	if err != nil {
		return false, err
	}

	return cmp(leftFloat, rightFloat), nil
}

// parseFloat converts a string to float64
func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// patternMatch checks if a string matches a pattern with wildcard support
func patternMatch(str, pattern string) bool {
	// Simple wildcard matching - * matches any sequence, ? matches single character
	if strings.HasSuffix(pattern, "/") {
		return strings.HasPrefix(str, pattern)
	}
	// Handle * wildcard
	if strings.Contains(pattern, "*") {
		return globMatch(str, pattern)
	}
	// For exact match
	return str == pattern
}

// globMatch implements simple glob matching with * wildcard
func globMatch(str, pattern string) bool {
	// Split by * and check each part appears in order
	parts := strings.Split(pattern, "*")
	if len(parts) == 1 {
		return str == pattern
	}

	// Empty pattern parts from consecutive * or leading/trailing *
	// Filter out empty parts for sequential matching
	remaining := str

	for i, part := range parts {
		if part == "" {
			continue
		}
		idx := strings.Index(remaining, part)
		if idx < 0 {
			return false
		}
		// First part must match at the start
		if i == 0 && idx != 0 {
			return false
		}
		// Last part must match at the end
		if i == len(parts)-1 && !strings.HasSuffix(remaining, part) {
			return false
		}
		// Move past this match
		remaining = remaining[idx+len(part):]
	}

	return true
}

// ExistsExpr represents a field existence check (field exists)
type ExistsExpr struct {
	Pos   *Position
	Field string
}

func (e *ExistsExpr) String() string {
	return fmt.Sprintf("exists(%s)", e.Field)
}

func (e *ExistsExpr) Type() ExprType {
	return ExprTypeExists
}

func (e *ExistsExpr) Clone() Expr {
	return &ExistsExpr{
		Pos:   e.Pos,
		Field: e.Field,
	}
}

func (e *ExistsExpr) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	value, err := event.GetField(e.Field)
	if err != nil {
		return false, nil
	}
	valueStr := fmt.Sprintf("%v", value)
	return valueStr != "" && valueStr != "<NA>" && valueStr != "N/A", nil
}

// MacroRef represents a reference to a macro
type MacroRef struct {
	Pos  *Position
	Name string
}

func (e *MacroRef) String() string {
	return fmt.Sprintf("@%s", e.Name)
}

func (e *MacroRef) Type() ExprType {
	return ExprTypeMacroRef
}

func (e *MacroRef) Clone() Expr {
	return &MacroRef{
		Pos:  e.Pos,
		Name: e.Name,
	}
}

// FieldRef represents a field reference (for function arguments)
type FieldRef struct {
	Pos   *Position
	Field string
}

func (e *FieldRef) String() string {
	return e.Field
}

func (e *FieldRef) Type() ExprType {
	return ExprTypeFieldRef
}

func (e *FieldRef) Clone() Expr {
	return &FieldRef{
		Pos:   e.Pos,
		Field: e.Field,
	}
}

func (e *FieldRef) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// Field references should not be evaluated as boolean expressions
	// They are used only as function arguments
	return false, fmt.Errorf("field reference cannot be evaluated as boolean expression")
}

// StringLiteral represents a string literal (for function arguments)
type StringLiteral struct {
	Pos   *Position
	Value string
}

func (e *StringLiteral) String() string {
	return fmt.Sprintf("\"%s\"", e.Value)
}

func (e *StringLiteral) Type() ExprType {
	return ExprTypeStringLiteral
}

func (e *StringLiteral) Clone() Expr {
	return &StringLiteral{
		Pos:   e.Pos,
		Value: e.Value,
	}
}

func (e *StringLiteral) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// String literals should not be evaluated as boolean expressions
	// They are used only as function arguments
	return false, fmt.Errorf("string literal cannot be evaluated as boolean expression")
}

// FunctionExpr represents a function call
type FunctionExpr struct {
	Pos  *Position
	Name string
	Args []Expr
}

func (e *FunctionExpr) String() string {
	args := make([]string, len(e.Args))
	for i, arg := range e.Args {
		args[i] = arg.String()
	}
	return fmt.Sprintf("%s(%s)", e.Name, strings.Join(args, ", "))
}

func (e *FunctionExpr) Type() ExprType {
	return ExprTypeFunction
}

func (e *FunctionExpr) Clone() Expr {
	args := make([]Expr, len(e.Args))
	for i, arg := range e.Args {
		args[i] = arg.Clone()
	}
	return &FunctionExpr{
		Pos:  e.Pos,
		Name: e.Name,
		Args: args,
	}
}

func (e *FunctionExpr) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	// Get the function from registry
	fn, ok := engine.Functions.Get(e.Name)
	if !ok {
		return false, fmt.Errorf("function '%s' not found", e.Name)
	}

	// Evaluate arguments (handle FieldRef and StringLiteral specially)
	args := make([]interface{}, len(e.Args))
	for i, arg := range e.Args {
		switch node := arg.(type) {
		case *FieldRef:
			// Field reference - get value from event
			val, err := event.GetField(node.Field)
			if err != nil {
				return false, fmt.Errorf("failed to get field '%s': %w", node.Field, err)
			}
			args[i] = val
		case *StringLiteral:
			// String literal - use value directly
			args[i] = node.Value
		default:
			// Other expression types - evaluate normally
			val, err := arg.Evaluate(ctx, event, engine)
			if err != nil {
				return false, fmt.Errorf("failed to evaluate argument %d: %w", i, err)
			}
			args[i] = val
		}
	}

	// Call the function
	result, err := fn.Func(ctx, args, event, engine)
	if err != nil {
		return false, fmt.Errorf("function '%s' failed: %w", e.Name, err)
	}

	// Convert result to bool
	switch v := result.(type) {
	case bool:
		return v, nil
	case int:
		return v != 0, nil
	case float64:
		return v != 0, nil
	case string:
		return v != "" && v != "false", nil
	default:
		return fmt.Sprintf("%v", v) != "", nil
	}
}

func (e *MacroRef) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	// Check context cancellation
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}

	macro, ok := engine.Macros[e.Name]
	if !ok {
		return false, fmt.Errorf("macro '%s' not found", e.Name)
	}
	// Use cached expression if available, otherwise parse on-the-fly
	var expr Expr
	if macro.Expr != nil {
		expr = macro.Expr
	} else {
		var err error
		expr, err = ParseExpression(macro.Condition)
		if err != nil {
			return false, fmt.Errorf("failed to parse macro condition: %w", err)
		}
	}
	return expr.Evaluate(ctx, event, engine)
}

// DefaultMaxRecursionDepth is the default maximum recursion depth for the parser
const DefaultMaxRecursionDepth = 100

// Parser parses the expression
type Parser struct {
	lexer    *Lexer
	curr     Token
	peek     Token
	depth    int // current recursion depth
	maxDepth int // maximum allowed recursion depth
}

func NewParser(input string) *Parser {
	return NewParserWithDepth(input, DefaultMaxRecursionDepth)
}

func NewParserWithDepth(input string, maxDepth int) *Parser {
	lexer := NewLexer(input)
	p := &Parser{
		lexer:    lexer,
		maxDepth: maxDepth,
	}
	p.curr = p.lexer.NextToken()
	p.peek = p.lexer.NextToken()
	return p
}

func (p *Parser) checkDepth() error {
	if p.depth >= p.maxDepth {
		return &SecurityError{
			Type:    "recursion_depth_exceeded",
			Limit:   fmt.Sprintf("%d", p.maxDepth),
			Message: fmt.Sprintf("expression too complex: maximum recursion depth exceeded"),
		}
	}
	return nil
}

func (p *Parser) advance() {
	p.curr = p.peek
	p.peek = p.lexer.NextToken()
}

func (p *Parser) Parse() (Expr, error) {
	return p.parseOr()
}

func (p *Parser) parseOr() (Expr, error) {
	if err := p.checkDepth(); err != nil {
		return nil, err
	}
	p.depth++
	defer func() { p.depth-- }()

	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}

	for p.curr.Type == TokenIdent && p.curr.Value == "or" {
		opPos := p.curr.Pos
		op := p.curr.Value
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = &BooleanExpr{Pos: opPos, Operator: op, Left: left, Right: right}
	}

	return left, nil
}

func (p *Parser) parseAnd() (Expr, error) {
	if err := p.checkDepth(); err != nil {
		return nil, err
	}
	p.depth++
	defer func() { p.depth-- }()

	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}

	for p.curr.Type == TokenIdent && p.curr.Value == "and" {
		opPos := p.curr.Pos
		op := p.curr.Value
		p.advance()
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = &BooleanExpr{Pos: opPos, Operator: op, Left: left, Right: right}
	}

	return left, nil
}

func (p *Parser) parseNot() (Expr, error) {
	if p.curr.Type == TokenIdent && p.curr.Value == "not" {
		if err := p.checkDepth(); err != nil {
			return nil, err
		}
		p.depth++
		defer func() { p.depth-- }()

		opPos := p.curr.Pos
		op := p.curr.Value
		p.advance()
		expr, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return &BooleanExpr{Pos: opPos, Operator: op, Left: expr, Right: nil}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	if p.curr.Type == TokenLParen {
		p.advance()
		expr, err := p.Parse()
		if err != nil {
			return nil, err
		}
		if p.curr.Type != TokenRParen {
			return nil, &ParseError{
				Pos:     p.curr.Pos,
				Message: fmt.Sprintf("expected ')', got token %v", p.curr),
			}
		}
		p.advance()
		return expr, nil
	}

	if p.curr.Type == TokenIdent {
		identPos := p.curr.Pos
		ident := p.curr.Value
		p.advance()

		// Check for function call: ident followed by (
		if p.curr.Type == TokenLParen {
			return p.parseFunctionCall(ident, identPos)
		}

		// Check for exists operator (unary operator)
		if p.curr.Type == TokenIdent && p.curr.Value == "exists" {
			p.advance()
			return &ExistsExpr{Pos: identPos, Field: ident}, nil
		}

		// Check for comparison operators (contains, endswith, startswith, !=, =, >=, <=, >, <, glob, =~)
		isComparison := false
		switch p.curr.Value {
		case "contains", "endswith", "startswith":
			isComparison = true
		case "!=", "=", ">=", "<=", ">", "<", "glob", "=~":
			isComparison = true
		}
		if isComparison {
			opPos := p.curr.Pos
			op := p.curr.Value
			p.advance()
			right := p.parseValue()
			return &ComparisonExpr{Pos: opPos, Operator: op, Left: ident, Right: right}, nil
		}

		// If not a valid operator, return error
		if p.curr.Type == TokenIdent {
			return nil, &ParseError{
				Pos:     p.curr.Pos,
				Message: fmt.Sprintf("unknown operator '%s'", p.curr.Value),
			}
		}

		// Check for list reference (ident in (list_name)) or inline list (ident in (val1,val2,val3))
		if p.curr.Value == "in" || p.curr.Value == "pmatch" {
			opPos := p.curr.Pos
			op := p.curr.Value
			p.advance()
			// Expect opening parenthesis
			if p.curr.Type != TokenLParen {
				return nil, &ParseError{
					Pos:     p.curr.Pos,
					Message: fmt.Sprintf("expected '(' after '%s' operator", op),
				}
			}
			p.advance()
			// Parse comma-separated values or single list name
			var items []string
			for p.curr.Type != TokenRParen && p.curr.Type != TokenEOF {
				if p.curr.Type == TokenIdent || p.curr.Type == TokenString {
					items = append(items, p.curr.Value)
					p.advance()
					// Check for comma separator
					if p.curr.Type == TokenComma {
						p.advance()
					}
				} else {
					p.advance()
				}
			}
			// Check for unexpected EOF (missing closing paren)
			if p.curr.Type == TokenEOF {
				return nil, &ParseError{
					Message: "unexpected end of expression, expected ')'",
				}
			}
			// Expect closing parenthesis
			if p.curr.Type != TokenRParen {
				return nil, &ParseError{
					Pos:     p.curr.Pos,
					Message: "expected ')' after list values",
				}
			}
			p.advance()
			// If only one item, treat as list reference
			if len(items) == 1 {
				return &ComparisonExpr{Pos: opPos, Operator: op, Left: ident, Right: items[0]}, nil
			}
			// Otherwise, treat as inline list
			return &ComparisonExpr{Pos: opPos, Operator: op, Left: ident, Right: items}, nil
		}

		// Treat as macro reference only if it doesn't contain dots (field paths)
		// Identifiers with dots are field paths, not macro references
		if strings.Contains(ident, ".") || strings.Contains(ident, "/") {
			// This is a field path, treat as comparison with implicit equality
			// This shouldn't normally happen in well-formed conditions, but handle it gracefully
			return &ComparisonExpr{Pos: identPos, Operator: "=", Left: ident, Right: ""}, nil
		}

		return &MacroRef{Pos: identPos, Name: ident}, nil
	}

	if p.curr.Type == TokenString {
		// Quoted string - treat as a plain value (compare against evt.type)
		valuePos := p.curr.Pos
		value := p.curr.Value
		p.advance()
		return &MacroRef{Pos: valuePos, Name: value}, nil
	}

	return nil, &ParseError{
		Pos:     p.curr.Pos,
		Message: fmt.Sprintf("unexpected token: %v", p.curr),
	}
}

// parseFunctionCall parses a function call expression like len(field)
func (p *Parser) parseFunctionCall(funcName string, funcPos *Position) (Expr, error) {
	// Skip opening parenthesis
	p.advance()

	// Parse function arguments (field references or string literals)
	var args []Expr
	for p.curr.Type != TokenRParen && p.curr.Type != TokenEOF {
		var arg Expr

		if p.curr.Type == TokenString {
			// String literal
			arg = &StringLiteral{Pos: p.curr.Pos, Value: p.curr.Value}
			p.advance()
		} else if p.curr.Type == TokenIdent {
			// Field reference
			arg = &FieldRef{Pos: p.curr.Pos, Field: p.curr.Value}
			p.advance()
		} else {
			return nil, &ParseError{
				Pos:     p.curr.Pos,
				Message: fmt.Sprintf("function argument must be field reference or string literal, got %v", p.curr),
			}
		}

		args = append(args, arg)

		// Check for comma separator
		if p.curr.Type == TokenComma {
			p.advance()
		}
	}

	// Check for unexpected EOF
	if p.curr.Type == TokenEOF {
		return nil, &ParseError{
			Message: "unexpected end of expression, expected ')' after function call",
		}
	}

	// Skip closing parenthesis
	p.advance()

	return &FunctionExpr{
		Pos:  funcPos,
		Name: funcName,
		Args: args,
	}, nil
}

func (p *Parser) parseValue() interface{} {
	switch p.curr.Type {
	case TokenIdent, TokenString:
		val := p.curr.Value
		p.advance()
		return val
	case TokenEOF:
		// Return empty string for EOF to allow graceful handling
		return ""
	default:
		// For other token types, return the value but advance to prevent infinite loops
		val := p.curr.Value
		p.advance()
		return val
	}
}

// ParseExpression is a convenience function to parse an expression
func ParseExpression(input string) (Expr, error) {
	parser := NewParser(input)
	return parser.Parse()
}
