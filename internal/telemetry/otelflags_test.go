package telemetry

import (
	"math"
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func TestClampSampleRatio(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   float64
		want float64
	}{
		{"in range", 0.25, 0.25},
		{"zero", 0, 0},
		{"one", 1, 1},
		{"below zero clamps to 0", -0.5, 0},
		{"above one clamps to 1", 1.5, 1},
		{"NaN falls back to 1 (never drop all)", math.NaN(), 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, clampSampleRatio(tt.in))
		})
	}
}

// Defaults must be "everything on" so a binary that just registers + reads the
// flags (no env, no CLI overrides) exports fully.
func TestOtelConfigFromFlagsDefaults(t *testing.T) {
	t.Parallel()

	f := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterOtelFlags(f)
	assert.NoError(t, f.Parse(nil))

	got := OtelConfigFromFlags(f)
	assert.Equal(t, OtelConfig{
		Enabled:          true,
		LogsEnabled:      true,
		LogLevel:         "info",
		TracesEnabled:    true,
		TraceSampleRatio: 1.0,
		MetricsEnabled:   true,
	}, got)
}

func TestOtelConfigFromFlagsOverrides(t *testing.T) {
	t.Parallel()

	f := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterOtelFlags(f)
	assert.NoError(t, f.Parse([]string{
		"--otel-enabled=false",
		"--otel-log-level=warn",
		"--otel-trace-sample-ratio=2.0", // out of range -> clamped
		"--otel-metrics-enabled=false",
	}))

	got := OtelConfigFromFlags(f)
	assert.False(t, got.Enabled)
	assert.Equal(t, "warn", got.LogLevel)
	assert.Equal(t, 1.0, got.TraceSampleRatio) // clamped from 2.0
	assert.False(t, got.MetricsEnabled)
	assert.True(t, got.LogsEnabled)   // untouched default
	assert.True(t, got.TracesEnabled) // untouched default
}
