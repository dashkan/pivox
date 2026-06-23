package telemetry

import (
	"context"
	"log/slog"
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

// When no OTLP endpoint is configured, Setup must not error and must return a
// usable JSON logger plus a non-nil shutdown that is safe to defer — so every
// binary gets a logger and can `defer shutdown(ctx)` unconditionally without a
// nil check.
func TestSetupDisabledReturnsLoggerAndNoopShutdown(t *testing.T) {
	for _, k := range otelEnvKeys {
		t.Setenv(k, "")
	}

	logger, shutdown, err := Setup(context.Background(), Config{ServiceName: "pivox-test", LogLevel: "debug"})
	require.NoError(t, err)
	require.NotNil(t, logger, "Setup must always return a usable logger")
	require.NotNil(t, shutdown)
	logger.Info("smoke") // must not panic
	assert.NoError(t, shutdown(context.Background()))
}

func TestParseLevel(t *testing.T) {
	tests := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"DEBUG":   slog.LevelDebug,
		" warn ":  slog.LevelWarn,
		"error":   slog.LevelError,
		"info":    slog.LevelInfo,
		"":        slog.LevelInfo,
		"verbose": slog.LevelInfo, // unrecognized falls back to info
	}
	for in, want := range tests {
		t.Run(in, func(t *testing.T) {
			assert.Equal(t, want, parseLevel(in))
		})
	}
}
