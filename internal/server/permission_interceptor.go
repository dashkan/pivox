package server

//go:generate go run ../../cmd/gen-permission-registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"

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

	// ScopeSpace means the RPC operates on a space. Resolution path:
	// org slug → org row → space slug → space row → permission check
	// against SpaceTarget, which inherits org-level role bindings.
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
//
// For ScopeOrg: only Slug is set (the org slug).
// For ScopeSpace: both OrgSlug (parent org) and Slug (space) are set —
// resolving a space requires looking up the parent org first because
// the schema scopes space slugs per-org (UNIQUE on (org_id, name)).
type ScopeRef struct {
	Kind    ScopeKind
	Slug    string
	OrgSlug string // populated for ScopeSpace; empty for ScopeOrg
}

// OrgScope is a convenience constructor for an org-scoped ScopeRef.
func OrgScope(slug string) ScopeRef {
	return ScopeRef{Kind: ScopeOrg, Slug: slug}
}

// SpaceScope is a convenience constructor for a space-scoped
// ScopeRef. Both org slug and space slug are required because spaces
// are scoped per-org (the same space slug can exist in two orgs).
func SpaceScope(orgSlug, spaceSlug string) ScopeRef {
	return ScopeRef{Kind: ScopeSpace, Slug: spaceSlug, OrgSlug: orgSlug}
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
// resolve an org scope. For space-scoped RPCs, the parent org row
// is also attached and is retrievable via this function.
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

// ResolvedSpace is what the interceptor attaches to the request
// context after a successful space-scope check. Like ResolvedOrg, it
// saves the handler from re-issuing the slug-resolution lookup.
//
// The space's parent org is resolved as part of the gate and attached
// separately via resolvedOrgKey, so handlers that need both can call
// MustResolvedOrgFromContext + MustResolvedSpaceFromContext without
// extra DB calls.
type ResolvedSpace struct {
	ID   uuid.UUID
	Slug string
	Row  db.Space
}

type resolvedSpaceKey struct{}

// ResolvedSpaceFromContext returns the space resolved by
// PermissionInterceptor for the current RPC, or (nil, false) if the
// method wasn't space-scoped or the interceptor didn't run.
func ResolvedSpaceFromContext(ctx context.Context) (*ResolvedSpace, bool) {
	v, ok := ctx.Value(resolvedSpaceKey{}).(*ResolvedSpace)
	return v, ok
}

// MustResolvedSpaceFromContext is the handler-side assertion variant
// for space-scoped RPCs. Mirrors MustResolvedOrgFromContext.
func MustResolvedSpaceFromContext(ctx context.Context) *ResolvedSpace {
	v, ok := ResolvedSpaceFromContext(ctx)
	if !ok {
		panic("server: space-scoped handler invoked without a resolved space on the context (missing or misconfigured permission interceptor)")
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
		return g.checkSpaceScope(ctx, fullMethod, entry, scope, callerID)
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
	// Use the gate-aware lookup so soft-deleted orgs are visible:
	// reads still work during the grace window, and Undelete needs
	// to find the row. The state check below enforces the
	// FAILED_PRECONDITION semantics for mutations on a
	// DELETE_REQUESTED org.
	org, err := g.queries.GetOrganizationByNameForGate(ctx, scope.Slug)
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
	if err := enforceSoftDeleteGate(org.State, entry.Permission, scope.Slug); err != nil {
		return nil, err
	}
	resolved := &ResolvedOrg{ID: org.ID, Slug: scope.Slug, Row: org}
	return context.WithValue(ctx, resolvedOrgKey{}, resolved), nil
}

// enforceSoftDeleteGate returns FAILED_PRECONDITION when the caller
// is mutating a soft-deleted org. Reads pass through (org metadata
// remains visible during the grace window) and `organizations.delete`
// passes through too — that permission gates UndeleteOrganization,
// which is the only mutating op valid against a DELETE_REQUESTED
// row. Other writes are blocked until the org is restored.
//
// This is the RPC-boundary "soft-delete gate" called for in the
// IAM/lifecycle roadmap: the gate lives in the interceptor so every
// gated handler inherits the protection without per-handler checks.
func enforceSoftDeleteGate(state db.ResourceState, perm, orgSlug string) error {
	if state != db.ResourceStateDELETEREQUESTED {
		return nil
	}
	if isReadPermission(perm) || perm == permission.OrganizationsDelete {
		return nil
	}
	return apierr.FailedPrecondition(
		fmt.Sprintf("organization %q is in DELETE_REQUESTED state; restore it via UndeleteOrganization or wait for purge before mutating", orgSlug))
}

// isReadPermission reports whether the permission ID is a read-only
// operation. Convention: every read perm in the catalog ends with
// `.read`. Workflow verbs (transferOwnership, fulfill, deliver, etc.)
// and CRUD writes use other suffixes.
func isReadPermission(perm string) bool {
	return strings.HasSuffix(perm, ".read")
}

// --- Test-only exports ---
//
// These helpers expose internals for tests in OTHER packages
// (e.g. service/organizations lifecycle tests that simulate a
// post-gate context). Same-package tests don't need them; they
// access checkOrgScope/enforceSoftDeleteGate/resolvedOrgKey
// directly. We can't put these in a _test.go file because Go's
// test compilation makes _test.go symbols invisible across
// packages. The cost is three exported functions in the
// production binary, all with `ForTest` suffixes — visible but
// uncallable in the wild without obvious intent.

// EnforceSoftDeleteGateForTest is the test-only export of the
// soft-delete gate logic.
func EnforceSoftDeleteGateForTest(state db.ResourceState, perm, orgSlug string) error {
	return enforceSoftDeleteGate(state, perm, orgSlug)
}

// WithResolvedOrgForTest injects a ResolvedOrg into ctx for
// service-level tests that bypass the interceptor.
func WithResolvedOrgForTest(ctx context.Context, org *ResolvedOrg) context.Context {
	return context.WithValue(ctx, resolvedOrgKey{}, org)
}

// WithResolvedSpaceForTest is the space-scoped analogue.
func WithResolvedSpaceForTest(ctx context.Context, space *ResolvedSpace) context.Context {
	return context.WithValue(ctx, resolvedSpaceKey{}, space)
}

// checkSpaceScope handles space-scope: resolve org slug → org row,
// then space slug → space row within that org, then check permission
// against SpaceTarget (which inherits org-level role bindings).
// Both rows are attached to ctx so handlers don't repeat either
// lookup.
func (g *permissionGate) checkSpaceScope(
	ctx context.Context,
	fullMethod string,
	entry RegistryEntry,
	scope ScopeRef,
	callerID uuid.UUID,
) (context.Context, error) {
	// Same soft-delete-aware lookup as the org-scope path so a
	// space-scoped RPC against a soft-deleted parent org surfaces
	// FAILED_PRECONDITION (mutating) or proceeds (reading), not
	// NotFound. The soft-delete gate fires after HasPermission for
	// symmetry with checkOrgScope — non-permitted callers get
	// PermissionDenied and don't learn the org's state.
	org, err := g.queries.GetOrganizationByNameForGate(ctx, scope.OrgSlug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("organization", scope.OrgSlug)
		}
		slog.ErrorContext(ctx, "permission interceptor: org lookup failed", "method", fullMethod, "slug", scope.OrgSlug, "error", err)
		return nil, apierr.Internal("lookup organization")
	}
	space, err := g.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: scope.Slug})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("space", scope.OrgSlug+"/"+scope.Slug)
		}
		slog.ErrorContext(ctx, "permission interceptor: space lookup failed", "method", fullMethod, "org", scope.OrgSlug, "space", scope.Slug, "error", err)
		return nil, apierr.Internal("lookup space")
	}
	allowed, err := g.resolver.HasPermission(ctx, callerID, permission.SpaceTarget(space.ID), entry.Permission)
	if err != nil {
		slog.ErrorContext(ctx, "permission interceptor: resolve failed", "method", fullMethod, "permission", entry.Permission, "error", err)
		return nil, apierr.Internal("resolve permission")
	}
	if !allowed {
		return nil, apierr.PermissionDenied(fmt.Sprintf("caller lacks %q on space %q/%q", entry.Permission, scope.OrgSlug, scope.Slug))
	}
	if err := enforceSoftDeleteGate(org.State, entry.Permission, scope.OrgSlug); err != nil {
		return nil, err
	}
	resolvedOrg := &ResolvedOrg{ID: org.ID, Slug: scope.OrgSlug, Row: org}
	resolvedSpace := &ResolvedSpace{ID: space.ID, Slug: scope.Slug, Row: space}
	ctx = context.WithValue(ctx, resolvedOrgKey{}, resolvedOrg)
	ctx = context.WithValue(ctx, resolvedSpaceKey{}, resolvedSpace)
	return ctx, nil
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

