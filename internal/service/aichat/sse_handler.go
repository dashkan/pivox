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

// SSEHandler serves the POST /v1/ai:stream endpoint using Server-Sent Events.
// It self-dials the local gRPC AiChat.Stream method and translates proto
// ServerEvents to Vercel AI SDK UI message stream format.
type SSEHandler struct {
	grpcClient aiv1.AiChatClient
	logger     *slog.Logger
}

// NewSSEHandler creates a new SSE handler.
func NewSSEHandler(client aiv1.AiChatClient, logger *slog.Logger) *SSEHandler {
	return &SSEHandler{grpcClient: client, logger: logger}
}

// sseStreamRequest is the JSON body for POST /v1/ai:stream.
type sseStreamRequest struct {
	ConversationName string         `json:"conversation_name"`
	Message          sseUserMessage `json:"message"`
	Model            string         `json:"model,omitempty"`
}

type sseUserMessage struct {
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

	// Parse the parts from the JSON body into proto MessageParts.
	parts, err := unmarshalParts(req.Message.Parts)
	if err != nil {
		http.Error(w, "invalid message parts", http.StatusBadRequest)
		return
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

	// Open gRPC bidi stream, forwarding the auth token.
	authHeader := r.Header.Get("Authorization")
	ctx := metadata.AppendToOutgoingContext(r.Context(), "authorization", authHeader)
	stream, err := h.grpcClient.Stream(ctx, grpc.WaitForReady(true))
	if err != nil {
		sseError(w, flusher, err)
		return
	}

	// Send user message as the first ClientEvent.
	if err := stream.Send(&aiv1.ClientEvent{
		Event: &aiv1.ClientEvent_Message{Message: &aiv1.UserMessage{
			Conversation: req.ConversationName,
			Parts:        parts,
		}},
	}); err != nil {
		sseError(w, flusher, err)
		return
	}
	if err := stream.CloseSend(); err != nil {
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

func sseError(w http.ResponseWriter, flusher http.Flusher, err error) {
	errJSON, _ := json.Marshal(map[string]string{
		"type":  "error",
		"error": err.Error(),
	})
	fmt.Fprintf(w, "data: %s\n\n", errJSON)
	flusher.Flush()
}
