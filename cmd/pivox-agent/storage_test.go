package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The agent's data directories are configured by env var, and the agent runs as a
// host process alongside api/worker under one shared environment — so these names
// are an interface, not an implementation detail. Renaming one silently sends the
// agent back to its /var/lib default, where (running as a non-root user) it cannot
// create the directory and degrades to in-memory only: no crash resilience, and the
// only signal is a WARN in the log. Pin the names and the defaults.
//
// The PIVOX_AGENT_ prefix is deliberate: unprefixed PIVOX_* names collide with the
// cloud's own config in that shared environment.
func TestStorageCmdDataDirFlags(t *testing.T) {
	tests := []struct {
		name    string
		flag    string
		env     string
		wantDef string
	}{
		{
			name:    "state dir",
			flag:    "state-dir",
			env:     "PIVOX_AGENT_STATE_DIR",
			wantDef: "/var/lib/pivox/state",
		},
		{
			name:    "cache dir",
			flag:    "cache-dir",
			env:     "PIVOX_AGENT_CACHE_DIR",
			wantDef: "/var/lib/pivox/cache",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+": default when unset", func(t *testing.T) {
			t.Setenv(tt.env, "")

			got, err := storageCmd().Flags().GetString(tt.flag)
			require.NoError(t, err)
			assert.Equal(t, tt.wantDef, got, "production default must not change")
		})

		t.Run(tt.name+": env override", func(t *testing.T) {
			t.Setenv(tt.env, "/tmp/pivox-agent-"+tt.flag)

			got, err := storageCmd().Flags().GetString(tt.flag)
			require.NoError(t, err)
			assert.Equal(t, "/tmp/pivox-agent-"+tt.flag, got)
		})
	}
}

// State and cache must not be nested: cache cleanup walks its own directory, so a
// state DB living under it would be deleted out from under a running agent.
func TestStorageCmdStateAndCacheDefaultsAreSiblings(t *testing.T) {
	f := storageCmd().Flags()

	state, err := f.GetString("state-dir")
	require.NoError(t, err)
	cache, err := f.GetString("cache-dir")
	require.NoError(t, err)

	assert.NotEqual(t, state, cache)
	assert.NotContains(t, state, cache+"/", "state must not live inside the cache dir")
	assert.NotContains(t, cache, state+"/", "cache must not live inside the state dir")
}
