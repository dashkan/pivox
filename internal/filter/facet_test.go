package filter

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// facetTestResource is a small ResourceFilter used by the pure-function facet
// tests: an `agent` + `state` filterable/facetable surface over a fixed table.
func facetTestResource() *ResourceFilter {
	return &ResourceFilter{
		Filterable: map[string]FilterableField{
			"agent":       {Column: "agent"},
			"state":       {Column: "state"},
			"displayName": {Column: "display_name", AllowPartial: true},
		},
		Facetable: map[string]FacetableField{
			"agent": {Column: "agent"},
			"state": {Column: "state"},
		},
		Table:         "connectors",
		DefaultFields: []string{"displayName"},
	}
}

func TestActiveFilterFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		filter string
		want   []string
	}{
		{"empty", "", nil},
		{"single", `agent = "A"`, []string{"agent"}},
		{"conjunction", `agent = "A" AND state = "ACTIVE"`, []string{"agent", "state"}},
		{"disjunction", `agent = "A" OR agent = "B"`, []string{"agent"}},
		{"negation", `NOT (agent = "A")`, []string{"agent"}},
		// A bare literal expands over DefaultFields; it is not a field reference.
		{"bare literal", `webhook`, nil},
		{"mixed with partial", `displayName = "x*" AND agent = "A"`, []string{"agent", "displayName"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := activeFilterFields(tt.filter)
			require.NoError(t, err)
			keys := make([]string, 0, len(got))
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			assert.Equal(t, tt.want, nilIfEmpty(keys))
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func TestTranspileExcluding_DropsOwnField(t *testing.T) {
	t.Parallel()
	rf := facetTestResource()

	// Excluding `agent` neutralizes every agent comparison to TRUE while keeping
	// the state predicate and its bound value.
	wc, err := TranspileExcluding(rf, `agent = "A" AND state = "ACTIVE"`, 1, "agent")
	require.NoError(t, err)
	assert.Contains(t, wc.SQL, "TRUE")
	assert.Contains(t, wc.SQL, "state = $1")
	// Only the state operand is bound; the excluded agent operand is not.
	assert.Equal(t, []any{"ACTIVE"}, wc.Args)

	// Excluding a field not present in the filter leaves it fully intact.
	wc2, err := TranspileExcluding(rf, `agent = "A"`, 1, "state")
	require.NoError(t, err)
	assert.Equal(t, "agent = $1", wc2.SQL)
	assert.Equal(t, []any{"A"}, wc2.Args)
}

func TestValidateFacetSpecs_RejectsUnknownField(t *testing.T) {
	t.Parallel()
	rf := facetTestResource()

	_, _, err := ComputeFacets(context.Background(), nil, ListQuery{Resource: rf}, []FacetSpec{
		{Field: "agent"},
		{Field: "not_facetable"},
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownFacetField), "unknown facet field must surface ErrUnknownFacetField")
	assert.Contains(t, err.Error(), "not_facetable")
}

func TestComputeFacets_EmptySpecs_NoOp(t *testing.T) {
	t.Parallel()
	// nil querier proves no query is issued when there are no specs.
	total, results, err := ComputeFacets(context.Background(), nil, ListQuery{Resource: facetTestResource()}, nil)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Empty(t, results)
}

// TestBuildSharedFacetQuery pins the GROUPING SETS SQL shape: whitelisted
// columns emitted verbatim, one grouping set per shared facet plus the empty
// set () for the grand total, and only the WHERE operands bound as $N.
func TestBuildSharedFacetQuery(t *testing.T) {
	t.Parallel()
	rf := facetTestResource()
	conds := []string{"org_id = $1"}

	sql := buildSharedFacetQuery(rf, conds, []FacetSpec{
		{Field: "agent", Column: "agent"},
		{Field: "state", Column: "state"},
	})
	assert.Contains(t, sql, "count(*)")
	assert.Contains(t, sql, "grouping(agent)")
	assert.Contains(t, sql, "grouping(state)")
	assert.Contains(t, sql, "GROUP BY GROUPING SETS ((agent), (state), ())")
	assert.Contains(t, sql, "WHERE org_id = $1")
	// No value is ever formatted into the text.
	assert.NotContains(t, sql, "'")

	// Zero shared facets → a bare COUNT(*) for the total only.
	only := buildSharedFacetQuery(rf, conds, nil)
	assert.Contains(t, only, "count(*)")
	assert.NotContains(t, only, "GROUPING SETS")
}

func TestBuildTermsFacetQuery(t *testing.T) {
	t.Parallel()
	rf := facetTestResource()
	sql := buildTermsFacetQuery(rf, []string{"org_id = $1", "TRUE"}, FacetSpec{Field: "agent", Column: "agent"})
	assert.Contains(t, sql, "GROUP BY agent")
	assert.Contains(t, sql, "count(*)")
	assert.Contains(t, sql, "WHERE org_id = $1 AND TRUE")
	assert.True(t, strings.HasPrefix(sql, "SELECT"))
}
