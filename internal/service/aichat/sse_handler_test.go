package aichat

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// SSE handler tests exercise the HTTP-layer adapter behavior —
// URL parsing, body decode (protojson), SSE framing (headers,
// `[DONE]` terminator, error chunks), and client-disconnect
// handling. They do NOT cover the gRPC service's auth/membership/
// permission chain; that's the gRPC service's own concern and is
// covered by gRPC integration tests via internal/testutil/grpcharness.
//
// Setup uses a stub AiChatServer driven by a per-test scripted
// channel of ServerEvents, dialed via bufconn. No Postgres, no
// interceptors — the SUT here is the HTTP-to-SSE adapter.

// stubAiChatServer scripts a ServerEvent sequence for
// StreamGenerateContent and returns errors on demand. Other RPCs
// fall through to UnimplementedAiChatServer (which returns
// Unimplemented), so a test that accidentally calls one fails loud.
type stubAiChatServer struct {
	aiv1.UnimplementedAiChatServer
	events    []*aiv1.ServerEvent
	streamErr error // returned from StreamGenerateContent immediately when set
	sendErr   error // returned after the first successful Send
}

func (s *stubAiChatServer) StreamGenerateContent(req *aiv1.GenerateContentRequest, stream grpc.ServerStreamingServer[aiv1.ServerEvent]) error {
	if s.streamErr != nil {
		return s.streamErr
	}
	for i, ev := range s.events {
		if err := stream.Send(ev); err != nil {
			return err
		}
		if s.sendErr != nil && i == 0 {
			return s.sendErr
		}
	}
	return nil
}

// flushingRecorder wraps httptest.ResponseRecorder to satisfy
// http.Flusher. The plain ResponseRecorder doesn't implement
// Flusher, which would cause the SSE handler to short-circuit with
// "streaming not supported"; in production an http.Server's
// ResponseWriter does flush. Override is a no-op since the recorder
// buffers everything anyway.
type flushingRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flushingRecorder) Flush() {}

// startStubServer spins up a bufconn-backed gRPC server with the
// supplied stubAiChatServer registered. Returns an aiv1.AiChatClient
// dialed against it. Server is cleaned up via t.Cleanup.
func startStubServer(t *testing.T, stub *stubAiChatServer) aiv1.AiChatClient {
	t.Helper()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	aiv1.RegisterAiChatServer(srv, stub)

	serveDone := make(chan struct{})
	go func() {
		_ = srv.Serve(lis)
		close(serveDone)
	}()

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		_ = conn.Close()
		srv.GracefulStop()
		<-serveDone
	})

	return aiv1.NewAiChatClient(conn)
}

// newSSEHandlerWithEvents builds an SSE handler driven by a stub
// that emits the given events in order, then returns clean EOF.
func newSSEHandlerWithEvents(t *testing.T, events ...*aiv1.ServerEvent) *SSEHandler {
	t.Helper()
	client := startStubServer(t, &stubAiChatServer{events: events})
	return NewSSEHandler(SSEHandlerConfig{
		Client: client,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

// newRequest builds an SSE POST request with the path-value table
// populated as if Go 1.22 mux had matched the parametric pattern
// `POST /v1/organizations/{org}/users/{userVerb}`. The body is
// passed as a raw string (the handler does protojson decode).
func newRequest(t *testing.T, org, userVerb, body string) *http.Request {
	t.Helper()
	url := fmt.Sprintf("/v1/organizations/%s/users/%s", org, userVerb)
	r := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	r.SetPathValue("org", org)
	r.SetPathValue("userVerb", userVerb)
	return r
}

// minimalBody is a syntactically-valid request body (one user
// message with one text part) in the Vercel UIMessage wire shape
// our flat MessagePart accepts directly. Used by tests whose
// subject is the HTTP-layer behavior, not the body itself.
const minimalBody = `{"messages":[{"role":"user","parts":[{"type":"text","text":"hi"}]}]}`

// ─── Tests ──────────────────────────────────────────────────

// TestSSE_HappyPath asserts the SSE handler streams a scripted
// ServerEvent sequence as Vercel-shaped chunks, in order, with the
// terminal `[DONE]` sentinel, and that the SSE response headers are
// set correctly (including `X-Accel-Buffering: no` so nginx-compat
// proxies don't batch the stream).
func TestSSE_HappyPath(t *testing.T) {
	t.Parallel()

	h := newSSEHandlerWithEvents(t,
		&aiv1.ServerEvent{Event: &aiv1.ServerEvent_Start{Start: &aiv1.Start{MessageId: "m1"}}},
		&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextStart{TextStart: &aiv1.TextStart{Id: "m1"}}},
		&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{Id: "m1", Delta: "Hello"}}},
		&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{Id: "m1", Delta: " world"}}},
		&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextEnd{TextEnd: &aiv1.TextEnd{Id: "m1"}}},
		&aiv1.ServerEvent{Event: &aiv1.ServerEvent_Finish{Finish: &aiv1.Finish{FinishReason: "stop"}}},
	)

	rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := newRequest(t, "acme", "alice:streamGenerateContent", minimalBody)
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "text/event-stream", rr.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", rr.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", rr.Header().Get("Connection"))
	assert.Equal(t, "no", rr.Header().Get("X-Accel-Buffering"))

	body := rr.Body.String()

	// Each chunk arrives as a `data: <json>\n\n` line. Verify the
	// SSE framing and chunk order.
	lines := splitSSEData(body)
	require.Len(t, lines, 7, "expected 6 chunks + [DONE], got %d lines\n%s", len(lines), body)

	assert.JSONEq(t, `{"type":"start","messageId":"m1"}`, lines[0])
	assert.JSONEq(t, `{"type":"text-start","id":"m1"}`, lines[1])
	assert.JSONEq(t, `{"type":"text-delta","id":"m1","delta":"Hello"}`, lines[2])
	assert.JSONEq(t, `{"type":"text-delta","id":"m1","delta":" world"}`, lines[3])
	assert.JSONEq(t, `{"type":"text-end","id":"m1"}`, lines[4])
	assert.JSONEq(t, `{"type":"finish","finishReason":"stop"}`, lines[5])
	assert.Equal(t, "[DONE]", lines[6])
}

