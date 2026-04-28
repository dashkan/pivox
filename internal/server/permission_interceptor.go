package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
)

// ScopeKind discriminates between org-scoped and space-scoped RPCs.
// The interceptor uses this to decide which slug-resolution path to
// take and which permission.Target to build.
type ScopeKind int

const (
	// ScopeOrg means the RPC operates on an organization. The Slug
	// field of ScopeRef is the org's URL slug.
	ScopeOrg ScopeKind = iota + 1

	// ScopeSpace means the RPC operates on a space. Resolution path
	// (slug → space row → parent org → permission check with space
	// inheritance) lands in a follow-up commit.
	ScopeSpace
)

// String implements fmt.Stringer for slog-friendly diagnostics.
func (k ScopeKind) String() string {
	switch k {
	case ScopeOrg:
		return "org"
	case ScopeSpace:
		return "space"
	default:
		return fmt.Sprintf("unknown(%d)", int(k))
	}
}

// ScopeRef is the unresolved scope an RPC operates on, as pulled from
// the request body by a ScopeExtractor. The interceptor resolves
// slug → uuid via the database before calling the permission resolver.
type ScopeRef struct {
	Kind ScopeKind
	Slug string
}

// OrgScope is a convenience constructor for an org-scoped ScopeRef.
func OrgScope(slug string) ScopeRef {
	return ScopeRef{Kind: ScopeOrg, Slug: slug}
}

// ScopeExtractor pulls the ScopeRef from a request. Each gated RPC
// supplies one. Returning a non-nil error short-circuits the
// interceptor — the error must already be a gRPC status (typically
// InvalidArgument) since it surfaces directly to the caller.
type ScopeExtractor func(req any) (ScopeRef, error)

// RegistryEntry binds a permission and a scope-extraction strategy to
// a single gRPC method.
type RegistryEntry struct {
	Permission string
	Extract    ScopeExtractor
}

// Registry maps full gRPC method names (e.g.
// "/pivox.api.v1.Organizations/UpdateOrganization") to their gating
// entry. Methods absent from both Registry and the exempt set are
// treated as a server misconfiguration (Internal) — default-deny by
// failure, with a loud signal to operators rather than a silent
// passthrough or a misleading PermissionDenied.
type Registry map[string]RegistryEntry

// ResolvedOrg is what the interceptor attaches to the request context
// after a successful org-scope check. Handlers that need the org row
// should read it from the context via ResolvedOrgFromContext rather
// than re-issuing GetOrganizationByName — the interceptor has already
// paid for the lookup.
//
// The resolved row is point-in-time; the interceptor relies on org
// slugs being immutable post-create. If slug renames ever ship, this
// type must surface a re-read affordance.
type ResolvedOrg struct {
	ID   uuid.UUID
	Slug string
	Row  db.Organization
}

type resolvedOrgKey struct{}

// ResolvedOrgFromContext returns the org resolved by
// PermissionInterceptor for the current RPC, or (nil, false) if the
// interceptor didn't run (e.g. the method was exempt) or didn't
// resolve an org scope.
func ResolvedOrgFromContext(ctx context.Context) (*ResolvedOrg, bool) {
	v, ok := ctx.Value(resolvedOrgKey{}).(*ResolvedOrg)
	return v, ok
}

// MustResolvedOrgFromContext is the handler-side assertion variant.
// Org-scoped handlers can call this knowing the interceptor must have
// resolved the org before they ran; a missing value indicates the
// handler was reached by a misconfigured chain (no permission
// interceptor, wrong scope kind in registry) and should never happen
// at runtime. Mirrors MustAuthenticatedUID.
func MustResolvedOrgFromContext(ctx context.Context) *ResolvedOrg {
	v, ok := ResolvedOrgFromContext(ctx)
	if !ok {
		panic("server: org-scoped handler invoked without a resolved org on the context (missing or misconfigured permission interceptor)")
	}
	return v
}

// permissionGate holds the immutable, post-construction view used by
// both unary and stream interceptors. Constructing the gate once and
// sharing it ensures the same registry/exempt set governs both
// dispatch paths and protects against post-init mutation of the input
// maps.
type permissionGate struct {
	registry Registry
	exempt   map[string]bool
	queries  db.Querier
	resolver *permission.Resolver
	identity CallerIdentityResolver
}

func newPermissionGate(
	registry Registry,
	exempt map[string]bool,
	queries db.Querier,
	resolver *permission.Resolver,
	identity CallerIdentityResolver,
) *permissionGate {
	for method := range exempt {
		if _, dup := registry[method]; dup {
			panic(fmt.Sprintf("permission interceptor: method %q is in both registry and exempt set", method))
		}
	}
	regCopy := make(Registry, len(registry))
	maps.Copy(regCopy, registry)
	exemptCopy := make(map[string]bool, len(exempt))
	maps.Copy(exemptCopy, exempt)
	return &permissionGate{
		registry: regCopy,
		exempt:   exemptCopy,
		queries:  queries,
		resolver: resolver,
		identity: identity,
	}
}

