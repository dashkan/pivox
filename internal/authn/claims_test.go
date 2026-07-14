package authn_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/dashkan/pivox/internal/authn"
)

func TestOrganizationsFromClaims(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		claims map[string]any
		want   []string
	}{
		{
			name:   "absent claim",
			claims: map[string]any{"sub": "x"},
			want:   nil,
		},
		{
			name:   "nil claims",
			claims: nil,
			want:   nil,
		},
		{
			name:   "empty string",
			claims: map[string]any{authn.ClaimOrganization: ""},
			want:   nil,
		},
		{
			name:   "single string",
			claims: map[string]any{authn.ClaimOrganization: "acme"},
			want:   []string{"acme"},
		},
		{
			name:   "json array (decoded as []any)",
			claims: map[string]any{authn.ClaimOrganization: []any{"acme", "beta"}},
			want:   []string{"acme", "beta"},
		},
		{
			name:   "json array skips non-strings and empties",
			claims: map[string]any{authn.ClaimOrganization: []any{"acme", "", 42, "beta"}},
			want:   []string{"acme", "beta"},
		},
		{
			name:   "json array all invalid",
			claims: map[string]any{authn.ClaimOrganization: []any{"", 42}},
			want:   nil,
		},
		{
			name:   "already []string",
			claims: map[string]any{authn.ClaimOrganization: []string{"acme"}},
			want:   []string{"acme"},
		},
		{
			name:   "[]string skips empties",
			claims: map[string]any{authn.ClaimOrganization: []string{"", "acme", ""}},
			want:   []string{"acme"},
		},
		{
			name:   "[]string all empty",
			claims: map[string]any{authn.ClaimOrganization: []string{"", ""}},
			want:   nil,
		},
		{
			name:   "unexpected type",
			claims: map[string]any{authn.ClaimOrganization: 42},
			want:   nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, authn.OrganizationsFromClaims(tc.claims))
		})
	}
}
