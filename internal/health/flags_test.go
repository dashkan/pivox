package health

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// NOT parallel, at any level: a subtest uses t.Setenv, which panics if the test
// or ANY parent is parallel.
func TestRegisterFlag(t *testing.T) {
	t.Run("defaults to the shared port", func(t *testing.T) {
		// All three binaries share this default on purpose: in production each runs
		// in its own container, so one port means one probe config everywhere.
		f := pflag.NewFlagSet("t", pflag.ContinueOnError)
		RegisterFlag(f)

		assert.Equal(t, DefaultAddr, Addr(f))
	})

	t.Run("the flag wins", func(t *testing.T) {
		f := pflag.NewFlagSet("t", pflag.ContinueOnError)
		RegisterFlag(f)
		require.NoError(t, f.Parse([]string{"--debug-port", ":9091"}))

		assert.Equal(t, ":9091", Addr(f))
	})

	t.Run("the env var overrides the default", func(t *testing.T) {
		t.Setenv("PIVOX_DEBUG_PORT", ":9092")

		f := pflag.NewFlagSet("t", pflag.ContinueOnError)
		RegisterFlag(f)

		// Aspire sets PIVOX_DEBUG_PORT per process, because in dev all three share
		// one host and would otherwise collide on :9090.
		assert.Equal(t, ":9092", Addr(f))
	})
}
