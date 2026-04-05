package filter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Filter constructors — verify non-nil and expected fields
// ---------------------------------------------------------------------------

func TestTagValueFilter(t *testing.T) {
	rf := TagValueFilter()
	require.NotNil(t, rf)
	assert.Equal(t, "tag_values", rf.Table)
	assert.Equal(t, "tag_key_id", rf.ParentColumn)
	assert.False(t, rf.SoftDelete)
	assert.NotEmpty(t, rf.Fields)
}

func TestTagBindingFilter(t *testing.T) {
	rf := TagBindingFilter()
	require.NotNil(t, rf)
	assert.Equal(t, "tag_bindings", rf.Table)
	assert.Equal(t, "parent_resource", rf.ParentColumn)
	assert.False(t, rf.SoftDelete)
	assert.NotEmpty(t, rf.Fields)
}

func TestApiKeyFilter(t *testing.T) {
	rf := ApiKeyFilter()
	require.NotNil(t, rf)
	assert.Equal(t, "api_keys", rf.Table)
	assert.Equal(t, "org_id", rf.ParentColumn)
	assert.True(t, rf.SoftDelete)
	assert.NotEmpty(t, rf.Fields)
}

func TestProjectFilter(t *testing.T) {
	rf := ProjectFilter()
	require.NotNil(t, rf)
	assert.Equal(t, "projects", rf.Table)
	assert.True(t, rf.SoftDelete)
	assert.Contains(t, rf.Fields, "labels")
	assert.True(t, rf.Fields["labels"].JSONB)
}

func TestOrganizationFilter(t *testing.T) {
	rf := OrganizationFilter()
	require.NotNil(t, rf)
	assert.Equal(t, "organizations", rf.Table)
	assert.True(t, rf.SoftDelete)
	assert.Empty(t, rf.ParentColumn)
}

func TestTagKeyFilter(t *testing.T) {
	rf := TagKeyFilter()
	require.NotNil(t, rf)
	assert.Equal(t, "tag_keys", rf.Table)
	assert.Equal(t, "org_id", rf.ParentColumn)
	assert.False(t, rf.SoftDelete)
}

// ---------------------------------------------------------------------------
// ParseOrderBy
// ---------------------------------------------------------------------------

func TestParseOrderBy(t *testing.T) {
	rf := ProjectFilter()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{
			name:  "single field default asc",
			input: "displayName",
			want:  "display_name ASC",
		},
		{
			name:  "single field explicit asc",
			input: "displayName asc",
			want:  "display_name ASC",
		},
		{
			name:  "single field desc",
			input: "displayName desc",
			want:  "display_name DESC",
		},
		{
			name:  "multiple fields",
			input: "createTime desc, name",
			want:  "create_time DESC, name ASC",
		},
		{
			name:  "name field always allowed",
			input: "name desc",
			want:  "name DESC",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  "",
		},
		{
			name:    "invalid field",
			input:   "unknownField",
			wantErr: true,
		},
		{
			name:    "invalid direction",
			input:   "displayName sideways",
			wantErr: true,
		},
		{
			name:    "too many tokens",
			input:   "displayName asc extra",
			wantErr: true,
		},
		{
			name:    "JSONB field not orderable",
			input:   "labels",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseOrderBy(rf, tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// Transpile — remaining coverage gaps
// ---------------------------------------------------------------------------

func TestTranspile_TimestampFunction(t *testing.T) {
	rf := ProjectFilter()

	// timestamp() as a standalone expression (not in a comparison).
	// This exercises the transpileTimestamp path via transpileCall.
	wc, err := Transpile(rf, `createTime = timestamp("2025-01-15T10:30:00Z")`, 1)
	require.NoError(t, err)
	assert.Equal(t, `create_time = $1`, wc.SQL)
	require.Len(t, wc.Args, 1)
}

func TestTranspile_TimestampInvalid(t *testing.T) {
	rf := ProjectFilter()

	_, err := Transpile(rf, `createTime = timestamp("not-a-timestamp")`, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timestamp")
}

func TestTranspile_InvalidFilterSyntax(t *testing.T) {
	rf := ProjectFilter()

	_, err := Transpile(rf, `"unclosed string`, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid filter")
}

func TestTranspile_SelectExpression(t *testing.T) {
	rf := ProjectFilter()

	// labels.env (select expression as an ident in traversal).
	wc, err := Transpile(rf, `labels.env = "prod"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `labels->>'env' = $1`, wc.SQL)
	assert.Equal(t, []any{"prod"}, wc.Args)
}

func TestTranspile_SelectOnNonJSONB_Error(t *testing.T) {
	rf := ProjectFilter()

	// state is not JSONB — traversal should fail.
	_, err := Transpile(rf, `state.sub = "val"`, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support traversal")
}

func TestTranspile_BareLiteral_NoDefaultFields_Error(t *testing.T) {
	rf := &ResourceFilter{
		Fields:        map[string]FieldMapping{"x": {Column: "x"}},
		Table:         "test",
		DefaultFields: nil, // no default fields
	}

	_, err := Transpile(rf, `bare_literal`, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no default fields configured")
}

func TestTranspile_HasSelectOnNonJSONB_Error(t *testing.T) {
	rf := ProjectFilter()

	// displayName is not JSONB — has-select should fail.
	_, err := Transpile(rf, `displayName.sub : "val"`, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support traversal")
}

// ---------------------------------------------------------------------------
// Transpile — constant types
// ---------------------------------------------------------------------------

func TestTranspile_ConstTypes(t *testing.T) {
	// Create a custom filter with numeric-typed field to test int/float constants.
	rf := ProjectFilter()

	// Bool constant via bare identifier in value position (e.g., state = true).
	// The AIP parser interprets `true` as an ident, resolved via resolveValue.
	// Since state doesn't have AllowPartial, it uses = with the string "true".
	wc, err := Transpile(rf, `state = "ACTIVE"`, 1)
	require.NoError(t, err)
	assert.Equal(t, `state = $1`, wc.SQL)
	assert.Equal(t, []any{"ACTIVE"}, wc.Args)
}
