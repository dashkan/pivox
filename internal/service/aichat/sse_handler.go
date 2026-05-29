package aichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/dashkan/pivox/internal/apierr"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
)

// sseRequestBodyMaxBytes caps the size of an incoming SSE request
// body. The body is just a JSON envelope with a few fields and an
// array of conversation messages — 1 MiB is generous and bounds
// memory while still leaving room for a long-context resumed thread.
const sseRequestBodyMaxBytes = 1 << 20

// sseStreamVerb is the suffix this handler claims on the user-scoped
// AIP path. The dispatcher in main.go uses this constant to decide
// whether to route to the SSE handler or fall through to the gateway
// for other custom methods (e.g. `:generateContent`) on the same
// parent.
const sseStreamVerb = "streamGenerateContent"

// SSEHandler serves the user-scoped streaming chat endpoint via
// Server-Sent Events. It self-dials the local gRPC
// AiChat.StreamGenerateContent method and translates proto
// ServerEvents to Vercel AI SDK UIMessageChunks on the wire.
//
// Registered path:
//
//	POST /v1/organizations/{org}/users/{userVerb}
//
// where `{userVerb}` is `<user>:streamGenerateContent`. The
// dispatcher in main.go forwards non-stream verbs (e.g.
// `:generateContent`) to the grpc-gateway so the unary call still
// works on the same parent.
type SSEHandler struct {
	grpcClient aiv1.AiChatClient
	logger     *slog.Logger
}

// SSEHandlerConfig is the constructor input for SSEHandler. Suffixed
// to avoid colliding with the package-level Config used by Server.
type SSEHandlerConfig struct {
	// Client is the local AiChat gRPC client the handler self-dials
	// to drive StreamGenerateContent. Required.
	Client aiv1.AiChatClient
	// Logger is the slog logger used for stream-side error lines.
	// Required.
	Logger *slog.Logger
}

// NewSSEHandler constructs an SSE handler from cfg. Panics on a
// missing required field — startup-time programmer error, fail loud
// on boot.
func NewSSEHandler(cfg SSEHandlerConfig) *SSEHandler {
	if cfg.Client == nil {
		panic("aichat: SSEHandlerConfig.Client is required")
	}
	if cfg.Logger == nil {
		panic("aichat: SSEHandlerConfig.Logger is required")
	}
	return &SSEHandler{grpcClient: cfg.Client, logger: cfg.Logger}
}

// SSEStreamVerb is the AIP custom-method verb suffix this handler
// owns on the user-scoped path. Exported for the registration site
// (cmd/pivox-cloud/main.go) so the dispatcher constant and the
// SSE handler stay in sync.
func SSEStreamVerb() string { return sseStreamVerb }

