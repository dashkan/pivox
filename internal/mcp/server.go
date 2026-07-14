package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/authn"
	mcpv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/mcp/v1"
)

const (
	serverName    = "pivox"
	serverVersion = "0.1.0"

	// Resource URIs + templates. The concrete account resource is the
	// whoami; the two templates address orgs and spaces by slug and back
	// the resources/read + completion/complete surfaces that Codex and
	// VS Code consume.
	accountResourceURI = "pivox://account"
	orgURITemplate     = "pivox://organizations/{organization}"
	spaceURITemplate   = "pivox://organizations/{organization}/spaces/{space}"

	// completionLimit caps how many completion candidates we return per
	// request — completion menus are interactive, not bulk listings.
	completionLimit = 25

	// wellKnownPRMPath is the Protected Resource Metadata discovery path,
	// suffixed under the /mcp mount. RequireBearerToken advertises the absolute
	// form of this to clients via the WWW-Authenticate challenge.
	wellKnownPRMPath = "/mcp/.well-known/oauth-protected-resource"
)

// mcpScopes are advertised in the PRM as the scopes a client should request. The
// bare `organization` scope drives Keycloak's org picker (binding the token to
// one org); the `mcp:*` scopes carry the resource-URL audience via their KC
// audience mappers. `offline_access` is here because clients (e.g. Claude Code)
// register a DCR client with EXACTLY these scopes as its optional set, then
// append `offline_access` at authorize time (KC's AS metadata advertises it, for
// token refresh). If it's not in the DCR client's scopes, KC rejects the
// authorize with invalid_scope — so the PRM must advertise it to keep the
// DCR-registered client and the authorize request in sync.
var mcpScopes = []string{"organization", "mcp:tools", "mcp:resources", "offline_access"}

// protoJSON renders proto responses to the JSON the MCP tools/resources
// return. UseProtoNames keeps the wire field names (snake_case) and
// EmitUnpopulated makes empty pages/tokens explicit for the agent.
var protoJSON = protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}

// Config configures the MCP HTTP handler.
type Config struct {
	// Client is the in-process McpService gRPC client (dialed over the
	// server's own bufconn). Every tool/resource/completion handler
	// forwards the caller's MCP bearer on this client so McpService's
	// interceptor chain re-verifies + authorizes. Required.
	Client mcpv1.McpServiceClient
	// Verifier validates MCP bearer tokens at the HTTP edge. It MUST be an
	// OIDC verifier whose audience is ResourceURL — the anti-confusion
	// boundary that stops a token minted for the main Pivox API from being
	// replayed here. Required.
	Verifier authn.Service
	// ResourceURL is this server's canonical resource identifier and token
	// audience, e.g. https://pivox.example/mcp. Required.
	ResourceURL string
	// Issuer is the Keycloak realm issuer URL, advertised to clients as the
	// authorization server in the Protected Resource Metadata. Required.
	Issuer string
}

// server holds the dependencies the tool and resource handlers need.
//
// AUTHORIZATION MODEL. This surface does NOT authorize locally. Every
// handler proxies to McpService over the in-process bufconn, forwarding
// the caller's verified MCP bearer as gRPC metadata; McpService's chain
// (MCP-audience token verification → membership → in-handler read gate)
// is the authorization boundary. The bufconn dial is a performance
// preference, NOT a security boundary — McpService enforces the same
// authz on its public TCP listener. The bearer edge-verifier here only
// gates this HTTP transport + yields the raw token to forward.
type server struct {
	client mcpv1.McpServiceClient
}

// Tool inputs. The generic AddTool derives each tool's input schema
// from these structs; the rich usage guidance lives in the tool
// Description strings below.
type getAccountInput struct{}

type listOrgsInput struct {
	PageSize   int32  `json:"page_size,omitempty"`
	PageToken  string `json:"page_token,omitempty"`
	NamePrefix string `json:"name_prefix,omitempty"`
}

