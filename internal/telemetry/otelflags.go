package telemetry

import (
	"math"
	"os"
	"strconv"

	"github.com/spf13/pflag"
)

// OtelConfig is the per-signal OTel export policy. Everything defaults on with
// sane levers; the whole layer is additionally gated by an OTLP endpoint being
// present (see enabled()) and by the master Enabled toggle. Transport (endpoint,
// protocol, headers, service name, metric interval) stays on the standard OTEL_*
// env — these are Pivox policy knobs on top of it.
type OtelConfig struct {
	Enabled          bool    // PIVOX_OTEL_ENABLED — master gate
	LogsEnabled      bool    // PIVOX_OTEL_LOG_ENABLED
	LogLevel         string  // PIVOX_OTEL_LOG_LEVEL — min level for EXPORTED logs
	TracesEnabled    bool    // PIVOX_OTEL_TRACE_ENABLED
	TraceSampleRatio float64 // PIVOX_OTEL_TRACE_SAMPLE_RATIO — head sampling [0,1]
	MetricsEnabled   bool    // PIVOX_OTEL_METRICS_ENABLED
}

// RegisterOtelFlags registers the PIVOX_OTEL_* flags (with env-var defaults) on
// f, so every pivox binary exposes the same OTel controls identically. Pair with
// OtelConfigFromFlags after the command parses.
func RegisterOtelFlags(f *pflag.FlagSet) {
	f.Bool("otel-enabled", envBool("PIVOX_OTEL_ENABLED", true),
		"Master OTel export toggle. Requires an OTLP endpoint too; off => stdout logging only, no traces/metrics.")
	f.Bool("otel-log-enabled", envBool("PIVOX_OTEL_LOG_ENABLED", true),
		"Export logs over OTLP (in addition to stdout).")
	f.String("otel-log-level", envStr("PIVOX_OTEL_LOG_LEVEL", "info"),
		"Min level for logs EXPORTED to OTel (debug|info|warn|error). Stdout keeps --log-level, so debug can stay local.")
	f.Bool("otel-trace-enabled", envBool("PIVOX_OTEL_TRACE_ENABLED", true),
		"Export traces over OTLP.")
	f.Float64("otel-trace-sample-ratio", envFloat("PIVOX_OTEL_TRACE_SAMPLE_RATIO", 1.0),
		"Head trace sampling ratio [0.0-1.0] via ParentBased(TraceIDRatioBased). Clamped to [0,1]; 1.0 samples everything.")
	f.Bool("otel-metrics-enabled", envBool("PIVOX_OTEL_METRICS_ENABLED", true),
		"Export metrics over OTLP. Export interval stays the standard OTEL_METRIC_EXPORT_INTERVAL.")
}

// OtelConfigFromFlags reads the registered PIVOX_OTEL_* flags into an OtelConfig,
// clamping the sample ratio.
func OtelConfigFromFlags(f *pflag.FlagSet) OtelConfig {
	enabled, _ := f.GetBool("otel-enabled")
	logs, _ := f.GetBool("otel-log-enabled")
	level, _ := f.GetString("otel-log-level")
	traces, _ := f.GetBool("otel-trace-enabled")
	ratio, _ := f.GetFloat64("otel-trace-sample-ratio")
	metrics, _ := f.GetBool("otel-metrics-enabled")
	return OtelConfig{
		Enabled:          enabled,
		LogsEnabled:      logs,
		LogLevel:         level,
		TracesEnabled:    traces,
		TraceSampleRatio: clampSampleRatio(ratio),
		MetricsEnabled:   metrics,
	}
}

// clampSampleRatio bounds the head sampling ratio to [0,1]. NaN falls back to
// 1.0 (sample everything) — a bad value should never silently drop all traces.
func clampSampleRatio(r float64) float64 {
	switch {
	case math.IsNaN(r):
		return 1.0
	case r < 0:
		return 0
	case r > 1:
		return 1
	default:
		return r
	}
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}
