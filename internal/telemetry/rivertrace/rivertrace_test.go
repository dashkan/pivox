package rivertrace

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/riverqueue/river/rivertype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func sampledContext(t *testing.T) (context.Context, trace.TraceID, trace.SpanID) {
	t.Helper()
	traceID, err := trace.TraceIDFromHex("0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("0123456789abcdef")
	require.NoError(t, err)
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})
	return trace.ContextWithSpanContext(context.Background(), sc), traceID, spanID
}

// The core contract: a trace context present when a job is INSERTED must be
// reconstructable when the job is WORKED — even though work happens in a
// different process with a fresh context. This is what links the pivox-cloud
// insert and the pivox-worker execution into one distributed trace.
func TestMiddleware_RoundTripsTraceContextThroughMetadata(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	insertCtx, traceID, spanID := sampledContext(t)

	mw := &middleware{}

	params := []*rivertype.JobInsertParams{{}}
	_, err := mw.InsertMany(insertCtx, params, func(context.Context) ([]*rivertype.JobInsertResult, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, params[0].Metadata, "trace context must be injected into job metadata")

	// Worker side: a fresh context (no ambient span), only the persisted job.
	job := &rivertype.JobRow{Metadata: params[0].Metadata}
	var got trace.SpanContext
	err = mw.Work(context.Background(), job, func(ctx context.Context) error {
		got = trace.SpanContextFromContext(ctx)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, traceID, got.TraceID())
	assert.Equal(t, spanID, got.SpanID())
	assert.True(t, got.IsRemote(), "extracted parent should be marked remote")
}

// No active span on insert → nothing injected, existing metadata untouched.
func TestMiddleware_NoActiveSpanLeavesMetadataUnchanged(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})

	mw := &middleware{}
	params := []*rivertype.JobInsertParams{{Metadata: []byte(`{"foo":"bar"}`)}}
	_, err := mw.InsertMany(context.Background(), params, func(context.Context) ([]*rivertype.JobInsertResult, error) {
		return nil, nil
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"foo":"bar"}`, string(params[0].Metadata))
}

// Injection must preserve pre-existing job metadata, namespacing the trace
// carrier under its own key.
func TestMiddleware_PreservesExistingMetadata(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	insertCtx, _, _ := sampledContext(t)

	mw := &middleware{}
	params := []*rivertype.JobInsertParams{{Metadata: []byte(`{"foo":"bar"}`)}}
	_, err := mw.InsertMany(insertCtx, params, func(context.Context) ([]*rivertype.JobInsertResult, error) {
		return nil, nil
	})
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(params[0].Metadata, &m))
	assert.Equal(t, "bar", m["foo"])
	assert.Contains(t, m, traceMetadataKey)
}

// Work with no trace metadata must be a clean pass-through (no panic, ctx
// flows unchanged).
func TestMiddleware_WorkWithoutTraceMetadataIsPassThrough(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	mw := &middleware{}
	called := false
	err := mw.Work(context.Background(), &rivertype.JobRow{Metadata: []byte(`{"foo":"bar"}`)}, func(context.Context) error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}