type listSpacesInput struct {
	Org        string `json:"org"`
	PageSize   int32  `json:"page_size,omitempty"`
	PageToken  string `json:"page_token,omitempty"`
	NamePrefix string `json:"name_prefix,omitempty"`
}

// NewHandler builds the MCP HTTP handler: the Streamable HTTP transport behind
// bearer auth, plus the public Protected Resource Metadata discovery document.
// Mount it at /mcp and /mcp/ on the parent mux.
func NewHandler(cfg Config) http.Handler {
	switch {
	case cfg.Client == nil:
		panic("mcp: Config.Client is required")
	case cfg.Verifier == nil:
		panic("mcp: Config.Verifier is required")
	case cfg.ResourceURL == "":
		panic("mcp: Config.ResourceURL is required")
	case cfg.Issuer == "":
		panic("mcp: Config.Issuer is required")
	}

	srv := &server{client: cfg.Client}

	mcpServer := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: serverName, Version: serverVersion},
		&mcpsdk.ServerOptions{CompletionHandler: srv.complete},
	)

	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "get_account",
		Description: "Return the authenticated Pivox account: subject, email, and display name.",
	}, srv.getAccount)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name: "list_orgs",
		Description: "List the organizations you are a member of. Each org has a `slug` " +
			"(use it as the `org` argument elsewhere) and a `display_name`. " +
			"Optional `name_prefix` filters by a case-insensitive slug prefix. " +
			"Pagination: if the response's `next_page_token` is non-empty, call again " +
			"with it as `page_token` to get the next page.",
	}, srv.listOrgs)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name: "list_spaces",
		Description: "List spaces within one organization. `org` (a slug from list_orgs) is " +
			"REQUIRED. Optional `name_prefix` filters by a case-insensitive display-name " +
			"prefix. Pagination: if the response's `next_page_token` is non-empty, call " +
			"again with it as `page_token` (keeping the same `org`) to get the next page.",
	}, srv.listSpaces)

	mcpServer.AddResource(&mcpsdk.Resource{
		Name:        "account",
		URI:         accountResourceURI,
		Description: "The authenticated Pivox account.",
		MIMEType:    "application/json",
	}, srv.readAccount)
	mcpServer.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		Name:        "organization",
		URITemplate: orgURITemplate,
		Description: "A Pivox organization you are a member of, addressed by slug.",
		MIMEType:    "application/json",
	}, srv.readOrg)
	mcpServer.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		Name:        "space",
		URITemplate: spaceURITemplate,
		Description: "A Pivox space, addressed by its organization slug and space slug.",
		MIMEType:    "application/json",
	}, srv.readSpace)

	streamable := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return mcpServer },
		&mcpsdk.StreamableHTTPOptions{
			// This is a REMOTE MCP server: the process listens on loopback and sits
			// behind the gateway/the tunnel, so every request legitimately arrives with
			// Host=pivox.example (non-loopback). The SDK's default DNS-rebinding
			// guard rejects exactly that shape — loopback listener + non-loopback
			// Host — with 403. That guard protects *local* MCP servers from a browser
			// being DNS-rebound onto localhost; it does not fit a remote,
			// bearer-authenticated server behind a reverse proxy, where it only
			// produces false 403s. CSRF is already mitigated by the required bearer
			// token (an attacker cannot obtain it cross-origin).
			DisableLocalhostProtection: true,
		})

	prm := &oauthex.ProtectedResourceMetadata{
		Resource:               cfg.ResourceURL,
		AuthorizationServers:   []string{cfg.Issuer},
		ScopesSupported:        mcpScopes,
		BearerMethodsSupported: []string{"header"},
	}
	bearer := auth.RequireBearerToken(NewTokenVerifier(cfg.Verifier), &auth.RequireBearerTokenOptions{
		ResourceMetadataURL: cfg.ResourceURL + "/.well-known/oauth-protected-resource",
		// NOTE: we intentionally do NOT set Scopes here. Advertising them in the
		// WWW-Authenticate challenge does not help — observed clients (Claude Code)
		// ignore both the challenge `scope` and the PRM `scopes_supported` and send
		// no `scope` at authorize. The reliable lever is the client's per-server
		// `oauth.scopes` config. Setting Scopes here would also ENFORCE them, which
		// risks a spurious 403 (e.g. offline_access may be absent from the access
		// token's scope claim). The audience check already gates the transport.
	})

	mux := http.NewServeMux()
	// Discovery is public; the transport requires a valid MCP-audience token.
	// The specific well-known pattern outranks the /mcp/ subtree in ServeMux.
	mux.Handle(wellKnownPRMPath, auth.ProtectedResourceMetadataHandler(prm))
	mux.Handle("/mcp", bearer(streamable))
	mux.Handle("/mcp/", bearer(streamable))
	return mux
}

