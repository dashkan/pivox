package aichat

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// SSEHandler serves the POST /v1/ai:streamGenerateContent endpoint
// using Server-Sent Events. It self-dials the local gRPC
// AiChat.StreamGenerateContent method and translates proto
// ServerEvents to Vercel AI SDK UI message stream format.
type SSEHandler struct {
	grpcClient aiv1.AiChatClient
	logger     *slog.Logger
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(client aiv1.AiChatClient, logger *slog.Logger) *SSEHandler {
	return &SSEHandler{grpcClient: client, logger: logger}
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

func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	uid, ok := server.AuthenticatedUID(r.Context())
	if !ok || uid == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

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

	// Convert each input message's parts JSON to proto.
	protoMessages := make([]*aiv1.InputMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		role, ok := protoRoleFromString(m.Role)
		if !ok {
			http.Error(w, fmt.Sprintf("invalid role %q", m.Role), http.StatusBadRequest)
			return
		}
		parts, err := unmarshalParts(m.Parts)
		if err != nil {
			http.Error(w, "invalid message parts", http.StatusBadRequest)
			return
		}
		protoMessages = append(protoMessages, &aiv1.InputMessage{
			Role:  role,
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

	// Pump ServerEvents → SSE events.
	for {
		ev, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			sseError(w, flusher, err)
			return
		}
		sseLine := translateToSSE(ev)
		fmt.Fprintf(w, "data: %s\n\n", sseLine)
		flusher.Flush()
	}
}

// protoRoleFromString maps the JSON-side role string to the proto
// Role enum. Unrecognized values fail with `ok=false` so the SSE
// handler can return 400 — silently coercing to USER would let
// clients smuggle assistant-tagged turns into the request and have
// them written as user-role on the wire (which the gRPC layer would
// also reject, but earlier-validation is friendlier).
func protoRoleFromString(s string) (aiv1.Role, bool) {
	switch s {
	case "user", "":
		return aiv1.Role_USER, true
	case "assistant":
		return aiv1.Role_ASSISTANT, true
	case "system":
		return aiv1.Role_SYSTEM, true
	case "tool":
		return aiv1.Role_TOOL, true
	default:
		return aiv1.Role_ROLE_UNSPECIFIED, false
	}
}

func sseError(w http.ResponseWriter, flusher http.Flusher, err error) {
	errJSON, _ := json.Marshal(map[string]string{
		"type":  "error",
		"error": err.Error(),
	})
	fmt.Fprintf(w, "data: %s\n\n", errJSON)
	flusher.Flush()
}
