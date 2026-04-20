package libdslgo

import (
	"context"
	"fmt"
	"sync"
)

// Function represents a custom function that can be called in expressions
type Function struct {
	Name     string
	Func     FunctionFunc
	MinArgs  int
	MaxArgs  int
	Variadic bool
}

// FunctionFunc is the signature for custom functions
type FunctionFunc func(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error)

// FunctionRegistry manages registered functions
type FunctionRegistry struct {
	mu        sync.RWMutex
	functions map[string]*Function
}

// NewFunctionRegistry creates a new function registry
func NewFunctionRegistry() *FunctionRegistry {
	return &FunctionRegistry{
		functions: make(map[string]*Function),
	}
}

// Register registers a custom function
func (r *FunctionRegistry) Register(fn *Function) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if fn.Name == "" {
		return fmt.Errorf("function name cannot be empty")
	}

	if _, exists := r.functions[fn.Name]; exists {
		return fmt.Errorf("function '%s' already registered", fn.Name)
	}

	r.functions[fn.Name] = fn
	return nil
}

// Get retrieves a function by name
func (r *FunctionRegistry) Get(name string) (*Function, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	fn, ok := r.functions[name]
	return fn, ok
}

// Unregister removes a function
func (r *FunctionRegistry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.functions, name)
}

// List returns all registered function names
func (r *FunctionRegistry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.functions))
	for name := range r.functions {
		names = append(names, name)
	}
	return names
}

// RegisterBuiltinFunctions registers built-in functions
func (r *FunctionRegistry) RegisterBuiltinFunctions() {
	// String functions
	r.Register(&Function{
		Name:    "len",
		Func:    builtinLen,
		MinArgs: 1,
		MaxArgs: 1,
	})

	r.Register(&Function{
		Name:    "upper",
		Func:    builtinUpper,
		MinArgs: 1,
		MaxArgs: 1,
	})

	r.Register(&Function{
		Name:    "lower",
		Func:    builtinLower,
		MinArgs: 1,
		MaxArgs: 1,
	})

	r.Register(&Function{
		Name:    "trim",
		Func:    builtinTrim,
		MinArgs: 1,
		MaxArgs: 1,
	})

	// Numeric functions
	r.Register(&Function{
		Name:    "abs",
		Func:    builtinAbs,
		MinArgs: 1,
		MaxArgs: 1,
	})

	r.Register(&Function{
		Name:    "min",
		Func:    builtinMin,
		MinArgs: 2,
		MaxArgs: -1, // variadic
		Variadic: true,
	})

	r.Register(&Function{
		Name:    "max",
		Func:    builtinMax,
		MinArgs: 2,
		MaxArgs: -1, // variadic
		Variadic: true,
	})
}

// Built-in function implementations
func builtinLen(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	switch v := args[0].(type) {
	case string:
		return len(v), nil
	case []interface{}:
		return len(v), nil
	case []string:
		return len(v), nil
	default:
		return 0, fmt.Errorf("len() argument must be string or array, got %T", args[0])
	}
}

func builtinUpper(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("upper() argument must be string, got %T", args[0])
	}
	return toUpper(s), nil
}

func builtinLower(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("lower() argument must be string, got %T", args[0])
	}
	return toLower(s), nil
}

func builtinTrim(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	s, ok := args[0].(string)
	if !ok {
		return nil, fmt.Errorf("trim() argument must be string, got %T", args[0])
	}
	return trimSpace(s), nil
}

func builtinAbs(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	switch v := args[0].(type) {
	case int:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	case float64:
		if v < 0 {
			return -v, nil
		}
		return v, nil
	default:
		return 0, fmt.Errorf("abs() argument must be number, got %T", args[0])
	}
}

func builtinMin(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("min() requires at least 2 arguments")
	}

	minVal := args[0]
	for _, arg := range args[1:] {
		if compareValues(arg, minVal) < 0 {
			minVal = arg
		}
	}
	return minVal, nil
}

func builtinMax(ctx context.Context, args []interface{}, event Event, engine *Engine) (interface{}, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("max() requires at least 2 arguments")
	}

	maxVal := args[0]
	for _, arg := range args[1:] {
		if compareValues(arg, maxVal) > 0 {
			maxVal = arg
		}
	}
	return maxVal, nil
}

// Helper functions for string operations (avoiding strings package to reduce imports)
func toUpper(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c = c - ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && s[start] == ' ' {
		start++
	}
	end := len(s)
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

func compareValues(a, b interface{}) int {
	// Simple comparison for numeric values
	switch va := a.(type) {
	case int:
		switch vb := b.(type) {
		case int:
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		case float64:
			if float64(va) < vb {
				return -1
			} else if float64(va) > vb {
				return 1
			}
			return 0
		}
	case float64:
		switch vb := b.(type) {
		case int:
			if va < float64(vb) {
				return -1
			} else if va > float64(vb) {
				return 1
			}
			return 0
		case float64:
			if va < vb {
				return -1
			} else if va > vb {
				return 1
			}
			return 0
		}
	}
	return 0
}
