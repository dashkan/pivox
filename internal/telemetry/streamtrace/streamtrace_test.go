package streamtrace

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// withTracing installs a real (always-sampling) tracer provider + W3C
// propagator for the duration of a test, so spans are recording and Inject
// actually produces a carrier. Without a provider the global no-op tracer
// yields non-recording spans and Inject returns nil — which is itself a case
// we test separately.
func withTracing(t *testing.T) {
	t.Helper()
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())))
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
}

// Inject returns nil when no span is active, so a message's trace-context field
// stays empty rather than carrying a meaningless carrier.
func TestInject_NoActiveSpan_ReturnsNil(t *testing.T) {
	withTracing(t)
	assert.Nil(t, Inject(context.Background()))
}

// Inject returns a carrier containing the W3C traceparent for the active span.
func TestInject_ActiveSpan_ReturnsCarrier(t *testing.T) {
	withTracing(t)
	ctx, span := otel.Tracer("test").Start(context.Background(), "op")
	defer span.End()

	carrier := Inject(ctx)
	require.NotNil(t, carrier)
	assert.Contains(t, carrier, "traceparent")
}

// The core contract: trace context captured on the producer side must be
// reconstructable on Receive in a fresh context, continuing the same trace —
// this is what links a producer in one process to the consumer's span in
// another (across server/client/bidi streams alike).
func TestInjectReceive_ContinuesSameTrace(t *testing.T) {
	withTracing(t)

	parentCtx, parentSpan := otel.Tracer("test").Start(context.Background(), "producer")
	defer parentSpan.End()
	carrier := Inject(parentCtx)
	require.NotEmpty(t, carrier)

	// Consumer side: a fresh context carrying only the propagated carrier.
	ctx, span := Receive(context.Background(), "Svc/Recv", carrier)
	defer span.End()

	got := trace.SpanContextFromContext(ctx)
	require.True(t, got.IsValid())
	assert.Equal(t, parentSpan.SpanContext().TraceID(), got.TraceID(),
		"consumer span must join the producer's trace")
}

// Send opens a producer span, injects its context into the message, and sends.
func TestSend_InjectsAndSends(t *testing.T) {
	withTracing(t)

	type msgT struct{ tc map[string]string }
	msg := &msgT{}
	sent := false
	err := Send(context.Background(), "Svc/Send", msg,
		func(m *msgT, c map[string]string) { m.tc = c },
		func(m *msgT) error { sent = true; return nil },
	)
	require.NoError(t, err)
	assert.True(t, sent, "Send must call the send func")
	assert.NotEmpty(t, msg.tc, "Send must inject trace context into the message")
	assert.Contains(t, msg.tc, "traceparent")
}

// Send returns (and records) the underlying send error.
func TestSend_SendErrorIsReturned(t *testing.T) {
	withTracing(t)
	type msgT struct{ tc map[string]string }
	wantErr := errors.New("boom")
	err := Send(context.Background(), "Svc/Send", &msgT{},
		func(m *msgT, c map[string]string) { m.tc = c },
		func(*msgT) error { return wantErr },
	)
	assert.ErrorIs(t, err, wantErr)
}

// Receive with a nil carrier must not panic and returns a usable new root span
// (nothing to continue).
func TestReceive_NilCarrier_StartsRootSpan(t *testing.T) {
	withTracing(t)
	ctx, span := Receive(context.Background(), "Svc/Recv", nil)
	defer span.End()
	assert.False(t, trace.SpanContextFromContext(ctx).IsRemote())
}
