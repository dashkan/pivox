// Package rivertrace provides a River middleware that propagates W3C trace
// context through jobs, so a job worked in pivox-worker continues the trace of
// the pivox-cloud request that enqueued it.
//
// River's official otelriver middleware creates the `river.insert_many` and
// `river.work` spans + metrics, but does NOT carry trace context across the
// insert→work boundary — `river.work` would otherwise be a disconnected root
// span. This middleware closes that gap by stashing the trace context in the
// job's metadata on insert and restoring it on work.
//
// Register it BEFORE otelriver in river.Config.Middleware (River applies the
// slice outermost-first), so on insert the context is captured before
// otelriver's span starts, and on work the parent is restored before
// otelriver's `river.work` span starts — making it a child of the enqueuing
// request.
//
// It lives in its own package (not internal/telemetry) so binaries that use
// telemetry but not River — like pivox-agent — don't link River.
package rivertrace

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"github.com/riverqueue/rivercontrib/otelriver"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// traceMetadataKey namespaces the injected trace carrier inside a job's JSON
// metadata so it doesn't collide with other metadata River or the app stores.
const traceMetadataKey = "otel"

// traceKeyProbe is the JSON-encoded form of traceMetadataKey used as a cheap
// negative filter: if a job's raw metadata doesn't contain it, there's no
// carrier to extract and we skip the unmarshal entirely (the common case when
// tracing is off — the middleware runs on every job regardless).
var traceKeyProbe = []byte(`"` + traceMetadataKey + `"`)

// Middleware returns the River trace-propagation middleware. Pair it with
// otelriver, placed AFTER this one in river.Config.Middleware — or use
// Middlewares() to get both in the correct order.
func Middleware() rivertype.Middleware {
	return &middleware{}
}

// Middlewares returns the ordered River middleware slice for distributed
// tracing: this trace-propagation middleware first (outer), then River's
// otelriver. The ordering is load-bearing — see the package doc — so it lives
// here in one place rather than being hand-replicated at each river.NewClient
// call site.
func Middlewares() []rivertype.Middleware {
	return []rivertype.Middleware{Middleware(), otelriver.NewMiddleware(nil)}
}

type middleware struct {
	river.MiddlewareDefaults
}

// InsertMany injects the active trace context into each job's metadata.
func (m *middleware) InsertMany(
	ctx context.Context,
	params []*rivertype.JobInsertParams,
	doInner func(ctx context.Context) ([]*rivertype.JobInsertResult, error),
) ([]*rivertype.JobInsertResult, error) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) > 0 {
		// The carrier is identical for every job in this batch (same ctx), so
		// encode it once rather than per job inside the loop.
		if encodedCarrier, err := json.Marshal(carrier); err == nil {
			for _, p := range params {
				p.Metadata = withTraceCarrier(p.Metadata, encodedCarrier)
			}
		}
	}
	return doInner(ctx)
}

// Work restores the enqueuing request's trace context from job metadata so the
// work span (and everything it does) joins that distributed trace.
func (m *middleware) Work(
	ctx context.Context,
	job *rivertype.JobRow,
	doInner func(context.Context) error,
) error {
	if carrier := traceCarrier(job.Metadata); carrier != nil {
		ctx = otel.GetTextMapPropagator().Extract(ctx, carrier)
	}
	return doInner(ctx)
}

// withTraceCarrier merges the pre-encoded trace carrier into the job's JSON
// metadata under traceMetadataKey, preserving any existing metadata. On
// unparseable input it returns the original metadata rather than risk
// clobbering it.
func withTraceCarrier(metadata []byte, encodedCarrier json.RawMessage) []byte {
	fields := map[string]json.RawMessage{}
	if len(metadata) > 0 {
		if err := json.Unmarshal(metadata, &fields); err != nil {
			return metadata
		}
	}
	fields[traceMetadataKey] = encodedCarrier
	merged, err := json.Marshal(fields)
	if err != nil {
		return metadata
	}
	return merged
}

// traceCarrier extracts the trace carrier from a job's metadata, or nil when
// no trace context was stored.
func traceCarrier(metadata []byte) propagation.MapCarrier {
	// Fast negative path: no metadata, or metadata that can't contain our key.
	// Avoids the full unmarshal on every worked job when tracing is off.
	if len(metadata) == 0 || !bytes.Contains(metadata, traceKeyProbe) {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &fields); err != nil {
		return nil
	}
	raw, ok := fields[traceMetadataKey]
	if !ok {
		return nil
	}
	carrier := propagation.MapCarrier{}
	if err := json.Unmarshal(raw, &carrier); err != nil {
		return nil
	}
	return carrier
}