// check runs the gate logic and returns either (newCtx, nil) for
// allow or (nil, err) for deny / config bug. Used by both unary and
// stream interceptors.
func (g *permissionGate) check(ctx context.Context, fullMethod string, req any) (context.Context, error) {
	if g.exempt[fullMethod] {
		return ctx, nil
	}
	entry, ok := g.registry[fullMethod]
	if !ok {
		// Default-deny by failure. This is a server-side configuration
		// bug, not an authorization decision about the caller. Surface
		// as Internal so it's distinguishable from real auth denials
		// in logs and metrics.
		slog.ErrorContext(ctx, "permission interceptor: method has no gate registered", "method", fullMethod)
		return nil, apierr.Internal("permission gate not configured for this method")
	}
	callerID, err := g.identity(ctx)
	if err != nil {
		return nil, err
	}
	scope, err := entry.Extract(req)
	if err != nil {
		return nil, err
	}
	switch scope.Kind {
	case ScopeOrg:
		return g.checkOrgScope(ctx, fullMethod, entry, scope, callerID)
	case ScopeSpace:
		return nil, status.Error(codes.Unimplemented, "space-scoped permission gating not yet wired")
	default:
		slog.ErrorContext(ctx, "permission interceptor: unknown scope kind", "method", fullMethod, "kind", scope.Kind)
		return nil, apierr.Internal("permission gate misconfigured for this method")
	}
}

func (g *permissionGate) checkOrgScope(
	ctx context.Context,
	fullMethod string,
	entry RegistryEntry,
	scope ScopeRef,
	callerID uuid.UUID,
) (context.Context, error) {
	org, err := g.queries.GetOrganizationByName(ctx, scope.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("organization", scope.Slug)
		}
		slog.ErrorContext(ctx, "permission interceptor: org lookup failed", "method", fullMethod, "slug", scope.Slug, "error", err)
		return nil, apierr.Internal("lookup organization")
	}
	allowed, err := g.resolver.HasPermission(ctx, callerID, permission.OrgTarget(org.ID), entry.Permission)
	if err != nil {
		slog.ErrorContext(ctx, "permission interceptor: resolve failed", "method", fullMethod, "permission", entry.Permission, "error", err)
		return nil, apierr.Internal("resolve permission")
	}
	if !allowed {
		return nil, apierr.PermissionDenied(fmt.Sprintf("caller lacks %q on organization %q", entry.Permission, scope.Slug))
	}
	resolved := &ResolvedOrg{ID: org.ID, Slug: scope.Slug, Row: org}
	return context.WithValue(ctx, resolvedOrgKey{}, resolved), nil
}

// PermissionInterceptor returns a gRPC unary server interceptor that
// gates every RPC against the permission registry.
//
// Behavior, in order:
//
//  1. Methods in the exempt set bypass the check entirely.
//  2. Methods absent from the registry return Internal (server
//     misconfiguration: forgetting to register a new RPC fails closed
//     and surfaces loudly to operators).
//  3. The caller's firebase_identity is resolved via the supplied
//     CallerIdentityResolver. Identity errors propagate verbatim.
//  4. The registered ScopeExtractor pulls a ScopeRef from the
//     request. Extractor errors propagate.
//  5. The slug is resolved to a row. Missing rows surface as
//     NotFound; other DB errors as Internal.
//  6. permission.Resolver.HasPermission decides allow/deny.
//  7. On allow, the resolved row is attached to the context and the
//     handler is invoked. Handlers should use ResolvedOrgFromContext
//     instead of repeating the slug lookup.
//
// Must run AFTER AuthInterceptor (sets the UID) and AFTER
// MembershipRequiredInterceptor (cheap deny for memberless callers).
//
// Panics at construction time if Registry and the exempt set name the
// same method — that's a programming error, not a runtime condition.
func PermissionInterceptor(
	registry Registry,
	exempt map[string]bool,
	queries db.Querier,
	resolver *permission.Resolver,
	identity CallerIdentityResolver,
) grpc.UnaryServerInterceptor {
	gate := newPermissionGate(registry, exempt, queries, resolver, identity)
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		newCtx, err := gate.check(ctx, info.FullMethod, req)
		if err != nil {
			return nil, err
		}
		return handler(newCtx, req)
	}
}

// PermissionStreamInterceptor is the streaming variant. Same registry,
// same exempt set, same gate semantics. Streaming RPCs that pass the
// gate get a wrapped ServerStream whose Context() carries the
// resolved-scope value, so streaming handlers read scope the same way
// unary handlers do.
//
// Note: unlike unary, the gate runs on the initial request metadata
// (no request body to extract from yet for client-streaming RPCs).
// Today's streaming surface is server-side-only (AI chat) and is
// deferred from gating; the interceptor still default-denies any
// streaming method not registered or exempted, which prevents a
// future client-streaming RPC from reaching its handler before its
// gating is wired.
func PermissionStreamInterceptor(
	registry Registry,
	exempt map[string]bool,
	queries db.Querier,
	resolver *permission.Resolver,
	identity CallerIdentityResolver,
) grpc.StreamServerInterceptor {
	gate := newPermissionGate(registry, exempt, queries, resolver, identity)
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		// Streaming has no request body at gate time; passing nil here
		// means streaming extractors must be no-arg / parent-only.
		// Until streaming RPCs are explicitly registered, every
		// streaming method falls into the default-deny path.
		newCtx, err := gate.check(ss.Context(), info.FullMethod, nil)
		if err != nil {
			return err
		}
		return handler(srv, &serverStreamWithContext{ServerStream: ss, ctx: newCtx})
	}
}
