package telemetry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// otelEnvKeys are the OTLP env vars that gate enablement. Tests clear
// them to a known baseline before setting per-case values, so the
// ambient shell environment (or Aspire's injected vars) can't leak in.
var otelEnvKeys = []string{
	"OTEL_EXPORTER_OTLP_ENDPOINT",
	"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT",
	"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT",
	"OTEL_SDK_DISABLED",
}

func TestEnabled(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{name: "no endpoint configured", env: nil, want: false},
		{
			name: "generic OTLP endpoint enables",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317"},
			want: true,
		},
		{
			name: "per-signal traces endpoint enables",
			env:  map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://localhost:4318/v1/traces"},
			want: true,
		},
		{
			name: "OTEL_SDK_DISABLED overrides a present endpoint",
			env: map[string]string{
				"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4317",
				"OTEL_SDK_DISABLED":           "true",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Baseline: clear every gating var (empty reads as unset).
			for _, k := range otelEnvKeys {
				t.Setenv(k, "")
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			assert.Equal(t, tt.want, enabled())
		})
	}
}

// When no OTLP endpoint is configured, Setup must not error and must
// return a non-nil shutdown that is safe to defer — so every binary can
// `defer shutdown(ctx)` unconditionally without a nil check.
func TestSetupDisabledReturnsSafeNoopShutdown(t *testing.T) {
	for _, k := range otelEnvKeys {
		t.Setenv(k, "")
	}

	shutdown, err := Setup(context.Background(), Config{ServiceName: "pivox-test"})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	assert.NoError(t, shutdown(context.Background()))
}
