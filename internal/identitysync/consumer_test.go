package identitysync

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/plugin/kotel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// stubHandler lets a test inject Handle failures by record value, and
// records the order of values handled.
type stubHandler struct {
	failValues map[string]bool
	handled    []string
}

func (s *stubHandler) Handle(_ context.Context, raw []byte) error {
	s.handled = append(s.handled, string(raw))
	if s.failValues[string(raw)] {
		return errors.New("stub: handle failed")
	}
	return nil
}

func newConsumerForTest(h eventHandler) *Consumer {
	return &Consumer{handler: h, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func makeRecords(startOffset int64, values ...string) []*kgo.Record {
	out := make([]*kgo.Record, len(values))
	for i, v := range values {
		out[i] = &kgo.Record{Topic: topic, Partition: 0, Offset: startOffset + int64(i), Value: []byte(v)}
	}
	return out
}

// TestProcessPartitionRecords covers the offset decision the at-least-once
// guarantee hinges on: which records are committable, and (on failure)
// which offset the partition must rewind to so nothing is silently lost.
func TestProcessPartitionRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("all succeed: whole batch committable, no rewind", func(t *testing.T) {
		h := &stubHandler{}
		c := newConsumerForTest(h)

		committable, rewindTo, failed := c.processPartitionRecords(ctx, makeRecords(10, "A", "B", "C"))

		assert.False(t, failed)
		assert.Zero(t, rewindTo)
		require.Len(t, committable, 3)
		assert.Equal(t, []string{"A", "B", "C"}, h.handled)
	})

	t.Run("mid-batch failure: only prefix committable, rewind to failed offset, rest skipped", func(t *testing.T) {
		h := &stubHandler{failValues: map[string]bool{"B": true}}
		c := newConsumerForTest(h)

		// offsets 10,11,12 = A,B,C
		committable, rewindTo, failed := c.processPartitionRecords(ctx, makeRecords(10, "A", "B", "C"))

		assert.True(t, failed)
		assert.Equal(t, int64(11), rewindTo, "rewind to B (the failed record), not past it")
		require.Len(t, committable, 1)
		assert.Equal(t, int64(10), committable[0].Offset, "only A is committable")
		// C must NOT be handled this pass — it sits after B and replays on rewind.
		// Without the rewind, C would be lost once a later commit advanced the offset.
		assert.Equal(t, []string{"A", "B"}, h.handled)
	})

	t.Run("first record fails: nothing committable, rewind to first", func(t *testing.T) {
		h := &stubHandler{failValues: map[string]bool{"A": true}}
		c := newConsumerForTest(h)

		committable, rewindTo, failed := c.processPartitionRecords(ctx, makeRecords(10, "A", "B"))

		assert.True(t, failed)
		assert.Equal(t, int64(10), rewindTo)
		assert.Empty(t, committable)
		assert.Equal(t, []string{"A"}, h.handled)
	})
}

// Each attempted record gets a kotel "process" span; a failed Handle records the
// error on its span so a stuck partition is visible in traces, not only logs.
func TestProcessPartitionRecordsProcessSpans(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	c := &Consumer{
		handler: &stubHandler{failValues: map[string]bool{"B": true}},
		logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		tracer:  kotel.NewTracer(kotel.TracerProvider(tp)),
	}

	// A handled OK, B fails — processPartitionRecords attempts both, stops at B.
	_, _, failed := c.processPartitionRecords(context.Background(), makeRecords(10, "A", "B"))
	assert.True(t, failed)

	spans := sr.Ended()
	require.Len(t, spans, 2, "one process span per attempted record (A ok, B failed)")

	var errSpans, okSpans int
	for _, s := range spans {
		if s.Status().Code == codes.Error {
			errSpans++
			assert.NotEmpty(t, s.Events(), "failed span should record the error as an event")
		} else {
			okSpans++
		}
	}
	assert.Equal(t, 1, errSpans, "exactly the failed record's span carries error status")
	assert.Equal(t, 1, okSpans)
}
