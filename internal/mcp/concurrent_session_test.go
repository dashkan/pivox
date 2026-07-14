// Copyright 2025 Pivox
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mcp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/mcp"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
)

// echoClient is a fake McpServiceClient whose GetAccount echoes back the
// bearer that was forwarded to it (read off the outgoing gRPC metadata).
// It holds no per-call state, so its answer is a pure function of the
// call's context — which is exactly what lets the test detect a stale or
// cross-wired context.
type echoClient struct {
	mcpv1.McpServiceClient
}

func bearerFromCtx(ctx context.Context) string {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		return ""
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ""
	}
	return strings.TrimPrefix(vals[0], "Bearer ")
}

func (c *echoClient) GetAccount(ctx context.Context, _ *mcpv1.GetAccountRequest, _ ...grpc.CallOption) (*mcpv1.Account, error) {
	tok := bearerFromCtx(ctx)
	return &mcpv1.Account{Subject: tok, Email: tok + "@x.test", DisplayName: tok}, nil
}

// concurrencyVerifier admits any bearer with a future expiry. The raw
// token — not the identity — is what the handler forwards and the
// echoClient echoes, so a single fixed UID is fine.
type concurrencyVerifier struct{}

func (concurrencyVerifier) VerifyToken(_ context.Context, _ string) (*authn.Identity, error) {
	return &authn.Identity{
		UID:    "0192a000-0000-7000-8000-000000000001",
		Claims: map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())},
	}, nil
}

// bearerRoundTripper injects a fixed Authorization header on every
// request — one per MCP client, so two clients present two identities.
type bearerRoundTripper struct {
	token string
	base  http.RoundTripper
}

func (b bearerRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func connectMCP(t *testing.T, ctx context.Context, endpoint, token string) *mcpsdk.ClientSession {
	t.Helper()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	transport := &mcpsdk.StreamableClientTransport{
		Endpoint:   endpoint,
		HTTPClient: &http.Client{Transport: bearerRoundTripper{token: token, base: http.DefaultTransport}},
		// Request/response only — no persistent server-initiated SSE
		// stream (we make no server-push assertions, and the standalone
		// stream just holds connections open for the test's lifetime).
		DisableStandaloneSSE: true,
	}
	session, err := client.Connect(ctx, transport, nil)
	require.NoError(t, err, "connect as %s", token)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callGetAccountSubject(t *testing.T, ctx context.Context, session *mcpsdk.ClientSession) string {
	t.Helper()
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_account", Arguments: map[string]any{}})
	require.NoError(t, err)
	require.False(t, res.IsError, "tool returned an error result")
	require.Len(t, res.Content, 1)
	text := res.Content[0].(*mcpsdk.TextContent).Text
	return text
}

// TestConcurrentSessions_ForwardOwnBearer is the F4 lock-down: two MCP
// clients with distinct bearers, whose tool calls interleave (serially
// then concurrently). Each call must forward ITS OWN token and receive
// ITS OWN account — never the other caller's. If the SDK ever reused a
// stale request context across a multi-request session, the echoed
// subject would be wrong and this test would fail.
func TestConcurrentSessions_ForwardOwnBearer(t *testing.T) {
	t.Parallel()

	handler := mcp.NewHandler(mcp.Config{
		Client:      &echoClient{},
		Verifier:    concurrencyVerifier{},
		ResourceURL: "https://pivox.test/mcp",
		Issuer:      "https://kc.test/realms/pivox",
	})
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx := context.Background()
	endpoint := srv.URL + "/mcp"
	sessA := connectMCP(t, ctx, endpoint, "tok-A")
	sessB := connectMCP(t, ctx, endpoint, "tok-B")

	// Serial interleave.
	assert.Contains(t, callGetAccountSubject(t, ctx, sessA), `"subject":"tok-A"`)
	assert.Contains(t, callGetAccountSubject(t, ctx, sessB), `"subject":"tok-B"`)
	assert.Contains(t, callGetAccountSubject(t, ctx, sessA), `"subject":"tok-A"`)
	assert.Contains(t, callGetAccountSubject(t, ctx, sessB), `"subject":"tok-B"`)

	// Concurrent interleave: hammer both sessions in parallel and assert
	// every call still resolves to its own caller (no cross-wiring under
	// contention).
	var wg sync.WaitGroup
	const rounds = 20
	errs := make(chan string, rounds*2)
	for range rounds {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if got := callGetAccountSubject(t, ctx, sessA); !strings.Contains(got, `"subject":"tok-A"`) {
				errs <- "A saw: " + got
			}
		}()
		go func() {
			defer wg.Done()
			if got := callGetAccountSubject(t, ctx, sessB); !strings.Contains(got, `"subject":"tok-B"`) {
				errs <- "B saw: " + got
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("cross-wired session: %s", e)
	}
}
