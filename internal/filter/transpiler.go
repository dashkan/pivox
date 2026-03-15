package filter

import (
	"fmt"
	"strings"
	"time"

	"go.einride.tech/aip/filtering"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// WhereClause is the output of the transpiler.
type WhereClause struct {
	SQL  string // e.g. "(display_name ILIKE $1 AND state = $2)"
	Args []any  // positional args
}

// Transpiler converts a parsed AIP-160 filter AST into a PostgreSQL WHERE clause.
type Transpiler struct {
	filter   *ResourceFilter
	args     []any
	startIdx int // offset for $N placeholders (1-based)
}

// Transpile converts an AIP-160 filter string into a parameterized SQL WHERE clause.
// startIdx is the 1-based offset for $N placeholders (e.g. pass 1 if no prior params).
//
// Uses the einride Parser directly (without the Checker) so that bare identifiers
// like "news" or "ACTIVE" are accepted per the AIP-160 EBNF grammar without
// requiring them to be pre-declared. The transpiler handles unknown identifiers
// as bare literal values.
func Transpile(rf *ResourceFilter, filterStr string, startIdx int) (*WhereClause, error) {
	if filterStr == "" {
		return &WhereClause{}, nil
	}

	// Use the Parser directly to get the raw AST. This skips the Checker
	// which would reject undeclared identifiers like bare text values.
	var parser filtering.Parser
	parser.Init(filterStr)
	parsedExpr, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	root := parsedExpr.GetExpr()
	if root == nil {
		return &WhereClause{}, nil
	}

	t := &Transpiler{
		filter:   rf,
		startIdx: startIdx,
	}

	sql, err := t.transpileExpr(root)
	if err != nil {
		return nil, err
	}

	return &WhereClause{SQL: sql, Args: t.args}, nil
}

func (t *Transpiler) nextParam(value any) string {
	t.args = append(t.args, value)
	return fmt.Sprintf("$%d", t.startIdx+len(t.args)-1)
}

func (t *Transpiler) transpileExpr(e *expr.Expr) (string, error) {
	switch v := e.GetExprKind().(type) {
	case *expr.Expr_CallExpr:
		return t.transpileCall(v.CallExpr)
	case *expr.Expr_IdentExpr:
		return t.transpileIdent(v.IdentExpr.GetName())
	case *expr.Expr_ConstExpr:
		return t.transpileConst(v.ConstExpr)
	case *expr.Expr_SelectExpr:
		return t.transpileSelect(v.SelectExpr)
	default:
		return "", fmt.Errorf("unsupported expression type: %T", v)
	}
}

func (t *Transpiler) transpileCall(call *expr.Expr_Call) (string, error) {
	switch call.GetFunction() {
	case filtering.FunctionAnd, filtering.FunctionFuzzyAnd:
		return t.transpileBinary(call, "AND")
	case filtering.FunctionOr:
		return t.transpileBinary(call, "OR")
	case filtering.FunctionNot:
		return t.transpileNot(call)
	case filtering.FunctionEquals:
		return t.transpileComparison(call, "=")
	case filtering.FunctionNotEquals:
		return t.transpileComparison(call, "!=")
	case filtering.FunctionLessThan:
		return t.transpileComparison(call, "<")
	case filtering.FunctionLessEquals:
		return t.transpileComparison(call, "<=")
	case filtering.FunctionGreaterThan:
		return t.transpileComparison(call, ">")
	case filtering.FunctionGreaterEquals:
		return t.transpileComparison(call, ">=")
	case filtering.FunctionHas:
		return t.transpileHas(call)
	case filtering.FunctionTimestamp:
		return t.transpileTimestamp(call)
	default:
		return "", fmt.Errorf("unsupported function: %s", call.GetFunction())
	}
}

func (t *Transpiler) transpileBinary(call *expr.Expr_Call, op string) (string, error) {
	args := call.GetArgs()
	if len(args) != 2 {
		return "", fmt.Errorf("%s requires 2 arguments, got %d", op, len(args))
	}
	lhs, err := t.transpileExpr(args[0])
	if err != nil {
		return "", err
	}
	rhs, err := t.transpileExpr(args[1])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(%s %s %s)", lhs, op, rhs), nil
}

func (t *Transpiler) transpileNot(call *expr.Expr_Call) (string, error) {
	args := call.GetArgs()
	if len(args) != 1 {
		return "", fmt.Errorf("NOT requires 1 argument, got %d", len(args))
	}
	inner, err := t.transpileExpr(args[0])
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("(NOT %s)", inner), nil
}

func (t *Transpiler) transpileComparison(call *expr.Expr_Call, op string) (string, error) {
	args := call.GetArgs()
	if len(args) != 2 {
		return "", fmt.Errorf("%s requires 2 arguments", op)
	}

	lhs := args[0]
	rhs := args[1]

	// Resolve the left-hand side to a column.
	column, fm, err := t.resolveField(lhs)
	if err != nil {
		return "", err
	}

	// Resolve the right-hand value.
	value, err := t.resolveValue(rhs)
	if err != nil {
		return "", err
	}

	// Wildcard handling for = operator on AllowPartial fields.
	if op == "=" && fm.AllowPartial {
		if strVal, ok := value.(string); ok && strings.Contains(strVal, "*") {
			// Escape existing SQL LIKE metacharacters, then replace * with %.
			escaped := strings.ReplaceAll(strVal, "%", "\\%")
			escaped = strings.ReplaceAll(escaped, "_", "\\_")
			escaped = strings.ReplaceAll(escaped, "*", "%")
			param := t.nextParam(escaped)
			return fmt.Sprintf("%s ILIKE %s", column, param), nil
		}
	}

	param := t.nextParam(value)
	return fmt.Sprintf("%s %s %s", column, op, param), nil
}

func (t *Transpiler) transpileHas(call *expr.Expr_Call) (string, error) {
	args := call.GetArgs()
	if len(args) != 2 {
		return "", fmt.Errorf(": requires 2 arguments")
	}

	lhs := args[0]
	rhs := args[1]

	// Check if the left side is a select expression (labels.key).
	if sel, ok := lhs.GetExprKind().(*expr.Expr_SelectExpr); ok {
		return t.transpileHasSelect(sel.SelectExpr, rhs)
	}

	column, fm, err := t.resolveField(lhs)
	if err != nil {
		return "", err
	}

	value, err := t.resolveValue(rhs)
	if err != nil {
		return "", err
	}

	if fm.JSONB {
		// JSONB key existence: labels ? 'key'
		param := t.nextParam(value)
		return fmt.Sprintf("%s ? %s", column, param), nil
	}

	// Regular string: contains check.
	strVal := fmt.Sprintf("%%%v%%", value)
	param := t.nextParam(strVal)
	return fmt.Sprintf("%s ILIKE %s", column, param), nil
}

// transpileHasSelect handles `labels.key:value` expressions.
func (t *Transpiler) transpileHasSelect(sel *expr.Expr_Select, rhs *expr.Expr) (string, error) {
	_, fm, err := t.resolveField(sel.GetOperand())
	if err != nil {
		return "", err
	}
	if !fm.JSONB {
		return "", fmt.Errorf("%s does not support traversal", sel.GetField())
	}

	key := sel.GetField()
	value, err := t.resolveValue(rhs)
	if err != nil {
		return "", err
	}

	// labels->>'key' ILIKE '%value%'
	strVal := fmt.Sprintf("%%%v%%", value)
	param := t.nextParam(strVal)
	return fmt.Sprintf("%s->>'%s' ILIKE %s", fm.Column, key, param), nil
}

func (t *Transpiler) transpileTimestamp(call *expr.Expr_Call) (string, error) {
	args := call.GetArgs()
	if len(args) != 1 {
		return "", fmt.Errorf("timestamp() requires 1 argument")
	}
	value, err := t.resolveValue(args[0])
	if err != nil {
		return "", err
	}
	strVal, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("timestamp() argument must be a string")
	}
	ts, err := time.Parse(time.RFC3339, strVal)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp: %w", err)
	}
	return t.nextParam(ts), nil
}