// TestSSE_URLParse covers the URL path → parent translation, both
// the happy path and the dispatcher-mismatch / malformed cases that
// the handler must reject before opening the upstream stream.
func TestSSE_URLParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		org      string
		userVerb string
		wantCode int
	}{
		{
			name:     "happy",
			org:      "acme",
			userVerb: "alice:streamGenerateContent",
			wantCode: http.StatusOK,
		},
		{
			name:     "missing_org",
			org:      "",
			userVerb: "alice:streamGenerateContent",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "missing_user_verb",
			org:      "acme",
			userVerb: "",
			wantCode: http.StatusBadRequest,
		},
		{
			name:     "verb_only_no_user",
			org:      "acme",
			userVerb: ":streamGenerateContent",
			wantCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newSSEHandlerWithEvents(t,
				&aiv1.ServerEvent{Event: &aiv1.ServerEvent_Finish{Finish: &aiv1.Finish{FinishReason: "stop"}}},
			)
			rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}
			req := newRequest(t, tt.org, tt.userVerb, minimalBody)
			h.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantCode, rr.Code, "body=%s", rr.Body.String())
		})
	}
}

// TestSSE_BadBody covers body-decode failures. protojson surfaces
// malformed JSON as a 400; an empty messages array is rejected by
// the handler explicitly because the gRPC validator's empty-message
// rejection arrives as a stream-level error chunk (worse UX than a
// pre-flight 400).
func TestSSE_BadBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantBody string
	}{
		{
			name:     "invalid_json",
			body:     `{not json}`,
			wantCode: http.StatusBadRequest,
			wantBody: "bad request",
		},
		{
			name:     "empty_messages",
			body:     `{"messages":[]}`,
			wantCode: http.StatusBadRequest,
			wantBody: "messages must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newSSEHandlerWithEvents(t)
			rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}
			req := newRequest(t, "acme", "alice:streamGenerateContent", tt.body)
			h.ServeHTTP(rr, req)
			assert.Equal(t, tt.wantCode, rr.Code)
			assert.Contains(t, rr.Body.String(), tt.wantBody)
		})
	}
}

// TestSSE_OversizeBody asserts the 1 MiB cap is enforced before the
// body is decoded. Without the cap, a multi-GB JSON would tie up
// memory; the handler must reject early.
func TestSSE_OversizeBody(t *testing.T) {
	t.Parallel()

	h := newSSEHandlerWithEvents(t)

	junk := strings.Repeat("a", sseRequestBodyMaxBytes+64)
	body := `{"messages":[{"role":"user","parts":[{"text":{"text":"` + junk + `"}}]}]}`

	rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := newRequest(t, "acme", "alice:streamGenerateContent", body)
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code,
		"oversized body must be rejected before opening the stream")
}

