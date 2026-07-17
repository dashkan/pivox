package filter

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.einride.tech/aip/filtering"
)

// The three per-resource "defaults" knobs on ResourceFilter — DefaultOrder,
// DefaultPageSize/MaxPageSize, and DefaultConditions — are exercised here. Each
// must be inert when unset (existing resources unchanged) and honored when set.

// ---------------------------------------------------------------------------
// DefaultOrder — the default sort order (incl. direction) knob.
// ---------------------------------------------------------------------------

// TestPlanOrderBy_DefaultOrderUnset_IsIDASC pins that a resource that declares
// no DefaultOrder keeps the historical id-ASC default (the zero plan). This is
// the guard that every already-migrated resource (connectors/spaces/apikeys/
// tags/requests/assets — none of which set DefaultOrder) is unaffected.
func TestPlanOrderBy_DefaultOrderUnset_IsIDASC(t *testing.T) {
	rf := ConnectorFilter() // DefaultOrder unset
	plan, err := PlanOrderBy(rf, "")
	require.NoError(t, err)
	assert.Equal(t, "", plan.Field, "unset DefaultOrder → id-only")
	assert.False(t, plan.Descending, "unset DefaultOrder → ASC")
}

// TestPlanOrderBy_DefaultOrderIDDesc pins the id-only DESC default (the aichat
// case): "id desc" yields Field=="" (compact id-only cursor) but Descending.
func TestPlanOrderBy_DefaultOrderIDDesc(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultOrder = "id desc"
	plan, err := PlanOrderBy(rf, "")
	require.NoError(t, err)
	assert.Equal(t, "", plan.Field, "id default stays the id-only keyset")
	assert.True(t, plan.Descending, "declared DESC direction flows through")
}

// TestPlanOrderBy_DefaultOrderCompoundDesc pins a non-id DESC default: the plan
// resolves the Sortable column and carries its type, exactly like a
// client-supplied order_by would.
func TestPlanOrderBy_DefaultOrderCompoundDesc(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultOrder = "createTime desc"
	plan, err := PlanOrderBy(rf, "")
	require.NoError(t, err)
	assert.Equal(t, "createTime", plan.Field)
	assert.Equal(t, "create_time", plan.Column)
	assert.Equal(t, filtering.TypeTimestamp, plan.Type)
	assert.True(t, plan.Descending)
}

// TestPlanOrderBy_ClientOrderOverridesDefault pins that an explicit client
// order_by wins over the resource default.
func TestPlanOrderBy_ClientOrderOverridesDefault(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultOrder = "id desc"
	plan, err := PlanOrderBy(rf, "displayName")
	require.NoError(t, err)
	assert.Equal(t, "displayName", plan.Field)
	assert.False(t, plan.Descending, "client asc overrides the desc default")
}

// TestPlanOrderBy_DefaultOrderBadField pins that a misconfigured DefaultOrder
// (a field not in Sortable) surfaces as an error rather than silently ignored —
// it is a server-side declaration, so this is a startup-time programmer error.
func TestPlanOrderBy_DefaultOrderBadField(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultOrder = "bogus desc"
	_, err := PlanOrderBy(rf, "")
	require.Error(t, err)
}

// TestBuildListQuery_DefaultOrderIDDesc_Boundary is the id-only DESC default
// end-to-end at the SQL-assembly level: first page orders by id DESC, and the
// next page (with a cursor) resumes on the STRICT `id < $cursor` predicate.
// Operator (`<`) and ORDER BY direction (DESC) agree, which is the structural
// guarantee that no row drops or duplicates across the page boundary. (The
// empirical drain-all-pages proof against a real DB lives in the aichat
// default-order e2e test.)
func TestBuildListQuery_DefaultOrderIDDesc_Boundary(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultOrder = "id desc"
	plan, err := PlanOrderBy(rf, "")
	require.NoError(t, err)

	// First page.
	sql, _, err := BuildListQuery(ListQuery{Resource: rf, Order: plan, PageSize: 3})
	require.NoError(t, err)
	assert.Contains(t, sql, "ORDER BY id DESC")
	assert.NotContains(t, sql, "ORDER BY id LIMIT", "DESC default must not fall back to ASC id order")

	// Next page.
	cid := uuid.New()
	sql, args, err := BuildListQuery(ListQuery{Resource: rf, Order: plan, PageSize: 3, Cursor: &KeysetCursor{ID: cid}})
	require.NoError(t, err)
	assert.Contains(t, sql, "id < $1", "DESC id keyset resumes on strict <")
	assert.Contains(t, sql, "ORDER BY id DESC")
	assert.Equal(t, cid, args[0])
}

