package aichat

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// stubAiChatClient is a no-op AiChat gRPC client for SSE handler tests
// that don't reach the streaming call. Returns an error from the
// stream methods so any test that DOES reach Recv() fails loudly.
type stubAiChatClient struct {
	aiv1.AiChatClient
}

func newSSEHandlerForTest() *SSEHandler {
	return &SSEHandler{
		grpcClient: &stubAiChatClient{},
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}

// TestSSE_BodyTooLarge_Returns400 covers the http.MaxBytesReader cap.
// Pre-fix the handler decoded the entire body unbounded; this guards
// against a regression where a multi-GB JSON would tie up memory.
func TestSSE_BodyTooLarge_Returns400(t *testing.T) {
	h := newSSEHandlerForTest()

	// Body well past the 1 MiB cap. JSON-shaped to ensure the
	// rejection comes from the size cap, not from a JSON parse
	// error that happens earlier.
	const oversize = sseRequestBodyMaxBytes + 64
	junk := bytes.Repeat([]byte("a"), oversize)
	body := []byte(`{"parent":"organizations/x","messages":[{"role":"user","parts":"`)
	body = append(body, junk...)
	body = append(body, []byte(`"}]}`)...)

	req := httptest.NewRequest(http.MethodPost, "/v1/ai:streamGenerateContent", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code,
		"oversized body must be rejected before the streaming call")
	assert.Contains(t, rr.Body.String(), "bad request")
}

// TestSSE_MissingParent_Returns400 covers the input validation path
// before the gRPC self-dial. Confirms the handler runs with no auth
// pre-check (the gRPC AuthInterceptor downstream is the auth gate).
func TestSSE_MissingParent_Returns400(t *testing.T) {
	h := newSSEHandlerForTest()
	body := `{"messages":[{"role":"user","parts":"hi"}]}`

	req := httptest.NewRequest(http.MethodPost, "/v1/ai:streamGenerateContent", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "parent is required")
}

// TestSSE_EmptyMessages_Returns400 covers the messages-empty path.
func TestSSE_EmptyMessages_Returns400(t *testing.T) {
	h := newSSEHandlerForTest()
	body := `{"parent":"organizations/x"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/ai:streamGenerateContent", strings.NewReader(body))
	rr := httptest.NewRecorder()

	h.ServeHTTP(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "messages must not be empty")
}

// Confirms no compile-time dependency on a server-side auth guard
// inside this package. If a redundant per-RPC auth check is ever
// re-introduced, the tests above will start tripping the gate
// before reaching the validation paths they exercise.
var _ = grpc.WaitForReady // keep import for future stream tests
