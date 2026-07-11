package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/auth"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"

	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
)

const (
	serverName    = "pivox"
	serverVersion = "0.1.0"

	// activeOrgResourceURI identifies the active-organization resource.
	activeOrgResourceURI = "pivox://active-organization"

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

// Config configures the MCP HTTP handler.
type Config struct {
	// Queries resolves the caller's active organization. Required.
	Queries db.Querier
	// Verifier validates MCP bearer tokens. It MUST be an OIDC verifier whose
	// audience is ResourceURL — the anti-confusion boundary that stops a token
	// minted for the main Pivox API from being replayed here. Required.
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
// AUTHORIZATION MODEL. This MCP surface does NOT run Pivox's gRPC
// Membership/Permission interceptor chain. Authorization currently rests on the
// audience-pinned token plus Keycloak having bound the caller's `organization`
// claim at mint time — which proves membership *at mint*, not current Pivox-side
// permission. The only tool/resource today returns the caller's OWN org identity
// (name/slug/id/caller_id), so that's acceptable. The moment a tool reads or
// writes real org data, it MUST re-check Pivox permissions server-side (e.g. via
// permission.Resolver, or by proxying to the gRPC server over bufconn so the
// interceptors run) rather than trusting the token's org binding.
type server struct {
	queries db.Querier
}

// ActiveOrganization is the structured result of both the get_active_organization
// tool and the active-organization resource: the Pivox org the session is scoped
// to, plus the caller's identity.
type ActiveOrganization struct {
	ResourceName string `json:"resource_name"` // AIP name: organizations/{slug}
	Slug         string `json:"slug"`
	DisplayName  string `json:"display_name"`
	ID           string `json:"id"`
	CallerID     string `json:"caller_id"`
}

// getActiveOrgInput is the (argument-free) input for get_active_organization.
type getActiveOrgInput struct{}

// NewHandler builds the MCP HTTP handler: the Streamable HTTP transport behind
// bearer auth, plus the public Protected Resource Metadata discovery document.
// Mount it at /mcp and /mcp/ on the parent mux.
func NewHandler(cfg Config) http.Handler {
	switch {
	case cfg.Queries == nil:
		panic("mcp: Config.Queries is required")
	case cfg.Verifier == nil:
		panic("mcp: Config.Verifier is required")
	case cfg.ResourceURL == "":
		panic("mcp: Config.ResourceURL is required")
	case cfg.Issuer == "":
		panic("mcp: Config.Issuer is required")
	}

	srv := &server{queries: cfg.Queries}

	mcpServer := mcpsdk.NewServer(&mcpsdk.Implementation{Name: serverName, Version: serverVersion}, nil)
	mcpsdk.AddTool(mcpServer, &mcpsdk.Tool{
		Name:        "get_active_organization",
		Description: "Return the Pivox organization the current session is scoped to, along with the caller's identity.",
	}, srv.getActiveOrganization)
	mcpServer.AddResource(&mcpsdk.Resource{
		Name:        "active-organization",
		URI:         activeOrgResourceURI,
		Description: "The Pivox organization the current session is scoped to.",
		MIMEType:    "application/json",
	}, srv.readActiveOrganization)

	streamable := mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return mcpServer },
		&mcpsdk.StreamableHTTPOptions{
			// This is a REMOTE MCP server: the process listens on loopback and sits
			// behind envoy/the tunnel, so every request legitimately arrives with
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

// getActiveOrganization is the get_active_organization tool handler.
func (s *server) getActiveOrganization(ctx context.Context, _ *mcpsdk.CallToolRequest, _ getActiveOrgInput) (*mcpsdk.CallToolResult, ActiveOrganization, error) {
	view, err := s.activeOrg(ctx)
	if err != nil {
		return nil, ActiveOrganization{}, err
	}
	// Non-nil Out with nil result: the SDK populates structuredContent from it.
	return nil, view, nil
}

// readActiveOrganization is the active-organization resource handler.
func (s *server) readActiveOrganization(ctx context.Context, _ *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
	view, err := s.activeOrg(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(view)
	if err != nil {
		return nil, fmt.Errorf("mcp: marshal active organization: %w", err)
	}
	return &mcpsdk.ReadResourceResult{
		Contents: []*mcpsdk.ResourceContents{{
			URI:      activeOrgResourceURI,
			MIMEType: "application/json",
			Text:     string(body),
		}},
	}, nil
}

// activeOrg resolves the caller's active organization from the request's token
// info (set by RequireBearerToken) and shapes it for output.
func (s *server) activeOrg(ctx context.Context) (ActiveOrganization, error) {
	info := auth.TokenInfoFromContext(ctx)
	org, err := resolveActiveOrganization(ctx, s.queries, info)
	if err != nil {
		return ActiveOrganization{}, err
	}
	return ActiveOrganization{
		ResourceName: "organizations/" + org.Name,
		Slug:         org.Name,
		DisplayName:  org.DisplayName,
		ID:           org.ID.String(),
		CallerID:     info.UserID,
	}, nil
}