// TestBuildListQuery_DefaultOrderCompoundDesc_Boundary is the non-id DESC
// default: ORDER BY <col> DESC, id DESC with the row-value comparison resuming
// on strict `<`.
func TestBuildListQuery_DefaultOrderCompoundDesc_Boundary(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultOrder = "createTime desc"
	plan, err := PlanOrderBy(rf, "")
	require.NoError(t, err)

	cid := uuid.New()
	sql, _, err := BuildListQuery(ListQuery{
		Resource: rf,
		Order:    plan,
		PageSize: 3,
		Cursor:   &KeysetCursor{SortValue: "2026-01-01T00:00:00Z", ID: cid},
	})
	require.NoError(t, err)
	assert.Contains(t, sql, "(create_time, id) < ($1, $2)")
	assert.Contains(t, sql, "ORDER BY create_time DESC, id DESC")
}

// ---------------------------------------------------------------------------
// DefaultPageSize / MaxPageSize — via the shared ClampPageSize helper.
// ---------------------------------------------------------------------------

func TestClampPageSize(t *testing.T) {
	tests := []struct {
		name        string
		defaultSize int32
		maxSize     int32
		in          int32
		want        int32
	}{
		{name: "unset default on empty → 100", in: 0, want: 100},
		{name: "unset cap on huge → 1000", in: 100000, want: 1000},
		{name: "in-range passes through", in: 42, want: 42},
		{name: "declared DefaultPageSize on empty", defaultSize: 50, in: 0, want: 50},
		{name: "declared DefaultPageSize ignored when client supplies", defaultSize: 50, in: 25, want: 25},
		{name: "declared MaxPageSize caps", maxSize: 100, in: 5000, want: 100},
		{name: "declared MaxPageSize passes in-range", maxSize: 100, in: 80, want: 80},
		{name: "both declared", defaultSize: 50, maxSize: 100, in: 0, want: 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := &ResourceFilter{DefaultPageSize: tt.defaultSize, MaxPageSize: tt.maxSize}
			assert.Equal(t, tt.want, ClampPageSize(rf, tt.in))
		})
	}
}

// TestClampPageSize_NilResource pins the defensive nil path (falls back to the
// universal 100/1000 policy).
func TestClampPageSize_NilResource(t *testing.T) {
	assert.Equal(t, int32(100), ClampPageSize(nil, 0))
	assert.Equal(t, int32(1000), ClampPageSize(nil, 5000))
	assert.Equal(t, int32(7), ClampPageSize(nil, 7))
}

// ---------------------------------------------------------------------------
// DefaultConditions — server-declared predicates always ANDed in.
// ---------------------------------------------------------------------------

// TestBuildListQuery_DefaultConditions_AppliedWithNoClientFilter pins that a
// default predicate is applied even when the client sends no filter.
func TestBuildListQuery_DefaultConditions_AppliedWithNoClientFilter(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultConditions = []Predicate{{SQL: "agent = %s", Arg: "claude"}}
	orgID := uuid.New()
	sql, args, err := BuildListQuery(ListQuery{
		Resource: rf,
		Base:     []Predicate{{SQL: "org_id = %s", Arg: orgID}},
		PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT * FROM connectors WHERE org_id = $1 AND agent = $2 ORDER BY id LIMIT $3",
		sql)
	assert.Equal(t, []any{orgID, "claude", int32(11)}, args)
}

// TestBuildListQuery_DefaultConditions_NumbersArgsWithBaseFilterCursor is the
// security-critical numbering pin: default conditions carry a bound Arg, so
// with base + client filter + cursor all present the $N placeholders must stay
// aligned with the args slice — base $1, default $2, filter $3, cursor $4,
// limit $5.
func TestBuildListQuery_DefaultConditions_NumbersArgsWithBaseFilterCursor(t *testing.T) {
	rf := ConnectorFilter()
	rf.DefaultConditions = []Predicate{{SQL: "agent = %s", Arg: "claude"}}
	orgID := uuid.New()
	cid := uuid.New()
	sql, args, err := BuildListQuery(ListQuery{
		Resource: rf,
		Base:     []Predicate{{SQL: "org_id = %s", Arg: orgID}},
		Filter:   `displayName = "acme"`,
		PageSize: 10,
		Cursor:   &KeysetCursor{ID: cid},
	})
	require.NoError(t, err)
	assert.Equal(t,
		"SELECT * FROM connectors WHERE org_id = $1 AND agent = $2 AND display_name = $3 AND id > $4 ORDER BY id LIMIT $5",
		sql)
	assert.Equal(t, []any{orgID, "claude", "acme", cid, int32(11)}, args)
}

// TestBuildListQuery_DefaultConditions_Unset_NoChange pins the inert case: a
// resource with no DefaultConditions produces byte-identical SQL to before the
// knob existed.
func TestBuildListQuery_DefaultConditions_Unset_NoChange(t *testing.T) {
	orgID := uuid.New()
	sql, _, err := BuildListQuery(ListQuery{
		Resource: ConnectorFilter(),
		Base:     []Predicate{{SQL: "org_id = %s", Arg: orgID}},
		PageSize: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, "SELECT * FROM connectors WHERE org_id = $1 ORDER BY id LIMIT $2", sql)
}