func (t *Transpiler) transpileIdent(name string) (string, error) {
	// Known field: resolve to column name.
	if fm, ok := t.filter.Fields[name]; ok {
		return fm.Column, nil
	}
	// Unknown identifier: treat as a bare literal and expand against default fields.
	return t.expandBareLiteral(name)
}

func (t *Transpiler) transpileConst(c *expr.Constant) (string, error) {
	switch v := c.GetConstantKind().(type) {
	case *expr.Constant_StringValue:
		return t.expandBareLiteral(v.StringValue)
	case *expr.Constant_Int64Value:
		return t.nextParam(v.Int64Value), nil
	case *expr.Constant_DoubleValue:
		return t.nextParam(v.DoubleValue), nil
	case *expr.Constant_BoolValue:
		return t.nextParam(v.BoolValue), nil
	default:
		return "", fmt.Errorf("unsupported constant type: %T", v)
	}
}

func (t *Transpiler) transpileSelect(sel *expr.Expr_Select) (string, error) {
	_, fm, err := t.resolveField(sel.GetOperand())
	if err != nil {
		return "", err
	}
	if !fm.JSONB {
		return "", fmt.Errorf("%s does not support traversal", sel.GetField())
	}
	// e.g. labels->>'env'
	return fmt.Sprintf("%s->>'%s'", fm.Column, sel.GetField()), nil
}

