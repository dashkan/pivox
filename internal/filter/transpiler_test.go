package filter

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranspile_EmptyFilter(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, "", 1)
	require.NoError(t, err)
	assert.Equal(t, "", wc.SQL)
	assert.Empty(t, wc.Args)
}

func TestTranspile_SimpleEquals(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `state = "ACTIVE"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `state = $1`, wc.SQL)
	assert.Equal(t, []any{"ACTIVE"}, wc.Args)
}

func TestTranspile_WildcardILIKE(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `displayName = "Test*"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `display_name ILIKE $1 ESCAPE '\'`, wc.SQL)
	assert.Equal(t, []any{"Test%"}, wc.Args)
}

func TestTranspile_WildcardEscapesMetachars(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `displayName = "100%_done*"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `display_name ILIKE $1 ESCAPE '\'`, wc.SQL)
	assert.Equal(t, []any{`100\%\_done%`}, wc.Args)
}

// TestTranspile_WildcardEscapesBackslash pins the LIKE escaping contract for
// the '*' wildcard operand. A caller-supplied '\' must match LITERALLY, not be
// consumed as the LIKE escape character. The generated fragment therefore
// escapes '\' → '\\' and carries an explicit `ESCAPE '\'` so the escaping is
// honored regardless of the driver default. '*' still becomes a SQL '%'
// wildcard, and '%'/'_' stay literal.
func TestTranspile_WildcardEscapesBackslash(t *testing.T) {
	rf := SpaceFilter()
	// Filter source `"a\\b*"` parses (CEL unescape) to StringValue `a\b*`.
	wc, err := Transpile(rf, `displayName = "a\\b*"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `display_name ILIKE $1 ESCAPE '\'`, wc.SQL)
	// `\` → `\\`, then `*` → `%`.
	assert.Equal(t, []any{`a\\b%`}, wc.Args)
}

// TestTranspile_WildcardEscapesAllSpecials exercises the full ordering: the
// three LIKE literals ('\', '%', '_') are escaped BEFORE the '*'→'%' wildcard
// translation, so the wildcard '%' is not itself escaped.
func TestTranspile_WildcardEscapesAllSpecials(t *testing.T) {
	rf := SpaceFilter()
	// Filter source `"\\%_*"` parses to StringValue `\%_*`.
	wc, err := Transpile(rf, `displayName = "\\%_*"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `display_name ILIKE $1 ESCAPE '\'`, wc.SQL)
	// `\`→`\\`, `%`→`\%`, `_`→`\_`, then `*`→`%`.
	assert.Equal(t, []any{`\\\%\_%`}, wc.Args)
}

func TestTranspile_NoWildcardOnNonPartialField(t *testing.T) {
	rf := SpaceFilter()
	// state does not have AllowPartial, so wildcard is treated literally.
	wc, err := Transpile(rf, `state = "ACT*"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `state = $1`, wc.SQL)
	assert.Equal(t, []any{"ACT*"}, wc.Args)
}

func TestTranspile_AND(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `displayName = "Test*" AND state = "ACTIVE"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(display_name ILIKE $1 ESCAPE '\' AND state = $2)`, wc.SQL)
	assert.Equal(t, []any{"Test%", "ACTIVE"}, wc.Args)
}

func TestTranspile_OR(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `state = "ACTIVE" OR state = "DELETE_REQUESTED"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(state = $1 OR state = $2)`, wc.SQL)
	assert.Equal(t, []any{"ACTIVE", "DELETE_REQUESTED"}, wc.Args)
}

func TestTranspile_NOT(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `NOT state = "DELETE_REQUESTED"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(NOT state = $1)`, wc.SQL)
	assert.Equal(t, []any{"DELETE_REQUESTED"}, wc.Args)
}

func TestTranspile_NotEquals(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `state != "DELETE_REQUESTED"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `state != $1`, wc.SQL)
	assert.Equal(t, []any{"DELETE_REQUESTED"}, wc.Args)
}

func TestTranspile_ComparisonOperators(t *testing.T) {
	tests := []struct {
		name     string
		filter   string
		wantSQL  string
		wantArgs []any
	}{
		{
			name:     "less than timestamp",
			filter:   `createTime < timestamp("2024-06-01T00:00:00Z")`,
			wantSQL:  `create_time < $1`,
			wantArgs: []any{time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)},
		},
		{
			name:     "greater than timestamp",
			filter:   `createTime > timestamp("2024-01-01T00:00:00Z")`,
			wantSQL:  `create_time > $1`,
			wantArgs: []any{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
		{
			name:     "less equals",
			filter:   `createTime <= timestamp("2024-12-31T23:59:59Z")`,
			wantSQL:  `create_time <= $1`,
			wantArgs: []any{time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)},
		},
		{
			name:     "greater equals",
			filter:   `createTime >= timestamp("2024-01-01T00:00:00Z")`,
			wantSQL:  `create_time >= $1`,
			wantArgs: []any{time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := SpaceFilter()
			wc, err := Transpile(rf, tt.filter, 1)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSQL, wc.SQL)
			assert.Equal(t, tt.wantArgs, wc.Args)
		})
	}
}

func TestTranspile_HasOperator_StringContains(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `displayName : "test"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `display_name ILIKE $1`, wc.SQL)
	assert.Equal(t, []any{"%test%"}, wc.Args)
}

func TestTranspile_HasOperator_JSONBKeyExists(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `labels : "env"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `labels ? $1`, wc.SQL)
	assert.Equal(t, []any{"env"}, wc.Args)
}

func TestTranspile_JSONB_DotTraversal_Equals(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `labels.env = "production"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `labels->>$1 = $2`, wc.SQL)
	assert.Equal(t, []any{"env", "production"}, wc.Args)
}

