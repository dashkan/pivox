package aichat

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// sseRequestBodyMaxBytes caps the size of an incoming SSE request
// body. The body is just a JSON envelope with a few fields and an
// array of conversation messages — 1 MiB is generous and bounds
// memory while still leaving room for a long-context resumed thread.
const sseRequestBodyMaxBytes = 1 << 20

// SSEHandler serves the POST /v1/ai:streamGenerateContent endpoint
// using Server-Sent Events. It self-dials the local gRPC
// AiChat.StreamGenerateContent method and translates proto
// ServerEvents to Vercel AI SDK UI message stream format.
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

// sseStreamRequest is the JSON body for POST /v1/ai:streamGenerateContent.
// Matches the shape of the underlying GenerateContentRequest, with
// `messages` allowed as either the new InputMessage[] form or the
// older single-message form for transitional clients.
type sseStreamRequest struct {
	Parent            string            `json:"parent"`
	Conversation      string            `json:"conversation,omitempty"`
	Messages          []sseInputMessage `json:"messages"`
	SystemInstruction string            `json:"system_instruction,omitempty"`
}

type sseInputMessage struct {
	Role  string          `json:"role"`
	Parts json.RawMessage `json:"parts"`
}

// ServeHTTP is registered on the top-level httpMux directly (not on
// the auth-wrapped grpc-gateway mux), so the HTTP RequireAuth
// middleware does not run for this route. The handler is a thin
// proxy to AiChat.StreamGenerateContent over an in-process bufconn
// dial; the gRPC AuthInterceptor on that call validates the bearer
// token forwarded as gRPC metadata below. Wrapping this route with
// HTTP auth would double-verify the same token without changing the
// auth boundary. See cmd/pivox-cloud/main.go for the registration
// site comment that pairs with this one.
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, sseRequestBodyMaxBytes)

	var req sseStreamRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if req.Parent == "" {
		http.Error(w, "parent is required", http.StatusBadRequest)
		return
	}
	if len(req.Messages) == 0 {
		http.Error(w, "messages must not be empty", http.StatusBadRequest)
		return
	}

	// Convert each input message's parts JSON to proto. Role is
	// passed through as the wire-shaped string ("user"/"tool"); the
	// gRPC validation interceptor downstream gates on the in-set
	// validator declared on InputMessage.role.
	protoMessages := make([]*aiv1.InputMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if !isAcceptedInputRole(m.Role) {
			http.Error(w, fmt.Sprintf("invalid role %q", m.Role), http.StatusBadRequest)
			return
		}
		parts, err := unmarshalParts(m.Parts)
		if err != nil {
			http.Error(w, "invalid message parts", http.StatusBadRequest)
			return
		}
		protoMessages = append(protoMessages, &aiv1.InputMessage{
			Role:  m.Role,
			Parts: parts,
		})
	}

	// SSE headers.
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Open server-streaming call, forwarding the auth token.
	authHeader := r.Header.Get("Authorization")
	ctx := metadata.AppendToOutgoingContext(r.Context(), "authorization", authHeader)

	stream, err := h.grpcClient.StreamGenerateContent(ctx, &aiv1.GenerateContentRequest{
		Parent:            req.Parent,
		Conversation:      req.Conversation,
		Messages:          protoMessages,
		SystemInstruction: req.SystemInstruction,
	}, grpc.WaitForReady(true))
	if err != nil {
		sseError(w, flusher, err)
		return
	}

	// Pump ServerEvents → SSE events. The full rewrite of this loop
	// (per-chunk translator + `[DONE]` sentinel + abort on ctx
	// cancellation + X-Accel-Buffering header) lands in a follow-up
	// commit. This implementation is a stub: it calls the new
	// marshalChunk (which currently returns ErrNotImplemented) and
	// surfaces any error as a stream-side error frame. Once
	// marshalChunk is implemented, this loop emits valid Vercel
	// UIMessageChunk JSON.
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			sseError(w, flusher, err)
			return
		}
		body, mErr := marshalChunk(ev)
		if mErr != nil {
			sseError(w, flusher, mErr)
			return
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", body)
		flusher.Flush()
	}
}

// isAcceptedInputRole reports whether a wire-side role string is an
// accepted InputMessage role. Empty is treated as "user" for
// forward-compat with clients that omit the field on the JSON side
// (the gRPC validator still gates on the in-set rule below). The
// list mirrors the InputMessage.role buf.validate `in` rule on the
// proto.
func isAcceptedInputRole(s string) bool {
	switch s {
	case "", "user", "tool":
		return true
	default:
		return false
	}
}

func sseError(w http.ResponseWriter, flusher http.Flusher, err error) {
	errJSON, _ := json.Marshal(map[string]string{
		"type":  "error",
		"error": err.Error(),
	})
	_, _ = fmt.Fprintf(w, "data: %s\n\n", errJSON)
	flusher.Flush()
}