// resolveField extracts the column name and FieldMapping from an expression.
func (t *Transpiler) resolveField(e *expr.Expr) (string, FieldMapping, error) {
	switch v := e.GetExprKind().(type) {
	case *expr.Expr_IdentExpr:
		name := v.IdentExpr.GetName()
		fm, ok := t.filter.Fields[name]
		if !ok {
			return "", FieldMapping{}, fmt.Errorf("unknown field: %s", name)
		}
		return fm.Column, fm, nil
	case *expr.Expr_SelectExpr:
		// Dot-traversal, e.g. labels.key → labels->>'key'
		_, fm, err := t.resolveField(v.SelectExpr.GetOperand())
		if err != nil {
			return "", FieldMapping{}, err
		}
		if !fm.JSONB {
			return "", FieldMapping{}, fmt.Errorf("%s does not support traversal", v.SelectExpr.GetOperand().GetIdentExpr().GetName())
		}
		col := fmt.Sprintf("%s->>'%s'", fm.Column, v.SelectExpr.GetField())
		return col, FieldMapping{Column: col, Type: filtering.TypeString}, nil
	default:
		return "", FieldMapping{}, fmt.Errorf("expected field identifier, got %T", v)
	}
}

// resolveValue extracts a Go value from a constant or function-call expression.
func (t *Transpiler) resolveValue(e *expr.Expr) (any, error) {
	switch v := e.GetExprKind().(type) {
	case *expr.Expr_ConstExpr:
		return constToValue(v.ConstExpr)
	case *expr.Expr_CallExpr:
		// Handle timestamp("...") and duration("...")
		if v.CallExpr.GetFunction() == filtering.FunctionTimestamp {
			args := v.CallExpr.GetArgs()
			if len(args) != 1 {
				return nil, fmt.Errorf("timestamp() requires 1 argument")
			}
			inner, err := t.resolveValue(args[0])
			if err != nil {
				return nil, err
			}
			strVal, ok := inner.(string)
			if !ok {
				return nil, fmt.Errorf("timestamp() argument must be a string")
			}
			ts, err := time.Parse(time.RFC3339, strVal)
			if err != nil {
				return nil, fmt.Errorf("invalid timestamp: %w", err)
			}
			return ts, nil
		}
		return nil, fmt.Errorf("unsupported function in value position: %s", v.CallExpr.GetFunction())
	case *expr.Expr_IdentExpr:
		// Treat bare ident as string value (e.g. ACTIVE in state = ACTIVE)
		return v.IdentExpr.GetName(), nil
	default:
		return nil, fmt.Errorf("expected value, got %T", v)
	}
}

func constToValue(c *expr.Constant) (any, error) {
	switch v := c.GetConstantKind().(type) {
	case *expr.Constant_StringValue:
		return v.StringValue, nil
	case *expr.Constant_Int64Value:
		return v.Int64Value, nil
	case *expr.Constant_DoubleValue:
		return v.DoubleValue, nil
	case *expr.Constant_BoolValue:
		return v.BoolValue, nil
	default:
		return nil, fmt.Errorf("unsupported constant type: %T", v)
	}
}

// expandBareLiteral expands a bare literal against the resource's default fields.
func (t *Transpiler) expandBareLiteral(value string) (string, error) {
	if len(t.filter.DefaultFields) == 0 {
		return "", fmt.Errorf("bare literal %q not supported: no default fields configured", value)
	}
	clauses := make([]string, 0, len(t.filter.DefaultFields))
	for _, fieldName := range t.filter.DefaultFields {
		fm, ok := t.filter.Fields[fieldName]
		if !ok {
			continue
		}
		param := t.nextParam(fmt.Sprintf("%%%s%%", value))
		clauses = append(clauses, fmt.Sprintf("%s ILIKE %s", fm.Column, param))
	}
	if len(clauses) == 0 {
		return "", fmt.Errorf("no default fields found for bare literal %q", value)
	}
	if len(clauses) == 1 {
		return clauses[0], nil
	}
	return fmt.Sprintf("(%s)", strings.Join(clauses, " OR ")), nil
}
