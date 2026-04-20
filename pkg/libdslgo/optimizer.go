package libdslgo

import (
	"fmt"
)

// ConstantFolder performs constant folding on AST
type ConstantFolder struct{}

// NewConstantFolder creates a new constant folder
func NewConstantFolder() *ConstantFolder {
	return &ConstantFolder{}
}

// Fold performs constant folding on an expression
func (f *ConstantFolder) Fold(expr Expr) Expr {
	if expr == nil {
		return nil
	}
	return f.foldExpr(expr)
}

// foldExpr recursively folds an expression
func (f *ConstantFolder) foldExpr(expr Expr) Expr {
	switch node := expr.(type) {
	case *BooleanExpr:
		return f.foldBoolean(node)
	case *ComparisonExpr:
		return f.foldComparison(node)
	case *ExistsExpr:
		return f.foldExists(node)
	case *MacroRef:
		return f.foldMacroRef(node)
	case *FunctionExpr:
		return f.foldFunction(node)
	case *FieldRef:
		return f.foldFieldRef(node)
	case *StringLiteral:
		return f.foldStringLiteral(node)
	default:
		return expr
	}
}

// foldBoolean folds BooleanExpr nodes
func (f *ConstantFolder) foldBoolean(expr *BooleanExpr) Expr {
	// Recursively fold children
	if expr.Left != nil {
		expr.Left = f.foldExpr(expr.Left)
	}
	if expr.Right != nil {
		expr.Right = f.foldExpr(expr.Right)
	}
	return expr
}

// foldComparison folds ComparisonExpr nodes
func (f *ConstantFolder) foldComparison(expr *ComparisonExpr) Expr {
	// Try to fold if both operands are constants
	if isStringConstant(expr.Right) {
		return f.foldStringComparison(expr)
	}
	return expr
}

// foldStringComparison folds string comparisons with constant right operand
func (f *ConstantFolder) foldStringComparison(expr *ComparisonExpr) Expr {
	// For now, we can't fold field-based comparisons because we don't know the field value
	// This would require type system support (P4-4)
	return expr
}

// foldExists folds ExistsExpr nodes
func (f *ConstantFolder) foldExists(expr *ExistsExpr) Expr {
	return expr
}

// foldMacroRef folds MacroRef nodes
func (f *ConstantFolder) foldMacroRef(expr *MacroRef) Expr {
	return expr
}

// foldFieldRef folds FieldRef nodes
func (f *ConstantFolder) foldFieldRef(expr *FieldRef) Expr {
	return expr
}

// foldStringLiteral folds StringLiteral nodes
func (f *ConstantFolder) foldStringLiteral(expr *StringLiteral) Expr {
	return expr
}

// foldFunction folds FunctionExpr nodes
func (f *ConstantFolder) foldFunction(expr *FunctionExpr) Expr {
	// Recursively fold arguments
	for i, arg := range expr.Args {
		expr.Args[i] = f.foldExpr(arg)
	}

	// Try to fold if all arguments are constants
	if f.canFoldFunction(expr) {
		return f.foldConstantFunction(expr)
	}
	return expr
}

// canFoldFunction checks if a function can be folded (all args are constants)
func (f *ConstantFolder) canFoldFunction(expr *FunctionExpr) bool {
	for _, arg := range expr.Args {
		if !isConstant(arg) {
			return false
		}
	}
	return true
}

// foldConstantFunction folds a function call with constant arguments
func (f *ConstantFolder) foldConstantFunction(expr *FunctionExpr) Expr {
	// For now, we can't fold functions without a proper constant representation
	// This would require type system support (P4-4)
	return expr
}

// Helper functions

// isStringConstant checks if a value is a string constant
func isStringConstant(v interface{}) bool {
	_, ok := v.(string)
	return ok
}

// isConstant checks if an expression is a constant
func isConstant(expr Expr) bool {
	// For now, we don't have a way to represent constant literals
	// This would require type system support (P4-4)
	return false
}

// DeadCodeEliminator performs dead code elimination on AST (Stage 1: Simple optimizations)
type DeadCodeEliminator struct{}

// NewDeadCodeEliminator creates a new dead code eliminator
func NewDeadCodeEliminator() *DeadCodeEliminator {
	return &DeadCodeEliminator{}
}

