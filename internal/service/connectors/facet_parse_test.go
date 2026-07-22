package connectors

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/filter"
)

// TestParseFacetAggs pins the List-tier `aggs` string grammar: each element is
// `field` or `field:size`, every parsed facet is self-excluding (List terms are
// multi-select), and a malformed element is InvalidArgument on `aggs`. Field
// membership in the Facetable allowlist is NOT checked here — that stays in
// filter.ComputeFacets (single allowlist source), covered by the e2e.
func TestParseFacetAggs(t *testing.T) {
	t.Parallel()

	// One past the cap — distinct fields so it's the count check, not dedup,
	// that rejects. Checked before any allocation/field validation.
	tooManyAggs := make([]string, maxFacetAggs+1)
	for i := range tooManyAggs {
		tooManyAggs[i] = fmt.Sprintf("f%d", i)
	}

	tests := []struct {
		name    string
		aggs    []string
		want    []filter.FacetSpec
		wantErr bool
	}{
		{name: "nil slice is a no-op", aggs: nil, want: nil},
		{name: "empty slice is a no-op", aggs: []string{}, want: nil},
		{
			name: "field only leaves size unset and self-excludes",
			aggs: []string{"agent"},
			want: []filter.FacetSpec{{Field: "agent", SelfExcluding: true}},
		},
		{
			name: "field with explicit size",
			aggs: []string{"agent:5"},
			want: []filter.FacetSpec{{Field: "agent", SelfExcluding: true, Size: 5}},
		},
		{
			name: "multiple aggs preserved in order",
			aggs: []string{"agent", "space:3"},
			want: []filter.FacetSpec{
				{Field: "agent", SelfExcluding: true},
				{Field: "space", SelfExcluding: true, Size: 3},
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			aggs: []string{"  agent : 4 "},
			want: []filter.FacetSpec{{Field: "agent", SelfExcluding: true, Size: 4}},
		},
		{name: "non-integer size rejected", aggs: []string{"agent:abc"}, wantErr: true},
		{name: "empty size after colon rejected", aggs: []string{"agent:"}, wantErr: true},
		{name: "zero size rejected", aggs: []string{"agent:0"}, wantErr: true},
		{name: "negative size rejected", aggs: []string{"agent:-1"}, wantErr: true},
		{name: "int32-overflow size rejected", aggs: []string{"agent:99999999999"}, wantErr: true},
		{name: "empty element rejected", aggs: []string{""}, wantErr: true},
		{name: "missing field before colon rejected", aggs: []string{":5"}, wantErr: true},
		{
			// Same field twice is ambiguous: the response facets map is
			// field-keyed, so a second spec would collide on / clobber the
			// first's entry. Reject rather than silently drop one.
			name:    "duplicate field rejected",
			aggs:    []string{"agent", "agent"},
			wantErr: true,
		},
		{
			name:    "duplicate field with differing sizes rejected",
			aggs:    []string{"agent:5", "agent:10"},
			wantErr: true,
		},
		{name: "more than maxFacetAggs rejected", aggs: tooManyAggs, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := parseFacetAggs(tt.aggs)
			if tt.wantErr {
				require.Error(t, err)
				assert.Equal(t, codes.InvalidArgument, status.Code(err),
					"a malformed agg is InvalidArgument")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
