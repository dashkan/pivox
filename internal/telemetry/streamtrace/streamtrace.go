// Package streamtrace propagates W3C trace context through gRPC stream messages
// and opens a span per message — so a long-lived stream is NOT modeled as one
// giant span (which never exports while the connection is healthy and roots all
// in-stream work under it). Instead each message gets a short producer/consumer
// span that exports immediately, and the trace context carried in the message
// links the consumer's span to whatever produced it.
//
// It is direction-agnostic: the same primitives instrument server-streaming,
// client-streaming, and bidirectional RPCs. The sender calls Send (or Inject)
// to stamp trace context into the message; the receiver calls Receive to start
// a consumer span continuing that trace. The message type only has to expose a
// settable string map for the carrier (proto `map<string,string> trace_context`).
//
// This mirrors what rivertrace does for River jobs — propagate context through
// the payload, span the unit of work, not the transport's lifetime.
package streamtrace

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/dashkan/pivox/internal/telemetry/streamtrace"

// Inject returns a W3C trace-context carrier for the active span in ctx, or nil
// when there is no sampled span. Assign the result to a stream message's
// trace-context map field before sending so the receiver can continue the trace.
//
// Send already does this; Inject is exposed for send paths that can't use Send
// (e.g. messages constructed and pushed elsewhere, like ConnectionManager fan-out).
func Inject(ctx context.Context) map[string]string {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return nil
	}
	return carrier
}

// Send instruments sending one stream message, regardless of stream direction:
// it opens a short producer span named name, injects that span's trace context
// into msg via setCarrier, sends msg via send, and ends the span (recording any
// send error). Pair it with Receive on the other end.
//
// setCarrier assigns the carrier to the message's trace-context field (proto
// doesn't generate a setter, so the caller supplies a one-line closure, e.g.
// func(m *agentv1.AgentMessage, c map[string]string) { m.TraceContext = c }).
func Send[T any](
	ctx context.Context,
	name string,
	msg T,
	setCarrier func(T, map[string]string),
	send func(T) error,
) error {
	ctx, span := otel.Tracer(tracerName).Start(ctx, name, trace.WithSpanKind(trace.SpanKindProducer))
	defer span.End()

	if carrier := Inject(ctx); carrier != nil {
		setCarrier(msg, carrier)
	}
	if err := send(msg); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "stream send failed")
		return err
	}
	return nil
}

// Receive starts a consumer span for a received stream message, continuing the
// trace carried in the message (carrier may be nil/empty — pass the message's
// trace-context map). The returned context carries the span; the caller MUST
// end it (defer span.End()). Work done under the returned context (DB queries,
// downstream RPCs) nests under this per-message span instead of the stream's
// lifetime span.
func Receive(ctx context.Context, name string, carrier map[string]string) (context.Context, trace.Span) {
	if len(carrier) > 0 {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(carrier))
	}
	return otel.Tracer(tracerName).Start(ctx, name, trace.WithSpanKind(trace.SpanKindConsumer))
}