// PermissionStreamInterceptor is the streaming variant. Same
// registry, same exempt set, same gate semantics — but the
// extraction point is shifted to first-RecvMsg because gRPC's
// streaming-server interceptor signature doesn't carry the request
// body. The flow:
//
//  1. Exempt methods bypass everything (no wrap).
//  2. Methods absent from the registry default-deny BEFORE the
//     handler runs (Internal — server misconfiguration).
//  3. Caller identity is resolved up-front so unauth callers fail
//     fast without waiting for a message that may never arrive.
//  4. The stream is wrapped with a `permissionStream` that runs
//     the registered extractor on the first message read by the
//     handler. If the gate denies, the deny error surfaces from
//     RecvMsg and the handler propagates it to the client.
//
// Subsequent messages on the same stream are NOT re-gated — the
// scope is fixed for the lifetime of the stream (matches the
// AI-chat conversation-per-stream usage and avoids re-resolving
// on every chunk).
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
		if gate.exempt[info.FullMethod] {
			return handler(srv, ss)
		}
		entry, ok := gate.registry[info.FullMethod]
		if !ok {
			slog.ErrorContext(ss.Context(), "permission interceptor: streaming method has no gate registered", "method", info.FullMethod)
			return apierr.Internal("permission gate not configured for this method")
		}
		// Pre-resolve caller identity so unauth callers fail fast
		// without waiting for the first message.
		callerID, err := gate.identity(ss.Context())
		if err != nil {
			return err
		}
		wrapped := &permissionStream{
			ServerStream: ss,
			gate:         gate,
			method:       info.FullMethod,
			entry:        entry,
			callerID:     callerID,
			ctx:          ss.Context(),
		}
		return handler(srv, wrapped)
	}
}

