package filter

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// basicRF is a minimal ResourceFilter for BuildListQuery assembly tests: no
// filterable/sortable surface, just a table name. Tests set SoftDelete or add
// scope predicates as needed.
func basicRF() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{},
		Sortable:   map[string]SortableField{},
		Table:      "t",
	}
}

func TestPlanOrderBy_DefaultIsIDOnly(t *testing.T) {
	plan, err := PlanOrderBy(ConnectorFilter(), "")
	require.NoError(t, err)
	assert.Equal(t, "", plan.Field, "empty order_by orders by id only")
	assert.False(t, plan.Descending)
}

func TestPlanOrderBy_SingleFieldAscDesc(t *testing.T) {
	rf := ConnectorFilter()

	plan, err := PlanOrderBy(rf, "displayName")
	require.NoError(t, err)
	assert.Equal(t, "displayName", plan.Field)
	assert.Equal(t, "display_name", plan.Column)
	assert.False(t, plan.Descending)

	plan, err = PlanOrderBy(rf, "createTime desc")
	require.NoError(t, err)
	assert.Equal(t, "create_time", plan.Column)
	assert.True(t, plan.Descending)
}

func TestPlanOrderBy_RejectsUnknownField(t *testing.T) {
	// slug is not sortable; agent is filterable-only, not sortable.
	for _, f := range []string{"slug", "agent", "id", "bogus"} {
		_, err := PlanOrderBy(ConnectorFilter(), f)
		require.Error(t, err, "order_by %q must be rejected", f)
	}
}

func TestPlanOrderBy_RejectsMultipleFields(t *testing.T) {
	// The compound cursor encodes exactly one (sortValue, id) pair, so a
	// multi-field order_by is refused rather than silently truncated.
	_, err := PlanOrderBy(ConnectorFilter(), "displayName, createTime")
	require.Error(t, err)
}

func TestPlanOrderBy_RejectsBadDirection(t *testing.T) {
	_, err := PlanOrderBy(ConnectorFilter(), "displayName sideways")
	require.Error(t, err)
}

func baseScope(t *testing.T) (uuid.UUID, pgtype.UUID, []Predicate) {
	t.Helper()
	orgID := uuid.New()
	spaceID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	return orgID, spaceID, []Predicate{
		{SQL: "org_id = %s", Arg: orgID},
		{SQL: "space_id IS NOT DISTINCT FROM %s", Arg: spaceID},
	}
}

func TestBuildListQuery_BaseScopeOnly(t *testing.T) {
	orgID, spaceID, base := baseScope(t)
	sql, args, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		PageSize: 25,
	})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT * FROM connectors WHERE org_id = $1 AND space_id IS NOT DISTINCT FROM $2 ORDER BY id LIMIT $3",
		sql)
	assert.Equal(t, []any{orgID, spaceID, int32(26)}, args, "page size over-fetches by one")
}

func TestBuildListQuery_WithFilter_BindsOperand(t *testing.T) {
	_, _, base := baseScope(t)
	sql, args, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		Filter:   `displayName = "acme"`,
		PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT * FROM connectors WHERE org_id = $1 AND space_id IS NOT DISTINCT FROM $2 AND display_name = $3 ORDER BY id LIMIT $4",
		sql)
	assert.Equal(t, "acme", args[2], "filter operand is bound, not interpolated")
}

func TestBuildListQuery_SubstringFilter(t *testing.T) {
	_, _, base := baseScope(t)
	sql, args, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		Filter:   `displayName : "hub"`,
		PageSize: 10,
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "display_name ILIKE $3")
	assert.Equal(t, "%hub%", args[2])
}

// TestBuildListQuery_InjectionOperandIsInert is the security pin the coordinator
// asked for: a SQL-injection payload in a filter VALUE must be treated as a
// literal string operand bound to $N, never spliced into the query text.
func TestBuildListQuery_InjectionOperandIsInert(t *testing.T) {
	_, _, base := baseScope(t)
	const payload = `x' OR '1'='1`
	sql, args, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		Filter:   `displayName = "` + payload + `"`,
		PageSize: 10,
	})
	require.NoError(t, err)
	// The dangerous text appears ONLY in the args, never in the SQL string.
	assert.Equal(t,
		"SELECT * FROM connectors WHERE org_id = $1 AND space_id IS NOT DISTINCT FROM $2 AND display_name = $3 ORDER BY id LIMIT $4",
		sql)
	assert.NotContains(t, sql, "OR '1'='1")
	assert.Equal(t, payload, args[2], "payload is a bound literal operand")
}

func TestBuildListQuery_RejectsUnknownFilterField(t *testing.T) {
	_, _, base := baseScope(t)
	_, _, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		Filter:   `secretColumn = "x"`,
		PageSize: 10,
	})
	require.Error(t, err, "unknown filter field must error")
}

