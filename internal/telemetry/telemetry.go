// Package telemetry wires the process logger and the OpenTelemetry SDK
// (traces + metrics + logs) for the pivox binaries (pivox-cloud, pivox-worker,
// pivox-agent) in a single call, so each main shares one bootstrap instead of
// copy-pasting logger construction + provider setup + shutdown.
//
// OTLP configuration comes entirely from the standard OTEL_* environment
// variables — which the Aspire AppHost injects (OTEL_EXPORTER_OTLP_ENDPOINT,
// OTEL_EXPORTER_OTLP_PROTOCOL, OTEL_SERVICE_NAME, ...) — so the only
// pivox-specific inputs are the service name and the log level.
//
// When no OTLP endpoint is present (production today, a plain `go run`, or
// tests) Setup installs no providers and returns a JSON-stdout logger plus a
// no-op shutdown: nothing connects, nothing is exported, and callers can still
// `defer shutdown(ctx)` unconditionally.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/exporters/autoexport"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

// shutdownTimeout bounds the flush-and-close on exit so a dead collector
// can't hang process shutdown.
const shutdownTimeout = 5 * time.Second

// Config is the small amount of identity each binary supplies; everything
// else (endpoint, protocol, sampling) is read from OTEL_* env.
type Config struct {
	// ServiceName is the fallback service.name (e.g. "pivox-cloud") used when
	// OTEL_SERVICE_NAME is not set in the environment, and the instrumentation
	// scope name for the slog->OTel bridge.
	ServiceName string
	// LogLevel is the slog level: "debug" | "info" | "warn" | "error".
	// Empty or unrecognized falls back to info.
	LogLevel string
}

// Setup builds the process logger, installs the global OTel providers (traces +
// metrics + logs) and W3C propagators, sets slog.Default, and returns the
// logger plus a shutdown that flushes and closes everything.
//
// The returned logger always writes JSON to stdout. When OTLP export is enabled
// (an OTEL_EXPORTER_OTLP_ENDPOINT is present and the SDK isn't disabled) it ALSO
// forwards records to the OTel log pipeline via the otelslog bridge — so logs
// reach the collector/dashboard and, for *Context log calls under an active
// span, carry the trace_id/span_id for log<->trace correlation.
//
// Call exactly once per process at startup. The returned logger and shutdown
// are always non-nil; shutdown is safe to defer even when export is disabled.
func Setup(ctx context.Context, cfg Config) (*slog.Logger, func(context.Context) error, error) {
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: parseLevel(cfg.LogLevel)})

	// Always install the propagator — it's cheap and lets trace context flow
	// across boundaries (incoming traceparent from the web apps, outgoing to
	// other services) even when export is disabled here.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !enabled() {
		logger := slog.New(jsonHandler)
		slog.SetDefault(logger)
		return logger, func(context.Context) error { return nil }, nil
	}

	// Bootstrap logger for the SDK's own internal errors (export retries, queue
	// drops) and resource warnings, until the fanned-out logger is built below.
	bootstrap := slog.New(jsonHandler)
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		bootstrap.Warn("opentelemetry sdk error", "error", err)
	}))

	res := newResource(ctx, cfg, bootstrap)

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("create otlp span exporter: %w", err)
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter),
	)
	otel.SetTracerProvider(tracerProvider)

	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		// Roll back what we already installed so we don't leave the process
		// half-instrumented.
		_ = tracerProvider.Shutdown(ctx)
		return nil, nil, fmt.Errorf("create otlp metric reader: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(metricReader),
	)
	otel.SetMeterProvider(meterProvider)

	// Go runtime metrics (GC pauses, goroutine count, heap) — cheap and
	// high-value; non-fatal if it fails.
	if err := otelruntime.Start(otelruntime.WithMeterProvider(meterProvider)); err != nil {
		bootstrap.Warn("opentelemetry runtime metrics failed to start", "error", err)
	}

	logExporter, err := autoexport.NewLogExporter(ctx)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)
		_ = meterProvider.Shutdown(ctx)
		return nil, nil, fmt.Errorf("create otlp log exporter: %w", err)
	}
	loggerProvider := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
	)
	otellogglobal.SetLoggerProvider(loggerProvider)

	// Fan out: JSON to stdout (local readability + container log capture) AND
	// the OTel bridge (collector/dashboard + trace correlation).
	logger := slog.New(slog.NewMultiHandler(
		jsonHandler,
		otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(loggerProvider)),
	))
	slog.SetDefault(logger)

	logger.Info("opentelemetry enabled", "service", cfg.ServiceName)

	return logger, func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		// errors.Join so a failure shutting down one provider still runs the
		// others and all surface to the caller.
		return errors.Join(
			tracerProvider.Shutdown(ctx),
			meterProvider.Shutdown(ctx),
			loggerProvider.Shutdown(ctx),
		)
	}, nil
}

// parseLevel maps a log-level string to slog.Level; empty/unknown => info.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// enabled reports whether OTLP export should be configured. True when an
// OTLP endpoint is present in the environment (generic or per-signal) and
// the SDK is not explicitly disabled. The Aspire AppHost sets
// OTEL_EXPORTER_OTLP_ENDPOINT for the resources it launches.
func enabled() bool {
	if isTruthy(os.Getenv("OTEL_SDK_DISABLED")) {
		return false
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") != ""
}

func isTruthy(v string) bool {
	// OTEL_SDK_DISABLED is a spec-defined boolean: case-insensitive "true".
	return strings.EqualFold(v, "true") || v == "1"
}

// newResource builds the OTel resource. Env-provided identity wins:
// OTEL_SERVICE_NAME (Aspire sets it to the resource name) and
// OTEL_RESOURCE_ATTRIBUTES are honored via WithFromEnv; cfg.ServiceName is
// only a fallback when the env doesn't name the service.
func newResource(ctx context.Context, cfg Config, logger *slog.Logger) *resource.Resource {
	var attrs []attribute.KeyValue
	if os.Getenv("OTEL_SERVICE_NAME") == "" && cfg.ServiceName != "" {
		attrs = append(attrs, semconv.ServiceName(cfg.ServiceName))
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		// resource.New returns a usable (merged) resource alongside
		// non-fatal errors like schema-URL conflicts. Log and proceed.
		logger.Warn("opentelemetry resource built with warnings", "error", err)
	}
	if res == nil {
		return resource.Default()
	}
	return res
}