// permissionStream wraps grpc.ServerStream so the gate fires on
// the first RecvMsg call. Subsequent messages skip the gate check.
// Context() returns the gate-augmented context after the first
// successful resolution so the handler observes the resolved
// scope via {Resolved Org,Space} FromContext.
//
// Concurrency: this wrapper is safe for server-streaming RPCs
// only. gRPC-Go serializes RecvMsg per stream (the transport
// pulls one message at a time from the wire), so `checked` and
// `ctx` are mutated from a single goroutine in the server-stream
// case. A future bidi or client-streaming RPC that fans RecvMsg
// out across goroutines would need a sync.Mutex around the
// checked/ctx fields — flag this when wrapping such a method.
type permissionStream struct {
	grpc.ServerStream
	gate     *permissionGate
	method   string
	entry    RegistryEntry
	callerID uuid.UUID
	ctx      context.Context
	// checked is set to true on the first call to RecvMsg
	// regardless of gate outcome (allow, deny, extractor error,
	// resolver fault). A handler that swallows a deny error and
	// retries RecvMsg will not re-run the gate; the next
	// underlying RecvMsg either returns io.EOF or the next message
	// (which is fine — the original deny already surfaced).
	checked bool
}

func (s *permissionStream) Context() context.Context { return s.ctx }

func (s *permissionStream) RecvMsg(m any) error {
	if err := s.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	if s.checked {
		return nil
	}
	s.checked = true
	scope, err := s.entry.Extract(m)
	if err != nil {
		return err
	}
	var newCtx context.Context
	switch scope.Kind {
	case ScopeOrg:
		newCtx, err = s.gate.checkOrgScope(s.ctx, s.method, s.entry, scope, s.callerID)
	case ScopeSpace:
		newCtx, err = s.gate.checkSpaceScope(s.ctx, s.method, s.entry, scope, s.callerID)
	default:
		slog.ErrorContext(s.ctx, "permission interceptor: unknown scope kind on stream", "method", s.method, "kind", scope.Kind)
		return apierr.Internal("permission gate misconfigured for this method")
	}
	if err != nil {
		return err
	}
	s.ctx = newCtx
	return nil
}