// Optimizer combines multiple optimization passes
type Optimizer struct {
	constantFolder     *ConstantFolder
	deadCodeEliminator *DeadCodeEliminator
}

// NewOptimizer creates a new optimizer
func NewOptimizer() *Optimizer {
	return &Optimizer{
		constantFolder:     NewConstantFolder(),
		deadCodeEliminator: NewDeadCodeEliminator(),
	}
}

// Optimize performs all optimization passes on an expression
func (o *Optimizer) Optimize(expr Expr) Expr {
	if expr == nil {
		return nil
	}

	// Pass 1: Constant folding
	expr = o.constantFolder.Fold(expr)

	// Pass 2: Dead code elimination
	expr = o.deadCodeEliminator.Optimize(expr)

	return expr
}

// Optimize performs dead code elimination on an expression
func (d *DeadCodeEliminator) Optimize(expr Expr) Expr {
	if expr == nil {
		return nil
	}
	return d.optimizeExpr(expr)
}

// optimizeExpr recursively optimizes an expression
func (d *DeadCodeEliminator) optimizeExpr(expr Expr) Expr {
	switch node := expr.(type) {
	case *BooleanExpr:
		return d.optimizeBoolean(node)
	case *ComparisonExpr:
		return d.optimizeComparison(node)
	case *ExistsExpr:
		return d.optimizeExists(node)
	case *MacroRef:
		return d.optimizeMacroRef(node)
	case *FunctionExpr:
		return d.optimizeFunction(node)
	case *FieldRef:
		return d.optimizeFieldRef(node)
	case *StringLiteral:
		return d.optimizeStringLiteral(node)
	default:
		return expr
	}
}

// optimizeBoolean optimizes BooleanExpr nodes
func (d *DeadCodeEliminator) optimizeBoolean(expr *BooleanExpr) Expr {
	// Recursively optimize children
	if expr.Left != nil {
		expr.Left = d.optimizeExpr(expr.Left)
	}
	if expr.Right != nil {
		expr.Right = d.optimizeExpr(expr.Right)
	}

	switch expr.Operator {
	case "and":
		return d.optimizeAnd(expr)
	case "or":
		return d.optimizeOr(expr)
	case "not":
		return d.optimizeNot(expr)
	}
	return expr
}

// optimizeAnd optimizes AND expressions
func (d *DeadCodeEliminator) optimizeAnd(expr *BooleanExpr) Expr {
	// false and x -> false
	if isConstantFalse(expr.Left) {
		return expr.Left
	}
	// true and x -> x
	if isConstantTrue(expr.Left) {
		return expr.Right
	}
	// x and false -> false
	if isConstantFalse(expr.Right) {
		return expr.Right
	}
	// x and true -> x
	if isConstantTrue(expr.Right) {
		return expr.Left
	}
	// x and x -> x (duplicate elimination)
	if expressionsEqual(expr.Left, expr.Right) {
		return expr.Left
	}
	return expr
}

// optimizeOr optimizes OR expressions
func (d *DeadCodeEliminator) optimizeOr(expr *BooleanExpr) Expr {
	// true or x -> true
	if isConstantTrue(expr.Left) {
		return expr.Left
	}
	// false or x -> x
	if isConstantFalse(expr.Left) {
		return expr.Right
	}
	// x or true -> true
	if isConstantTrue(expr.Right) {
		return expr.Right
	}
	// x or false -> x
	if isConstantFalse(expr.Right) {
		return expr.Left
	}
	// x or x -> x (duplicate elimination)
	if expressionsEqual(expr.Left, expr.Right) {
		return expr.Left
	}
	return expr
}

// optimizeNot optimizes NOT expressions
func (d *DeadCodeEliminator) optimizeNot(expr *BooleanExpr) Expr {
	// not true -> false
	if isConstantTrue(expr.Left) {
		return &BooleanExpr{
			Operator: "and",
			Left:     expr.Left,
			Right:    expr.Left, // false and false = false
		}
	}
	// not false -> true
	if isConstantFalse(expr.Left) {
		return &BooleanExpr{
			Operator: "or",
			Left:     expr.Left,
			Right:    expr.Left, // false or false = false, but we want true
		}
	}
	// not not x -> x (double negation elimination)
	if inner, ok := expr.Left.(*BooleanExpr); ok && inner.Operator == "not" {
		return inner.Left
	}
	return expr
}

