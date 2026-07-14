package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/dashkan/pivox/internal/authn"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
)

// --- URI parsing (pure) ---

func TestParseOrgURI(t *testing.T) {
	t.Parallel()
	org, err := parseOrgURI("pivox://organizations/acme")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)

	for _, bad := range []string{
		"pivox://organizations/",              // empty slug
		"pivox://organizations/acme/spaces/s", // too deep
		"pivox://account",                     // wrong collection
		"https://organizations/acme",          // wrong scheme
	} {
		_, err := parseOrgURI(bad)
		assert.Error(t, err, "must reject %q", bad)
	}
}

func TestParseSpaceURI(t *testing.T) {
	t.Parallel()
	org, space, err := parseSpaceURI("pivox://organizations/acme/spaces/prod")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "prod", space)

	for _, bad := range []string{
		"pivox://organizations/acme",              // no space
		"pivox://organizations/acme/spaces/",      // empty space
		"pivox://organizations//spaces/prod",      // empty org
		"pivox://organizations/acme/widgets/prod", // wrong sub-collection
	} {
		_, _, err := parseSpaceURI(bad)
		assert.Error(t, err, "must reject %q", bad)
	}
}

// --- Fake McpServiceClient ---

// fakeMcpClient records the authorization metadata seen on each call
// (proving the bearer was forwarded) and the request it received, and
// returns canned responses.
type fakeMcpClient struct {
	gotAuthz    string
	gotGetOrg   *mcpv1.GetOrgRequest
	gotGetSpace *mcpv1.GetSpaceRequest
	gotListSp   *mcpv1.ListSpacesRequest
	gotListOrgs *mcpv1.ListOrgsRequest

	orgs   []*mcpv1.Organization
	spaces []*mcpv1.Space
}

func (f *fakeMcpClient) record(ctx context.Context) {
	if md, ok := metadata.FromOutgoingContext(ctx); ok {
		if vals := md.Get("authorization"); len(vals) > 0 {
			f.gotAuthz = vals[0]
		}
	}
}

func (f *fakeMcpClient) GetAccount(ctx context.Context, _ *mcpv1.GetAccountRequest, _ ...grpc.CallOption) (*mcpv1.Account, error) {
	f.record(ctx)
	return &mcpv1.Account{Subject: "sub-1", Email: "me@x.test", DisplayName: "Me"}, nil
}

func (f *fakeMcpClient) ListOrgs(ctx context.Context, in *mcpv1.ListOrgsRequest, _ ...grpc.CallOption) (*mcpv1.ListOrgsResponse, error) {
	f.record(ctx)
	f.gotListOrgs = in
	return &mcpv1.ListOrgsResponse{Orgs: f.orgs}, nil
}

func (f *fakeMcpClient) GetOrg(ctx context.Context, in *mcpv1.GetOrgRequest, _ ...grpc.CallOption) (*mcpv1.Organization, error) {
	f.record(ctx)
	f.gotGetOrg = in
	return &mcpv1.Organization{Slug: in.GetOrg(), DisplayName: "Org " + in.GetOrg()}, nil
}

func (f *fakeMcpClient) ListSpaces(ctx context.Context, in *mcpv1.ListSpacesRequest, _ ...grpc.CallOption) (*mcpv1.ListSpacesResponse, error) {
	f.record(ctx)
	f.gotListSp = in
	return &mcpv1.ListSpacesResponse{Spaces: f.spaces}, nil
}

func (f *fakeMcpClient) GetSpace(ctx context.Context, in *mcpv1.GetSpaceRequest, _ ...grpc.CallOption) (*mcpv1.Space, error) {
	f.record(ctx)
	f.gotGetSpace = in
	return &mcpv1.Space{Org: in.GetOrg(), Slug: in.GetSpace(), DisplayName: "Space " + in.GetSpace()}, nil
}

// authedContext returns a context carrying the TokenInfo the real bearer
// middleware would set for `token` — obtained by driving the actual
// middleware, so the ExtraRawToken plumbing is exercised end to end
// rather than faked.
func authedContext(t *testing.T, token string) context.Context {
	t.Helper()
	verifier := stubVerifier{uid: "0192a000-0000-7000-8000-000000000001"}
	mw := auth.RequireBearerToken(NewTokenVerifier(verifier), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: "https://pivox.test/mcp/.well-known/oauth-protected-resource",
	})
	var captured context.Context
	h := mw(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = r.Context()
	}))
	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	h.ServeHTTP(httptest.NewRecorder(), req)
	require.NotNil(t, captured, "bearer middleware did not admit the request")
	return captured
}

// stubVerifier accepts any token, returning a fixed identity.
type stubVerifier struct{ uid string }

func (s stubVerifier) VerifyToken(_ context.Context, _ string) (*authn.Identity, error) {
	// A future `exp` is required: the bearer middleware rejects a token
	// with a zero expiration ("token missing expiration").
	return &authn.Identity{UID: s.uid, Email: "me@x.test", Claims: map[string]any{
		"name": "Me",
		"exp":  float64(time.Now().Add(time.Hour).Unix()),
	}}, nil
}

// TestForward_MissingTokenInfo pins the fail-closed edge: a context with
// no verified token yields an error rather than an unauthenticated call.
func TestForward_MissingTokenInfo(t *testing.T) {
	t.Parallel()
	_, err := forward(context.Background())
	assert.Error(t, err)
}

