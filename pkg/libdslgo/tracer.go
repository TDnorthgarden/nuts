package libdslgo

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// TraceEntry represents a single evaluation trace entry
type TraceEntry struct {
	Timestamp time.Time
	Expr      string
	ExprType  ExprType
	Result    bool
	Error     error
	Depth     int
	Duration  time.Duration
}

// Tracer records evaluation traces
type Tracer struct {
	mu      sync.Mutex
	entries []TraceEntry
	enabled bool
}

// NewTracer creates a new tracer
func NewTracer() *Tracer {
	return &Tracer{
		entries: make([]TraceEntry, 0),
		enabled: false,
	}
}

// Enable enables tracing
func (t *Tracer) Enable() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enabled = true
}

// Disable disables tracing
func (t *Tracer) Disable() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.enabled = false
}

// IsEnabled returns whether tracing is enabled
func (t *Tracer) IsEnabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enabled
}

// Record records a trace entry
func (t *Tracer) Record(expr string, exprType ExprType, result bool, err error, depth int, duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.enabled {
		return
	}
	t.entries = append(t.entries, TraceEntry{
		Timestamp: time.Now(),
		Expr:      expr,
		ExprType:  exprType,
		Result:    result,
		Error:     err,
		Depth:     depth,
		Duration:  duration,
	})
}

// GetEntries returns all trace entries
func (t *Tracer) GetEntries() []TraceEntry {
	t.mu.Lock()
	defer t.mu.Unlock()
	entries := make([]TraceEntry, len(t.entries))
	copy(entries, t.entries)
	return entries
}

// Clear clears all trace entries
func (t *Tracer) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.entries = make([]TraceEntry, 0)
}

// String returns a string representation of the trace
func (t *Tracer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.entries) == 0 {
		return "No trace entries"
	}

	var builder strings.Builder
	builder.WriteString("Evaluation Trace:\n")
	builder.WriteString("==================\n")

	for i, entry := range t.entries {
		indent := strings.Repeat("  ", entry.Depth)
		resultStr := "false"
		if entry.Result {
			resultStr = "true"
		}
		builder.WriteString(fmt.Sprintf("[%d] %s%s => %s (%.2fms)", i+1, indent, entry.Expr, resultStr, float64(entry.Duration.Microseconds())/1000))
		if entry.Error != nil {
			builder.WriteString(fmt.Sprintf(" ERROR: %v", entry.Error))
		}
		builder.WriteString("\n")
	}

	return builder.String()
}

// TracedExpr wraps an expression with tracing capability
type TracedExpr struct {
	Expr   Expr
	Tracer *Tracer
	Depth  int
}

func (e *TracedExpr) Evaluate(ctx context.Context, event Event, engine *Engine) (bool, error) {
	if e.Tracer == nil || !e.Tracer.IsEnabled() {
		return e.Expr.Evaluate(ctx, event, engine)
	}

	start := time.Now()
	result, err := e.Expr.Evaluate(ctx, event, engine)
	duration := time.Since(start)

	e.Tracer.Record(e.Expr.String(), e.Expr.Type(), result, err, e.Depth, duration)

	return result, err
}

func (e *TracedExpr) String() string {
	return e.Expr.String()
}

func (e *TracedExpr) Type() ExprType {
	return e.Expr.Type()
}

func (e *TracedExpr) Clone() Expr {
	return &TracedExpr{
		Expr:   e.Expr.Clone(),
		Tracer: e.Tracer,
		Depth:  e.Depth,
	}
}

// WrapWithTrace wraps an expression with a tracer
func WrapWithTrace(expr Expr, tracer *Tracer, depth int) Expr {
	if tracer == nil {
		return expr
	}

	switch node := expr.(type) {
	case *BooleanExpr:
		return &BooleanExpr{
			Operator: node.Operator,
			Left:     WrapWithTrace(node.Left, tracer, depth+1),
			Right:    WrapWithTrace(node.Right, tracer, depth+1),
		}
	case *ComparisonExpr:
		return &TracedExpr{
			Expr:   node,
			Tracer: tracer,
			Depth:  depth,
		}
	case *ExistsExpr:
		return &TracedExpr{
			Expr:   node,
			Tracer: tracer,
			Depth:  depth,
		}
	case *MacroRef:
		return &TracedExpr{
			Expr:   node,
			Tracer: tracer,
			Depth:  depth,
		}
	default:
		return expr
	}
}
