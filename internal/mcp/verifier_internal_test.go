package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestExpirationFromClaims(t *testing.T) {
	t.Parallel()

	const exp = int64(1893456000) // 2030-01-01T00:00:00Z

	tests := []struct {
		name   string
		claims map[string]any
		want   time.Time
	}{
		{name: "float64 (the golang-jwt path)", claims: map[string]any{"exp": float64(exp)}, want: time.Unix(exp, 0)},
		{name: "int64", claims: map[string]any{"exp": exp}, want: time.Unix(exp, 0)},
		{name: "json.Number", claims: map[string]any{"exp": json.Number("1893456000")}, want: time.Unix(exp, 0)},
		{name: "absent", claims: map[string]any{}, want: time.Time{}},
		{name: "unparseable json.Number", claims: map[string]any{"exp": json.Number("not-a-number")}, want: time.Time{}},
		{name: "wrong type", claims: map[string]any{"exp": "soon"}, want: time.Time{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, expirationFromClaims(tt.claims))
		})
	}
}