// ServeHTTP serves the SSE stream. The request URL is parsed by the
// caller (main.go's dispatcher) for the `{org}` and `{userVerb}`
// path values; the body is a Vercel-shaped UIMessage[] envelope
// (decoded via protojson) plus optional `conversation` and
// `systemInstruction` fields. The dispatcher has already verified
// the verb suffix is `streamGenerateContent` by the time this runs.
//
// HTTP auth is deliberately NOT applied to this route. The handler
// is a thin proxy to AiChat.StreamGenerateContent over an in-process
// bufconn dial; the gRPC AuthInterceptor validates the bearer token
// forwarded as gRPC metadata below. Wrapping with HTTP auth would
// double-verify the same token. See cmd/pivox-cloud/main.go for the
// registration-site comment that pairs with this one.
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Parse parent from URL path values.
	org, user, err := parsePathOrgUser(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	parent := fmt.Sprintf("organizations/%s/users/%s", org, user)

	// Decode body as a Vercel-shaped GenerateContentRequest.
	// DiscardUnknown=true lets useChat's extras (id, trigger,
	// messageId — Vercel-side fields with no Pivox counterpart)
	// pass through without rejection.
	r.Body = http.MaxBytesReader(w, r.Body, sseRequestBodyMaxBytes)
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	req := &aiv1.GenerateContentRequest{}
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(bodyBytes, req); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	// URL is the source of truth for parent. Overwrite any value
	// the caller put in the body so a misbehaving client can't
	// smuggle a different parent past the URL-level permission
	// check downstream.
	req.Parent = parent
	if len(req.Messages) == 0 {
		http.Error(w, "messages must not be empty", http.StatusBadRequest)
		return
	}

	// SSE headers. `X-Accel-Buffering: no` instructs nginx (and any
	// other nginx-compat reverse proxy) NOT to buffer the response,
	// which would otherwise batch the entire stream and break
	// real-time delivery. The value travels with the response, so
	// any nginx in the path — dev or prod — respects it.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Open the server-streaming call. Forward the bearer token to
	// the gRPC AuthInterceptor as the `authorization` metadata
	// entry. The stream's context derives from the HTTP request's
	// context, so client disconnect propagates to the upstream
	// gRPC call as ctx cancellation.
	authHeader := r.Header.Get("Authorization")
	ctx := metadata.AppendToOutgoingContext(r.Context(), "authorization", authHeader)

	stream, err := h.grpcClient.StreamGenerateContent(ctx, req, grpc.WaitForReady(true))
	if err != nil {
		writeErrorChunk(w, flusher, err)
		return
	}

	// Pump ServerEvents → SSE chunks. The terminal `data: [DONE]\n\n`
	// sentinel is emitted on clean EOF; on error mid-stream an
	// `error` chunk goes out instead. Client-initiated disconnects
	// surface here as ctx-cancellation errors from Recv; we don't
	// try to emit anything to a disconnected client (the chunk
	// would never arrive), just terminate the loop cleanly.
	for {
		ev, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			if isContextCancelled(r.Context()) {
				// Client disconnect — nothing to send, just exit.
				return
			}
			writeErrorChunk(w, flusher, recvErr)
			return
		}
		chunk, mErr := marshalChunk(ev)
		if mErr != nil {
			h.logger.WarnContext(ctx, "marshal SSE chunk failed", "error", mErr)
			writeErrorChunk(w, flusher, mErr)
			return
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", chunk); err != nil {
			// Write failure means the client has disconnected; the
			// next Recv will surface that via ctx cancellation.
			return
		}
		flusher.Flush()
	}

	// Stream completed cleanly — emit the terminator. Vercel's
	// `parseJsonEventStream` ignores this line, but emitting it
	// matches the SDK's own backend behavior and keeps the wire
	// interoperable with any intermediary that looks for the
	// explicit marker.
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// parsePathOrgUser extracts the `{org}` slug and the `{user}` UUID
// from the request's path-value table populated by the mux pattern.
// The dispatcher in main.go has already verified the verb suffix.
func parsePathOrgUser(r *http.Request) (org, user string, err error) {
	org = r.PathValue("org")
	if org == "" {
		return "", "", errors.New("missing org in path")
	}
	userVerb := r.PathValue("userVerb")
	if userVerb == "" {
		return "", "", errors.New("missing user in path")
	}
	// The dispatcher pre-validated the suffix; we just trim it.
	const suffix = ":" + sseStreamVerb
	if len(userVerb) <= len(suffix) {
		return "", "", errors.New("malformed user:verb segment")
	}
	user = userVerb[:len(userVerb)-len(suffix)]
	if user == "" {
		return "", "", errors.New("missing user in path")
	}
	return org, user, nil
}

// isContextCancelled reports whether ctx has been cancelled. Used to
// distinguish a Recv error caused by client disconnect from a real
// upstream failure.
func isContextCancelled(ctx context.Context) bool {
	return ctx.Err() != nil
}

// writeErrorChunk emits a Vercel-shaped `error` chunk and flushes.
// Best-effort: if the client has hung up the write fails silently,
// which is the right behavior — there's no recipient.
//
// Error text passes through `apierr.ToSSEErrorText` so messages from
// caller-safe codes (PermissionDenied, NotFound, InvalidArgument)
// surface verbatim while Internal/Unknown collapse to a generic
// string — no driver errors or wrapped pgx detail leaks past the
// SSE boundary.
func writeErrorChunk(w http.ResponseWriter, flusher http.Flusher, err error) {
	body, _ := json.Marshal(map[string]string{
		"type":      "error",
		"errorText": apierr.ToSSEErrorText(err),
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
	flusher.Flush()
}
