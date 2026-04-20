package libdslgo

import (
	"fmt"
	"strings"
)

// ToDOT generates a DOT graph representation of the expression AST
func ToDOT(expr Expr) string {
	var builder strings.Builder
	builder.WriteString("digraph AST {\n")
	builder.WriteString("\tnode [shape=box, style=rounded];\n")
	builder.WriteString("\trankdir=TB;\n\n")

	nodeID := 0
	ids := make(map[Expr]string)

	var generateDOT func(Expr, string) string
	generateDOT = func(e Expr, parentID string) string {
		nodeID++
		id := fmt.Sprintf("node%d", nodeID)
		ids[e] = id

		label := ""
		shape := "box"
		color := "lightblue"

		switch node := e.(type) {
		case *BooleanExpr:
			label = fmt.Sprintf("BooleanExpr\\nOperator: %s", node.Operator)
			if node.Operator == "not" {
				shape = "ellipse"
				color = "lightyellow"
			} else {
				shape = "diamond"
				color = "lightgreen"
			}
		case *ComparisonExpr:
			label = fmt.Sprintf("ComparisonExpr\\n%s %s %v", node.Left, node.Operator, node.Right)
			shape = "box"
			color = "lightcoral"
		case *ExistsExpr:
			label = fmt.Sprintf("ExistsExpr\\nField: %s", node.Field)
			shape = "ellipse"
			color = "lightpink"
		case *MacroRef:
			label = fmt.Sprintf("MacroRef\\nName: %s", node.Name)
			shape = "ellipse"
			color = "lavender"
		}

		builder.WriteString(fmt.Sprintf("\t%s [label=\"%s\", shape=%s, fillcolor=%s, style=filled];\n", id, label, shape, color))

		if parentID != "" {
			builder.WriteString(fmt.Sprintf("\t%s -> %s;\n", parentID, id))
		}

		switch node := e.(type) {
		case *BooleanExpr:
			if node.Left != nil {
				generateDOT(node.Left, id)
			}
			if node.Right != nil {
				generateDOT(node.Right, id)
			}
		}

		return id
	}

	generateDOT(expr, "")

	builder.WriteString("}")
	return builder.String()
}

// StringDOT returns a simplified DOT representation as a string
func StringDOT(expr Expr) string {
	return ToDOT(expr)
}