// TestReadOrg_ForwardsBearer drives the org resource-read path and
// asserts the caller's bearer was forwarded onto the McpService call.
func TestReadOrg_ForwardsBearer(t *testing.T) {
	t.Parallel()
	fake := &fakeMcpClient{}
	srv := &server{client: fake}
	ctx := authedContext(t, "tok-123")

	res, err := srv.readOrg(ctx, &mcpsdk.ReadResourceRequest{
		Params: &mcpsdk.ReadResourceParams{URI: "pivox://organizations/acme"},
	})
	require.NoError(t, err)
	require.Len(t, res.Contents, 1)
	assert.Contains(t, res.Contents[0].Text, `"slug":"acme"`)
	assert.Equal(t, "acme", fake.gotGetOrg.GetOrg(), "URI slug must reach the RPC")
	assert.Equal(t, "Bearer tok-123", fake.gotAuthz, "the caller's bearer must be forwarded")
}

// TestReadSpace_ForwardsBearer drives the space resource-read path.
func TestReadSpace_ForwardsBearer(t *testing.T) {
	t.Parallel()
	fake := &fakeMcpClient{}
	srv := &server{client: fake}
	ctx := authedContext(t, "tok-abc")

	res, err := srv.readSpace(ctx, &mcpsdk.ReadResourceRequest{
		Params: &mcpsdk.ReadResourceParams{URI: "pivox://organizations/acme/spaces/prod"},
	})
	require.NoError(t, err)
	require.Len(t, res.Contents, 1)
	assert.Contains(t, res.Contents[0].Text, `"slug":"prod"`)
	assert.Equal(t, "acme", fake.gotGetSpace.GetOrg())
	assert.Equal(t, "prod", fake.gotGetSpace.GetSpace())
	assert.Equal(t, "Bearer tok-abc", fake.gotAuthz)
}

// TestComplete_OrgThenProgressiveSpace pins the completion chain: the
// {organization} variable completes against the caller's orgs, and the
// {space} variable completes against the org already resolved in the
// progressive-completion context.
func TestComplete_OrgThenProgressiveSpace(t *testing.T) {
	t.Parallel()
	fake := &fakeMcpClient{
		orgs:   []*mcpv1.Organization{{Slug: "acme"}, {Slug: "acorn"}},
		spaces: []*mcpv1.Space{{Org: "acme", Slug: "prod"}, {Org: "acme", Slug: "preview"}},
	}
	srv := &server{client: fake}
	ctx := authedContext(t, "tok-xyz")

	orgRes, err := srv.complete(ctx, &mcpsdk.CompleteRequest{
		Params: &mcpsdk.CompleteParams{
			Ref:      &mcpsdk.CompleteReference{Type: "ref/resource", URI: orgURITemplate},
			Argument: mcpsdk.CompleteParamsArgument{Name: "organization", Value: "ac"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"acme", "acorn"}, orgRes.Completion.Values)
	assert.Equal(t, "ac", fake.gotListOrgs.GetNamePrefix(), "the partial drives name_prefix")
	assert.Equal(t, "Bearer tok-xyz", fake.gotAuthz)

	spaceRes, err := srv.complete(ctx, &mcpsdk.CompleteRequest{
		Params: &mcpsdk.CompleteParams{
			Ref:      &mcpsdk.CompleteReference{Type: "ref/resource", URI: spaceURITemplate},
			Argument: mcpsdk.CompleteParamsArgument{Name: "space", Value: "pr"},
			Context:  &mcpsdk.CompleteContext{Arguments: map[string]string{"organization": "acme"}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"prod", "preview"}, spaceRes.Completion.Values)
	assert.Equal(t, "acme", fake.gotListSp.GetOrg(), "space completion is scoped to the progressively-chosen org")
	assert.Equal(t, "pr", fake.gotListSp.GetNamePrefix())
}

// TestComplete_SpaceWithoutOrg returns nothing rather than erroring when
// the org hasn't been resolved yet.
func TestComplete_SpaceWithoutOrg(t *testing.T) {
	t.Parallel()
	fake := &fakeMcpClient{}
	srv := &server{client: fake}
	ctx := authedContext(t, "tok")

	res, err := srv.complete(ctx, &mcpsdk.CompleteRequest{
		Params: &mcpsdk.CompleteParams{
			Ref:      &mcpsdk.CompleteReference{Type: "ref/resource", URI: spaceURITemplate},
			Argument: mcpsdk.CompleteParamsArgument{Name: "space", Value: "p"},
		},
	})
	require.NoError(t, err)
	assert.Empty(t, res.Completion.Values)
	assert.Nil(t, fake.gotListSp, "no org resolved → no space listing")
}

// TestGetAccountTool_ForwardsBearer covers a tool path end to end.
func TestGetAccountTool_ForwardsBearer(t *testing.T) {
	t.Parallel()
	fake := &fakeMcpClient{}
	srv := &server{client: fake}
	ctx := authedContext(t, "tok-acct")

	res, _, err := srv.getAccount(ctx, &mcpsdk.CallToolRequest{}, getAccountInput{})
	require.NoError(t, err)
	require.Len(t, res.Content, 1)
	text := res.Content[0].(*mcpsdk.TextContent).Text
	assert.Contains(t, text, `"subject":"sub-1"`)
	assert.Equal(t, "Bearer tok-acct", fake.gotAuthz)
}