// forward derives an outbound gRPC context carrying the caller's MCP
// bearer, pulled from the TokenInfo the edge verifier stashed. Errors
// (rather than a partial/unauth call) when the token is missing.
func forward(ctx context.Context) (context.Context, error) {
	info := auth.TokenInfoFromContext(ctx)
	if info == nil {
		return nil, errors.New("mcp: missing token info on request context")
	}
	token, ok := info.Extra[ExtraRawToken].(string)
	if !ok || token == "" {
		return nil, errors.New("mcp: missing bearer token on request context")
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token), nil
}

// --- Tools ---

func (s *server) getAccount(ctx context.Context, _ *mcpsdk.CallToolRequest, _ getAccountInput) (*mcpsdk.CallToolResult, any, error) {
	octx, err := forward(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.GetAccount(octx, &mcpv1.GetAccountRequest{})
	if err != nil {
		return nil, nil, err
	}
	return toolJSON(resp)
}

func (s *server) listOrgs(ctx context.Context, _ *mcpsdk.CallToolRequest, in listOrgsInput) (*mcpsdk.CallToolResult, any, error) {
	octx, err := forward(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.ListOrgs(octx, &mcpv1.ListOrgsRequest{
		PageSize:   in.PageSize,
		PageToken:  in.PageToken,
		NamePrefix: in.NamePrefix,
	})
	if err != nil {
		return nil, nil, err
	}
	return toolJSON(resp)
}

func (s *server) listSpaces(ctx context.Context, _ *mcpsdk.CallToolRequest, in listSpacesInput) (*mcpsdk.CallToolResult, any, error) {
	octx, err := forward(ctx)
	if err != nil {
		return nil, nil, err
	}
	resp, err := s.client.ListSpaces(octx, &mcpv1.ListSpacesRequest{
		Org:        in.Org,
		PageSize:   in.PageSize,
		PageToken:  in.PageToken,
		NamePrefix: in.NamePrefix,
	})
	if err != nil {
		return nil, nil, err
	}
	return toolJSON(resp)
}

// --- Resources ---

func (s *server) readAccount(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	octx, err := forward(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.GetAccount(octx, &mcpv1.GetAccountRequest{})
	if err != nil {
		return nil, err
	}
	return resourceJSON(accountResourceURI, resp)
}

func (s *server) readOrg(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	org, err := parseOrgURI(req.Params.URI)
	if err != nil {
		return nil, err
	}
	octx, err := forward(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.GetOrg(octx, &mcpv1.GetOrgRequest{Org: org})
	if err != nil {
		return nil, err
	}
	return resourceJSON(req.Params.URI, resp)
}

func (s *server) readSpace(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	org, space, err := parseSpaceURI(req.Params.URI)
	if err != nil {
		return nil, err
	}
	octx, err := forward(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.GetSpace(octx, &mcpv1.GetSpaceRequest{Org: org, Space: space})
	if err != nil {
		return nil, err
	}
	return resourceJSON(req.Params.URI, resp)
}

// --- Completion ---

// complete answers completion/complete for the org + space URI template
// variables. `{organization}` completes against the caller's orgs;
// `{space}` completes against the org already resolved in the request's
// progressive-completion context (context.arguments["organization"]).
func (s *server) complete(ctx context.Context, req *mcpsdk.CompleteRequest) (*mcpsdk.CompleteResult, error) {
	octx, err := forward(ctx)
	if err != nil {
		return nil, err
	}
	arg := req.Params.Argument
	switch arg.Name {
	case "organization":
		resp, err := s.client.ListOrgs(octx, &mcpv1.ListOrgsRequest{
			NamePrefix: arg.Value,
			PageSize:   completionLimit,
		})
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(resp.GetOrgs()))
		for _, o := range resp.GetOrgs() {
			values = append(values, o.GetSlug())
		}
		return completionResult(values), nil
	case "space":
		org := progressiveOrg(req)
		if org == "" {
			// No org resolved yet: nothing to complete a space against.
			return completionResult(nil), nil
		}
		resp, err := s.client.ListSpaces(octx, &mcpv1.ListSpacesRequest{
			Org:        org,
			NamePrefix: arg.Value,
			PageSize:   completionLimit,
		})
		if err != nil {
			return nil, err
		}
		values := make([]string, 0, len(resp.GetSpaces()))
		for _, sp := range resp.GetSpaces() {
			values = append(values, sp.GetSlug())
		}
		return completionResult(values), nil
	default:
		return completionResult(nil), nil
	}
}

// progressiveOrg pulls the already-resolved organization slug from the
// completion request's context, which the client fills in as the user
// completes the earlier `{organization}` template variable.
func progressiveOrg(req *mcpsdk.CompleteRequest) string {
	if req.Params.Context == nil {
		return ""
	}
	return req.Params.Context.Arguments["organization"]
}

func completionResult(values []string) *mcpsdk.CompleteResult {
	if len(values) > completionLimit {
		values = values[:completionLimit]
	}
	return &mcpsdk.CompleteResult{
		Completion: mcpsdk.CompletionResultDetails{Values: values},
	}
}

// --- Marshaling + URI parsing ---

// toolJSON renders a proto response as the MCP tool result (a single
// JSON text block).
func toolJSON(msg proto.Message) (*mcpsdk.CallToolResult, any, error) {
	body, err := marshalJSON(msg)
	if err != nil {
		return nil, nil, err
	}
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: body}},
	}, nil, nil
}

// resourceJSON renders a proto response as a resources/read result.
func resourceJSON(uri string, msg proto.Message) (*mcpsdk.ReadResourceResult, error) {
	body, err := marshalJSON(msg)
	if err != nil {
		return nil, err
	}
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{
			URI:      uri,
			MIMEType: "application/json",
			Text:     body,
		}},
	}, nil
}