func TestTranspile_JSONB_DotTraversal_Has(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `labels.env : "prod"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `labels->>$1 ILIKE $2`, wc.SQL)
	assert.Equal(t, []any{"env", "%prod%"}, wc.Args)
}

// JSONB key injection guard — see github.com/dashkan/pivox/issues/23.
// The einride parser accepts STRING tokens after DOT, so a quoted key
// like `labels."x' OR '1'='1"` previously interpolated raw into SQL.
// All three transpiler sites now validate + parameterize JSONB keys.
func TestTranspile_JSONBKey_RejectsSQLInjection(t *testing.T) {
	rf := SpaceFilter()
	cases := []string{
		`labels."x' OR '1'='1":foo`,                // transpileHasSelect path
		`labels."x' OR '1'='1" = "foo"`,            // resolveField (comparison) path
		"labels.\"x'); DROP TABLE spaces;--\":foo", // statement terminator
		`labels."x->>'y" = "z"`,                    // operator smuggling
		`labels."" = "z"`,                          // empty key
		`labels."1leading" = "z"`,                  // non-identifier start
		`labels."has space" = "z"`,                 // whitespace
	}
	for _, f := range cases {
		t.Run(f, func(t *testing.T) {
			_, err := Transpile(rf, f, 1)
			require.Error(t, err, "filter %q must be rejected", f)
			assert.Contains(t, err.Error(), "invalid jsonb key")
		})
	}
}

func TestTranspile_JSONBKey_AcceptsValidIdentifiers(t *testing.T) {
	rf := SpaceFilter()
	cases := []string{
		`labels.env = "prod"`,
		`labels._private = "x"`,
		`labels.env_2 = "x"`,
		`labels.ENV = "x"`,
	}
	for _, f := range cases {
		t.Run(f, func(t *testing.T) {
			_, err := Transpile(rf, f, 1)
			require.NoError(t, err)
		})
	}
}

func TestTranspile_Complex(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `displayName = "My*" AND (state = "ACTIVE" OR name = "proj-123")`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(display_name ILIKE $1 ESCAPE '\' AND (state = $2 OR name = $3))`, wc.SQL)
	assert.Equal(t, []any{"My%", "ACTIVE", "proj-123"}, wc.Args)
}

func TestTranspile_StartIdx(t *testing.T) {
	rf := SpaceFilter()
	// Simulate prior params by starting at $3.
	wc, err := Transpile(rf, `state = "ACTIVE"`, 3)
	require.NoError(t, err)
	assert.Equal(t, `state = $3`, wc.SQL)
	assert.Equal(t, []any{"ACTIVE"}, wc.Args)
}

func TestTranspile_UnknownField(t *testing.T) {
	rf := SpaceFilter()
	_, err := Transpile(rf, `unknownField = "x"`, 1)
	require.Error(t, err)
}

func TestTranspile_BareLiteral(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `news`, 1)
	require.NoError(t, err)
	assert.Equal(t, `display_name ILIKE $1`, wc.SQL)
	assert.Equal(t, []any{"%news%"}, wc.Args)
}

func TestTranspile_BareLiteral_MultipleDefaultFields(t *testing.T) {
	rf := TagKeyFilter()
	wc, err := Transpile(rf, `compute`, 1)
	require.NoError(t, err)
	assert.Equal(t, `short_name ILIKE $1`, wc.SQL)
	assert.Equal(t, []any{"%compute%"}, wc.Args)
}

func TestTranspile_BareLiteralWithImplicitAnd(t *testing.T) {
	rf := SpaceFilter()
	// Two bare words = implicit AND, each expanded against default fields.
	wc, err := Transpile(rf, `news article`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(display_name ILIKE $1 AND display_name ILIKE $2)`, wc.SQL)
	assert.Equal(t, []any{"%news%", "%article%"}, wc.Args)
}

func TestTranspile_BareLiteralAndCondition(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `news AND state = "ACTIVE"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(display_name ILIKE $1 AND state = $2)`, wc.SQL)
	assert.Equal(t, []any{"%news%", "ACTIVE"}, wc.Args)
}

func TestTranspile_BareLiteralValue_InComparison(t *testing.T) {
	rf := SpaceFilter()
	// Bare value on RHS: state = ACTIVE (without quotes).
	wc, err := Transpile(rf, `state = ACTIVE`, 1)
	require.NoError(t, err)
	assert.Equal(t, `state = $1`, wc.SQL)
	assert.Equal(t, []any{"ACTIVE"}, wc.Args)
}

func TestTranspile_NotBareLiteral(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `NOT news`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(NOT display_name ILIKE $1)`, wc.SQL)
	assert.Equal(t, []any{"%news%"}, wc.Args)
}

func TestTranspile_BareLiteralMixedWithStructured(t *testing.T) {
	rf := SpaceFilter()
	wc, err := Transpile(rf, `myproject AND labels.env = production`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(display_name ILIKE $1 AND labels->>$2 = $3)`, wc.SQL)
	assert.Equal(t, []any{"%myproject%", "env", "production"}, wc.Args)
}

func TestTranspile_OrganizationFilter(t *testing.T) {
	rf := OrganizationFilter()
	wc, err := Transpile(rf, `displayName = "Acme*" AND state = "ACTIVE"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `(display_name ILIKE $1 ESCAPE '\' AND state = $2)`, wc.SQL)
	assert.Equal(t, []any{"Acme%", "ACTIVE"}, wc.Args)
}
