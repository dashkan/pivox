package workflows

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestParseRunStateFilter covers the AIP-160 `state = "VALUE"` filter parser
// that ListWorkflowRuns lowers into a scoped-query narg.
func TestParseRunStateFilter(t *testing.T) {
	t.Run("empty filter is unset (match-all)", func(t *testing.T) {
		got, err := parseRunStateFilter("")
		require.NoError(t, err)
		assert.False(t, got.Valid, "empty filter must not constrain state")
	})

	t.Run("blank filter is unset", func(t *testing.T) {
		got, err := parseRunStateFilter("   ")
		require.NoError(t, err)
		assert.False(t, got.Valid)
	})

	t.Run("quoted state value", func(t *testing.T) {
		got, err := parseRunStateFilter(`state = "RUNNING"`)
		require.NoError(t, err)
		assert.True(t, got.Valid)
		assert.Equal(t, "RUNNING", got.String)
	})

	t.Run("bare identifier state value", func(t *testing.T) {
		got, err := parseRunStateFilter(`state = SUCCEEDED`)
		require.NoError(t, err)
		assert.True(t, got.Valid)
		assert.Equal(t, "SUCCEEDED", got.String)
	})

	invalid := []struct {
		name   string
		filter string
	}{
		{"unknown state", `state = "BOGUS"`},
		{"lowercase state", `state = "running"`},
		{"unspecified state", `state = "STATE_UNSPECIFIED"`},
		{"unsupported field", `subject = "assets/a1"`},
		{"non-equality operator", `state != "RUNNING"`},
		{"conjunction unsupported", `state = "RUNNING" AND subject = "x"`},
		{"malformed expression", `state =`},
	}
	for _, tt := range invalid {
		t.Run(tt.name+" is InvalidArgument", func(t *testing.T) {
			_, err := parseRunStateFilter(tt.filter)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err),
				"filter %q must be rejected, not silently ignored", tt.filter)
		})
	}
}
