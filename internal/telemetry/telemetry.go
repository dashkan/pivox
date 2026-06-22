// Package telemetry wires the OpenTelemetry SDK (traces + metrics) for
// the pivox binaries (pivox-cloud, pivox-worker, pivox-agent).
//
// Configuration comes entirely from the standard OTEL_* environment
// variables — which the Aspire AppHost injects (OTEL_EXPORTER_OTLP_ENDPOINT,
// OTEL_EXPORTER_OTLP_PROTOCOL, OTEL_SERVICE_NAME, ...) — so there is no
// pivox-specific config surface and nothing to thread through CLI flags.
//
// When no OTLP endpoint is present (production today, a plain `go run`, or
// tests) Setup installs no providers and returns a no-op shutdown: nothing
// connects, nothing is emitted, and callers can still `defer shutdown(ctx)`
// unconditionally.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/exporters/autoexport"
	otelruntime "go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.28.0"
)

// shutdownTimeout bounds the flush-and-close on exit so a dead collector
// can't hang process shutdown.
const shutdownTimeout = 5 * time.Second

// Config is the small amount of identity the binary supplies; everything
// else (endpoint, protocol, sampling) is read from OTEL_* env.
type Config struct {
	// ServiceName is the fallback service.name (e.g. "pivox-cloud") used
	// only when OTEL_SERVICE_NAME is not set in the environment.
	ServiceName string
	// Logger receives OTel's internal errors (export failures, etc.) and
	// the enable/disable line. Defaults to slog.Default() when nil.
	Logger *slog.Logger
}

// Setup configures the global OTel TracerProvider + MeterProvider and the
// W3C propagators, returning a shutdown that flushes and closes them.
//
// It is safe to call exactly once per process at startup. The returned
// shutdown is always non-nil.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	// Always install the propagator — it's cheap and lets trace context
	// flow across boundaries (incoming traceparent headers from the web
	// apps, outgoing to other services) even if export is disabled here.
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	if !enabled() {
		return func(context.Context) error { return nil }, nil
	}

	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// Route OTel's internal errors (export retries, queue drops) through
	// slog instead of the SDK's default stderr writer.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		logger.Warn("opentelemetry sdk error", "error", err)
	}))

	res := newResource(ctx, cfg, logger)

	spanExporter, err := autoexport.NewSpanExporter(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp span exporter: %w", err)
	}
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(spanExporter),
	)
	otel.SetTracerProvider(tracerProvider)

	metricReader, err := autoexport.NewMetricReader(ctx)
	if err != nil {
		// Roll back the tracer provider we already installed so we don't
		// leave the process half-instrumented.
		_ = tracerProvider.Shutdown(ctx)
		return nil, fmt.Errorf("create otlp metric reader: %w", err)
	}
	meterProvider := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(metricReader),
	)
	otel.SetMeterProvider(meterProvider)

	// Go runtime metrics (GC pauses, goroutine count, heap) — cheap and
	// high-value; non-fatal if it fails.
	if err := otelruntime.Start(otelruntime.WithMeterProvider(meterProvider)); err != nil {
		logger.Warn("opentelemetry runtime metrics failed to start", "error", err)
	}

	logger.Info("opentelemetry enabled", "service", cfg.ServiceName)

	return func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		defer cancel()
		// errors.Join so a failure shutting down one provider still runs
		// the other and both surface to the caller.
		return errors.Join(
			tracerProvider.Shutdown(ctx),
			meterProvider.Shutdown(ctx),
		)
	}, nil
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
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != ""
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