func marshalJSON(msg proto.Message) (string, error) {
	b, err := protoJSON.Marshal(msg)
	if err != nil {
		return "", fmt.Errorf("mcp: marshal response: %w", err)
	}
	return string(b), nil
}

// parseOrgURI extracts the org slug from pivox://organizations/{org}.
func parseOrgURI(uri string) (string, error) {
	segs, err := uriSegments(uri)
	if err != nil {
		return "", err
	}
	if len(segs) != 2 || segs[0] != "organizations" || segs[1] == "" {
		return "", fmt.Errorf("mcp: not an organization resource URI: %q", uri)
	}
	return segs[1], nil
}

// parseSpaceURI extracts (org, space) from
// pivox://organizations/{org}/spaces/{space}.
func parseSpaceURI(uri string) (string, string, error) {
	segs, err := uriSegments(uri)
	if err != nil {
		return "", "", err
	}
	if len(segs) != 4 || segs[0] != "organizations" || segs[1] == "" || segs[2] != "spaces" || segs[3] == "" {
		return "", "", fmt.Errorf("mcp: not a space resource URI: %q", uri)
	}
	return segs[1], segs[3], nil
}

// uriSegments strips the pivox:// scheme and splits the remaining path.
func uriSegments(uri string) ([]string, error) {
	const scheme = "pivox://"
	if !strings.HasPrefix(uri, scheme) {
		return nil, fmt.Errorf("mcp: unexpected resource URI scheme: %q", uri)
	}
	return strings.Split(strings.TrimPrefix(uri, scheme), "/"), nil
}