// TestSSE_UpstreamError asserts that an error from the gRPC
// streaming RPC surfaces as a Vercel-shaped `error` chunk (with the
// `errorText` field, NOT the legacy `error` field), and the stream
// closes immediately afterward without emitting `[DONE]`. Errors and
// clean completions are terminal in different ways: clean → DONE
// sentinel; error → error chunk only.
func TestSSE_UpstreamError(t *testing.T) {
	t.Parallel()

	client := startStubServer(t, &stubAiChatServer{
		streamErr: status.Error(codes.Internal, "upstream broke"),
	})
	h := NewSSEHandler(SSEHandlerConfig{
		Client: client,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := newRequest(t, "acme", "alice:streamGenerateContent", minimalBody)
	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code, "headers already flushed before error")
	body := rr.Body.String()
	lines := splitSSEData(body)
	require.Len(t, lines, 1, "expected exactly one error chunk and no DONE: %s", body)

	// Internal-class errors collapse to a generic string via
	// apierr.ToSSEErrorText so the raw upstream message ("upstream
	// broke") doesn't leak to the UI. This assertion also serves as
	// the Vercel-shape contract test: the chunk must have `type` and
	// `errorText`, NOT the legacy `error` field.
	assert.JSONEq(t, `{"type":"error","errorText":"internal error"}`, lines[0])
	assert.NotContains(t, body, "[DONE]",
		"error path must NOT emit the clean-completion DONE sentinel")
}

// TestSSE_PartialThenError covers the case where one chunk arrives
// cleanly and then the upstream stream errors. The first chunk
// flushes; the error then surfaces as an `error` chunk.
func TestSSE_PartialThenError(t *testing.T) {
	t.Parallel()

	client := startStubServer(t, &stubAiChatServer{
		events: []*aiv1.ServerEvent{
			{Event: &aiv1.ServerEvent_Start{Start: &aiv1.Start{MessageId: "m1"}}},
		},
		sendErr: status.Error(codes.DeadlineExceeded, "timeout"),
	})
	h := NewSSEHandler(SSEHandlerConfig{
		Client: client,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}
	req := newRequest(t, "acme", "alice:streamGenerateContent", minimalBody)
	h.ServeHTTP(rr, req)

	body := rr.Body.String()
	lines := splitSSEData(body)
	require.Len(t, lines, 2, "expected start chunk + error chunk: %s", body)

	assert.JSONEq(t, `{"type":"start","messageId":"m1"}`, lines[0])
	// DeadlineExceeded maps to "request timed out" via ToSSEErrorText
	// — the upstream raw "timeout" message stays internal.
	assert.JSONEq(t, `{"type":"error","errorText":"request timed out"}`, lines[1])
	assert.NotContains(t, body, "[DONE]")
}

// TestSSE_ClientDisconnect asserts that when the HTTP request's
// context is cancelled mid-stream (client navigated away / aborted
// the fetch), the handler exits cleanly without panicking and
// without trying to emit anything to the (closed) connection. The
// scripted stream blocks indefinitely; cancelling the request
// context forces the Recv loop to surface ctx.Err.
func TestSSE_ClientDisconnect(t *testing.T) {
	t.Parallel()

	// Use an unbuffered channel that the stub reads from to simulate
	// a long-running stream. We never feed it, so Recv blocks until
	// the test cancels the request context.
	stallEvents := make(chan *aiv1.ServerEvent)
	stubBlocking := &blockingAiChatServer{events: stallEvents}

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	aiv1.RegisterAiChatServer(srv, stubBlocking)
	serveDone := make(chan struct{})
	go func() {
		_ = srv.Serve(lis)
		close(serveDone)
	}()
	t.Cleanup(func() {
		close(stallEvents)
		srv.GracefulStop()
		<-serveDone
	})

	conn, err := grpc.NewClient(
		"passthrough:///bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	h := NewSSEHandler(SSEHandlerConfig{
		Client: aiv1.NewAiChatClient(conn),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	ctx, cancel := context.WithCancel(context.Background())
	req := newRequest(t, "acme", "alice:streamGenerateContent", minimalBody)
	req = req.WithContext(ctx)
	rr := &flushingRecorder{ResponseRecorder: httptest.NewRecorder()}

	// Run the handler in a goroutine so we can cancel the ctx from
	// outside. Cancel after a short delay to ensure the Recv loop is
	// actually waiting on the stream.
	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()

	cancel()
	select {
	case <-done:
		// Pass: handler exited cleanly within the deadline.
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not exit within 5s of ctx cancellation")
	}

	// Body should NOT contain `[DONE]` — ctx cancellation is not a
	// clean completion.
	assert.NotContains(t, rr.Body.String(), "[DONE]")
}

// blockingAiChatServer's StreamGenerateContent ranges over a
// channel that never sends (until the test closes it), simulating a
// long-running model call. Lets TestSSE_ClientDisconnect drive
// cancellation while the upstream is mid-Recv.
type blockingAiChatServer struct {
	aiv1.UnimplementedAiChatServer
	events <-chan *aiv1.ServerEvent
}

func (b *blockingAiChatServer) StreamGenerateContent(req *aiv1.GenerateContentRequest, stream grpc.ServerStreamingServer[aiv1.ServerEvent]) error {
	for {
		select {
		case ev, ok := <-b.events:
			if !ok {
				return nil
			}
			if err := stream.Send(ev); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return stream.Context().Err()
		}
	}
}

// splitSSEData parses an SSE response body into the list of `data:`
// payloads. Each payload is the JSON between `data: ` and the
// trailing `\n\n`. The `[DONE]` sentinel comes through as the
// literal string "[DONE]".
//
// Example input:
//
//	data: {"type":"text-delta","id":"m1","delta":"hi"}
//
//	data: [DONE]
//
// Returns [`{"type":"text-delta","id":"m1","delta":"hi"}`, `[DONE]`].
func splitSSEData(body string) []string {
	var out []string
	for _, frame := range strings.Split(body, "\n\n") {
		frame = strings.TrimSpace(frame)
		if frame == "" {
			continue
		}
		// Each frame begins with `data: `. Tests don't emit
		// multi-line `data:` frames, so a simple prefix-strip is
		// sufficient.
		const prefix = "data: "
		if !strings.HasPrefix(frame, prefix) {
			continue
		}
		out = append(out, strings.TrimPrefix(frame, prefix))
	}
	return out
}