func TestBuildListQuery_IDOnlyCursor(t *testing.T) {
	_, _, base := baseScope(t)
	cid := uuid.New()
	sql, args, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		PageSize: 10,
		Cursor:   &KeysetCursor{ID: cid},
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "AND id > $3 ORDER BY id LIMIT $4")
	assert.Equal(t, cid, args[2])
}

func TestBuildListQuery_CompoundCursorAsc(t *testing.T) {
	_, _, base := baseScope(t)
	cid := uuid.New()
	plan, err := PlanOrderBy(ConnectorFilter(), "displayName")
	require.NoError(t, err)
	sql, args, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		Order:    plan,
		PageSize: 10,
		Cursor:   &KeysetCursor{SortValue: "foo", ID: cid},
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "AND (display_name, id) > ($3, $4)")
	assert.Contains(t, sql, "ORDER BY display_name ASC, id ASC")
	assert.Equal(t, "foo", args[2])
	assert.Equal(t, cid, args[3])
}

func TestBuildListQuery_CompoundCursorDescFlipsOperator(t *testing.T) {
	_, _, base := baseScope(t)
	plan, err := PlanOrderBy(ConnectorFilter(), "createTime desc")
	require.NoError(t, err)
	sql, _, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     base,
		Order:    plan,
		PageSize: 10,
		Cursor:   &KeysetCursor{SortValue: "2026-01-01T00:00:00Z", ID: uuid.New()},
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "AND (create_time, id) < (")
	assert.Contains(t, sql, "ORDER BY create_time DESC, id DESC")
}

// TestBuildListQuery_SoftDeleteExcludesDeletedByDefault pins that a
// SoftDelete resource excludes soft-deleted rows by default, mirroring the
// legacy filter.Query path. Spaces is the first SoftDelete resource to move
// onto the compound-cursor engine, so BuildListQuery must honor rf.SoftDelete.
func TestBuildListQuery_SoftDeleteExcludesDeletedByDefault(t *testing.T) {
	rf := basicRF()
	rf.SoftDelete = true
	sql, _, err := BuildListQuery(ListQuery{Resource: rf, PageSize: 10})
	require.NoError(t, err)
	assert.Contains(t, sql, "delete_time IS NULL")
}

// TestBuildListQuery_ShowDeletedIncludesSoftDeleted pins the show_deleted
// escape hatch: with ShowDeleted set, the soft-delete predicate is dropped.
func TestBuildListQuery_ShowDeletedIncludesSoftDeleted(t *testing.T) {
	rf := basicRF()
	rf.SoftDelete = true
	sql, _, err := BuildListQuery(ListQuery{Resource: rf, PageSize: 10, ShowDeleted: true})
	require.NoError(t, err)
	assert.NotContains(t, sql, "delete_time IS NULL")
}

// TestBuildListQuery_HardDeleteHasNoSoftDeletePredicate pins that a hard-delete
// resource (ConnectorFilter, SoftDelete:false — no delete_time column) never
// emits the predicate, so the existing connectors SQL is unaffected.
func TestBuildListQuery_HardDeleteHasNoSoftDeletePredicate(t *testing.T) {
	_, _, base := baseScope(t)
	sql, _, err := BuildListQuery(ListQuery{Resource: ConnectorFilter(), Base: base, PageSize: 10})
	require.NoError(t, err)
	assert.NotContains(t, sql, "delete_time")
}

// TestBuildListQuery_SoftDeleteWithBaseScope_NumbersArgsCorrectly pins the
// security-critical numbering: the soft-delete predicate is a no-arg literal,
// so the base scope still binds at $1 and the limit at $2 — the placeholder
// stream stays aligned with the args slice.
func TestBuildListQuery_SoftDeleteWithBaseScope_NumbersArgsCorrectly(t *testing.T) {
	rf := basicRF()
	rf.SoftDelete = true
	orgID := uuid.New()
	sql, args, err := BuildListQuery(ListQuery{
		Resource: rf,
		Base:     []Predicate{{SQL: "org_id = %s", Arg: orgID}},
		PageSize: 5,
	})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT * FROM t WHERE delete_time IS NULL AND org_id = $1 ORDER BY id LIMIT $2",
		sql)
	assert.Equal(t, []any{orgID, int32(6)}, args)
}

func TestBuildListQuery_PageSizeClamped(t *testing.T) {
	_, _, base := baseScope(t)
	_, args, err := BuildListQuery(ListQuery{Resource: ConnectorFilter(), Base: base, PageSize: 100000})
	require.NoError(t, err)
	assert.Equal(t, int32(1001), args[len(args)-1], "page size caps at 1000, over-fetch +1")

	_, args, err = BuildListQuery(ListQuery{Resource: ConnectorFilter(), Base: base, PageSize: 0})
	require.NoError(t, err)
	assert.Equal(t, int32(101), args[len(args)-1], "unset page size defaults to 100")
}