// optimizeComparison optimizes ComparisonExpr nodes
func (d *DeadCodeEliminator) optimizeComparison(expr *ComparisonExpr) Expr {
	// For now, just return as-is
	// Stage 2 optimizations (constant folding) would go here
	return expr
}

// optimizeExists optimizes ExistsExpr nodes
func (d *DeadCodeEliminator) optimizeExists(expr *ExistsExpr) Expr {
	// For now, just return as-is
	return expr
}

// optimizeMacroRef optimizes MacroRef nodes
func (d *DeadCodeEliminator) optimizeMacroRef(expr *MacroRef) Expr {
	// For now, just return as-is
	// Macros are expanded before optimization
	return expr
}

// optimizeFieldRef optimizes FieldRef nodes
func (d *DeadCodeEliminator) optimizeFieldRef(expr *FieldRef) Expr {
	return expr
}

// optimizeStringLiteral optimizes StringLiteral nodes
func (d *DeadCodeEliminator) optimizeStringLiteral(expr *StringLiteral) Expr {
	return expr
}

// optimizeFunction optimizes FunctionExpr nodes
func (d *DeadCodeEliminator) optimizeFunction(expr *FunctionExpr) Expr {
	// Recursively optimize arguments
	for i, arg := range expr.Args {
		expr.Args[i] = d.optimizeExpr(arg)
	}
	return expr
}

// Helper functions

// isConstantTrue checks if an expression is a constant true
func isConstantTrue(expr Expr) bool {
	if expr == nil {
		return false
	}
	// Check if it's a comparison that's always true
	// For now, we don't have a way to represent constant true/false literals
	// This would require type system support (P4-4)
	return false
}

// isConstantFalse checks if an expression is a constant false
func isConstantFalse(expr Expr) bool {
	if expr == nil {
		return false
	}
	// Check if it's a comparison that's always false
	// For now, we don't have a way to represent constant true/false literals
	// This would require type system support (P4-4)
	return false
}

// expressionsEqual checks if two expressions are structurally equal
func expressionsEqual(a, b Expr) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	// Check types first
	if a.Type() != b.Type() {
		return false
	}

	switch nodeA := a.(type) {
	case *BooleanExpr:
		nodeB, ok := b.(*BooleanExpr)
		if !ok {
			return false
		}
		return nodeA.Operator == nodeB.Operator &&
			expressionsEqual(nodeA.Left, nodeB.Left) &&
			expressionsEqual(nodeA.Right, nodeB.Right)
	case *ComparisonExpr:
		nodeB, ok := b.(*ComparisonExpr)
		if !ok {
			return false
		}
		return nodeA.Operator == nodeB.Operator &&
			nodeA.Left == nodeB.Left &&
			valuesEqual(nodeA.Right, nodeB.Right)
	case *ExistsExpr:
		nodeB, ok := b.(*ExistsExpr)
		if !ok {
			return false
		}
		return nodeA.Field == nodeB.Field
	case *MacroRef:
		nodeB, ok := b.(*MacroRef)
		if !ok {
			return false
		}
		return nodeA.Name == nodeB.Name
	case *FunctionExpr:
		nodeB, ok := b.(*FunctionExpr)
		if !ok {
			return false
		}
		if nodeA.Name != nodeB.Name || len(nodeA.Args) != len(nodeB.Args) {
			return false
		}
		for i := range nodeA.Args {
			if !expressionsEqual(nodeA.Args[i], nodeB.Args[i]) {
				return false
			}
		}
		return true
	case *FieldRef:
		nodeB, ok := b.(*FieldRef)
		if !ok {
			return false
		}
		return nodeA.Field == nodeB.Field
	case *StringLiteral:
		nodeB, ok := b.(*StringLiteral)
		if !ok {
			return false
		}
		return nodeA.Value == nodeB.Value
	default:
		return false
	}
}

// valuesEqual checks if two values are equal
func valuesEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
