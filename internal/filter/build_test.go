package filter

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/appkey"
)

// ---------------------------------------------------------------------------
// buildQuery — pure SQL assembly, unit-testable without a real DB.
// ---------------------------------------------------------------------------

func basicRF() *ResourceFilter {
	return &ResourceFilter{
		Filterable:   map[string]FilterableField{},
		Sortable:     map[string]SortableField{},
		Table:        "t",
		CursorColumn: "id",
	}
}

func testCodec(t *testing.T) *appkey.Codec {
	t.Helper()
	c, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	require.NoError(t, err)
	return c
}

func TestBuildQuery_Minimal(t *testing.T) {
	sql, args, err := buildQuery(basicRF(), QueryParams{})
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM t LIMIT $1", sql)
	assert.Equal(t, []any{int32(101)}, args) // default page size 100 + 1
}

func TestBuildQuery_SoftDelete(t *testing.T) {
	rf := basicRF()
	rf.SoftDelete = true
	sql, _, err := buildQuery(rf, QueryParams{})
	require.NoError(t, err)
	assert.Contains(t, sql, "delete_time IS NULL")
}

func TestBuildQuery_ShowDeletedBypassesSoftDelete(t *testing.T) {
	rf := basicRF()
	rf.SoftDelete = true
	sql, _, err := buildQuery(rf, QueryParams{ShowDeleted: true})
	require.NoError(t, err)
	assert.NotContains(t, sql, "delete_time IS NULL")
}

func TestBuildQuery_ParentFilter(t *testing.T) {
	rf := basicRF()
	rf.ParentColumn = "org_id"
	sql, args, err := buildQuery(rf, QueryParams{ParentID: "parent-uuid"})
	require.NoError(t, err)
	assert.Contains(t, sql, "org_id = $1")
	assert.Equal(t, "parent-uuid", args[0])
}

// ---------------------------------------------------------------------------
// User-scoped predicate (always-applied, for access control)
// ---------------------------------------------------------------------------

func TestBuildQuery_UserColumnAppliedAlwaysWhenUserIDSet(t *testing.T) {
	rf := basicRF()
	rf.UserColumn = "created_by"
	sql, args, err := buildQuery(rf, QueryParams{UserID: "uid-123"})
	require.NoError(t, err)
	assert.Contains(t, sql, "created_by = $1")
	assert.Equal(t, "uid-123", args[0])
}

func TestBuildQuery_UserColumnRequiresUserID(t *testing.T) {
	// If UserColumn is configured but no UserID is passed, that's a server
	// misconfiguration — the handler forgot to wire auth context. Fail
	// loudly rather than return unscoped rows.
	rf := basicRF()
	rf.UserColumn = "created_by"
	_, _, err := buildQuery(rf, QueryParams{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UserID required")
}

func TestBuildQuery_UserColumnWithParent_BothApplied(t *testing.T) {
	rf := basicRF()
	rf.ParentColumn = "org_id"
	rf.UserColumn = "created_by"
	sql, args, err := buildQuery(rf, QueryParams{
		ParentID: "org-uuid",
		UserID:   "uid-abc",
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "org_id = $1")
	assert.Contains(t, sql, "created_by = $2")
	assert.Equal(t, []any{"org-uuid", "uid-abc", int32(101)}, args)
}

// ---------------------------------------------------------------------------
// Cursor direction (DESC vs ASC)
// ---------------------------------------------------------------------------

func TestBuildQuery_CursorASC_UsesGreaterThan(t *testing.T) {
	rf := basicRF()
	// Default CursorDirection (empty → ASC).
	id := uuid.New()
	tok, err := EncodeNextPageToken(testCodec(t), id)
	require.NoError(t, err)

	sql, args, err := buildQuery(rf, QueryParams{Cursor: tok, Codec: testCodec(t)})
	require.NoError(t, err)
	assert.Contains(t, sql, "id > $1")
	assert.Equal(t, id.String(), args[0])
}

func TestBuildQuery_CursorDESC_UsesLessThan(t *testing.T) {
	rf := basicRF()
	rf.CursorDirection = "DESC"
	id := uuid.New()
	tok, err := EncodeNextPageToken(testCodec(t), id)
	require.NoError(t, err)

	sql, args, err := buildQuery(rf, QueryParams{Cursor: tok, Codec: testCodec(t)})
	require.NoError(t, err)
	assert.Contains(t, sql, "id < $1")
	assert.Equal(t, id.String(), args[0])
}

func TestBuildQuery_NoCursor_NoCursorPredicate(t *testing.T) {
	rf := basicRF()
	rf.CursorDirection = "DESC"
	sql, _, err := buildQuery(rf, QueryParams{})
	require.NoError(t, err)
	assert.NotContains(t, sql, "id < ")
	assert.NotContains(t, sql, "id > ")
}
