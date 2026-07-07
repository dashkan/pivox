package mcp

import (
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActiveOrganizationAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		info    *auth.TokenInfo
		want    string
		wantErr bool
	}{
		{
			name: "single bound org",
			info: &auth.TokenInfo{Extra: map[string]any{ExtraOrganization: []string{"pacific-coast-net"}}},
			want: "pacific-coast-net",
		},
		{
			name: "first of multiple",
			info: &auth.TokenInfo{Extra: map[string]any{ExtraOrganization: []string{"acme", "other"}}},
			want: "acme",
		},
		{name: "nil token info", info: nil, wantErr: true},
		{name: "no organization key", info: &auth.TokenInfo{Extra: map[string]any{}}, wantErr: true},
		{name: "empty organization slice", info: &auth.TokenInfo{Extra: map[string]any{ExtraOrganization: []string{}}}, wantErr: true},
		{name: "wrong type", info: &auth.TokenInfo{Extra: map[string]any{ExtraOrganization: "acme"}}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := activeOrganizationAlias(tt.info)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
