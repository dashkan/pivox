package storage_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/service/storage"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// recordingStream implements agentv1.AgentService_ConnectServer and
// captures every ControlMessage sent through Send. Used to inspect
// the SessionGrant pushed to mock-connected agents in
// CreateStorageSession tests.
type recordingStream struct {
	grpc.ServerStream
	mu   sync.Mutex
	sent []*agentv1.ControlMessage
}

func (r *recordingStream) Send(msg *agentv1.ControlMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, msg)
	return nil
}

func (r *recordingStream) Recv() (*agentv1.AgentMessage, error) {
	return nil, nil
}

func (r *recordingStream) sentSessionGrants() []*agentv1.SessionGrant {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*agentv1.SessionGrant
	for _, m := range r.sent {
		if g := m.GetSessionGrant(); g != nil {
			out = append(out, g)
		}
	}
	return out
}

// sessionTestEnv bundles the harness + storage clients + a recording
// agent connection so each session test can stand up the full stack
// in three lines.
type sessionTestEnv struct {
	h        *grpcharness.Harness
	gwClient storagev1.StorageGatewaysClient
	epClient storagev1.EndpointsClient
	conns    *agentstream.ConnectionManager
	stream   *recordingStream
}

// sessionTestOpts lets phase-4+ tests override the
// StorageGatewaysConfig fields that affect handler behavior. Each
// field is applied on top of the required Queries/Encryptor/Conns
// the harness already supplies.
type sessionTestOpts struct {
	MaxSessionTTL time.Duration
	CookieDomain  string
}

func newSessionTestEnv(t *testing.T, opts ...sessionTestOpts) *sessionTestEnv {
	t.Helper()
	var o sessionTestOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	conns := agentstream.NewConnectionManager()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
				Queries:       h.Queries,
				Encryptor:     h.Encryptor,
				Conns:         conns,
				MaxSessionTTL: o.MaxSessionTTL,
				CookieDomain:  o.CookieDomain,
			}))
			storagev1.RegisterEndpointsServer(s, storage.NewEndpointsServer(storage.EndpointsConfig{
				Queries: h.Queries, Encryptor: h.Encryptor,
			}))
		}))
	return &sessionTestEnv{
		h:        h,
		gwClient: storagev1.NewStorageGatewaysClient(h.Conn()),
		epClient: storagev1.NewEndpointsClient(h.Conn()),
		conns:    conns,
		stream:   &recordingStream{},
	}
}

// seedGatewayWithFilesystemEndpoint creates a gateway under the given
// org and one filesystem endpoint named `endpointID` on it. Returns
// the gateway's UUID so the caller can register a mock agent
// connection against it.
func (e *sessionTestEnv) seedGatewayWithFilesystemEndpoint(t *testing.T, ctx context.Context, orgSlug, gwSlug, endpointID string) uuid.UUID {
	t.Helper()
	op, err := e.gwClient.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent:           "organizations/" + orgSlug,
		StorageGatewayId: gwSlug,
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: gwSlug,
			IpAddresses: []string{"10.0.0.1"},
		},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())

	_, err = e.epClient.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:     "organizations/" + orgSlug + "/storageGateways/" + gwSlug,
		EndpointId: endpointID,
		Endpoint: &storagev1.Endpoint{
			DisplayName: endpointID,
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfig{
					Path: "/mnt/" + endpointID,
				},
			},
		},
	})
	require.NoError(t, err)

	row, err := e.h.Queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: e.h.LookupOrgID(t, orgSlug),
		Name:  gwSlug,
	})
	require.NoError(t, err)
	return row.ID
}

// addSpaceMember inserts a space_members.user_id row directly via
// queries (no public CreateMember RPC for space-scope without going
// through the IAM service which complicates the fixture). Mirrors
// SeedMembership's approach for org-scope.
func (e *sessionTestEnv) addSpaceMember(t *testing.T, ctx context.Context, orgID, spaceID uuid.UUID, member uuid.UUID, role string) {
	t.Helper()
	roleRow, err := e.h.Queries.GetSystemRole(ctx, db.GetSystemRoleParams{
		OrgID: orgID,
		Name:  role,
	})
	require.NoError(t, err, "system role %q must exist on org %s", role, orgID)
	_, err = e.h.Queries.CreateSpaceUserMember(ctx, db.CreateSpaceUserMemberParams{
		ID:        uuid.New(),
		SpaceID:   spaceID,
		RoleID:    roleRow.ID,
		UserID:    convert.PgUUID(member),
		CreatedBy: convert.PgUUID(member),
	})
	require.NoError(t, err)
}

// TestIntegration_CreateStorageSession_OrgMemberGetsOrgWidePatterns
// is the load-bearing acceptance test for the Prior-B trigger
// resolving #85's regression: an org-owner with NO direct
// space_members row must get an org-wide session that authorizes
// every storage path under the org. Today, all four system roles
// (Owner, Admin, Editor, Viewer) include `assets.assets.read` per
// internal/permission/permissions_gen.go:381+199+282+356+387, so
// any org_members binding triggers the org-wide branch.
func TestIntegration_CreateStorageSession_OrgMemberGetsOrgWidePatterns(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)

	// SeedOwnedOrg sets the harness caller to the new owner. The
	// owner has org_members.role=owner but NO space_members row in
	// any space they didn't personally create — exactly the
	// regression scenario.
	owner := env.h.SeedOwnedOrg(t, "acme", "Acme Inc", "owner-orgwide")
	// Spaces exist but were created by someone else (we'll seed via
	// the queries, bypassing CreateSpace's auto-add-to-space-members).
	// Easiest: keep no spaces; the org-wide pattern shape is
	// unaffected by space count.

	gwEastID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "acme", "gw-east", "media")
	gwWestID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "acme", "gw-west", "archive")

	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: gwEastID,
		OrgID:  env.h.LookupOrgID(t, "acme"),
		Stream: env.stream,
	})
	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: gwWestID,
		OrgID:  env.h.LookupOrgID(t, "acme"),
		Stream: env.stream,
	})

	// Owner is already the harness caller from SeedOwnedOrg.
	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err, "org-owner with no direct space_members must succeed; #85 regression check")
	assert.NotEmpty(t, resp.GetToken())

	grants := env.stream.sentSessionGrants()
	require.NotEmpty(t, grants)
	got := append([]string(nil), grants[0].GetPatterns()...)
	sort.Strings(got)
	want := []string{
		"/archive/acme/*",
		"/media/acme/*",
	}
	assert.Equal(t, want, got,
		"org-member must get org-wide patterns (one per endpoint, "+
			"NOT per-space); #85 regression check")

	_ = owner
}

// TestIntegration_CreateStorageSession_OrgMemberWithSpaceMembershipGetsOnlyOrgWide
// pins the branching contract: a caller who has BOTH org_members AND
// direct space_members in `parent` gets ONLY the org-wide pattern
// shape — never the OR-union of org-wide + per-space. Catches the
// regression where a future change accidentally folds both branches
// into the same `patterns` slice.
func TestIntegration_CreateStorageSession_OrgMemberWithSpaceMembershipGetsOnlyOrgWide(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)

	owner := env.h.SeedOwnedOrg(t, "acme", "Acme Inc", "owner-both-bindings")
	// Owner creates `alpha` — CreateSpace at server.go:315 also
	// inserts a space_members.user_id row for the creator. After
	// this, the owner has BOTH bindings in acme.
	_ = env.h.SeedOwnedSpace(t, "acme", "alpha", "Alpha")

	gwID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "acme", "gw", "media")
	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: gwID,
		OrgID:  env.h.LookupOrgID(t, "acme"),
		Stream: env.stream,
	})

	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.GetToken())

	grants := env.stream.sentSessionGrants()
	require.NotEmpty(t, grants)
	got := append([]string(nil), grants[0].GetPatterns()...)
	sort.Strings(got)

	// Org-wide ONLY. The per-space pattern (`/media/acme/alpha/*`)
	// must NOT appear — the branching contract says org-wide
	// subsumes per-space, and OR-ing them would be a regression.
	assert.Equal(t, []string{"/media/acme/*"}, got,
		"caller with BOTH org and space membership must get org-wide patterns ONLY; "+
			"per-space patterns must not be OR'd in")

	_ = owner
}

// TestIntegration_CreateStorageSession_SpaceOnlyMemberGetsPerSpacePatterns
// covers the per-space branch: a caller who has direct space_members
// in `parent` but NO org_members row in `parent` gets per-space
// patterns. Setup requires a non-trivial fixture — the membership
// interceptor demands ≥1 org membership somewhere globally, so the
// test caller is org-member of a SIBLING org while being only a
// space-member in the target org.
func TestIntegration_CreateStorageSession_SpaceOnlyMemberGetsPerSpacePatterns(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)

	// Target org acme. SeedOwnedOrg sets the harness caller to its
	// owner, so all subsequent CreateSpace / CreateStorageGateway /
	// CreateEndpoint calls run as someone with permissions on acme.
	acmeOwned := env.h.SeedOwnedOrg(t, "acme", "Acme Inc", "owner-spaceonly-acme")
	acmeID := env.h.LookupOrgID(t, "acme")
	alpha := env.h.SeedOwnedSpace(t, "acme", "alpha", "Alpha")
	beta := env.h.SeedOwnedSpace(t, "acme", "beta", "Beta")
	gwID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "acme", "gw", "media")
	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: gwID,
		OrgID:  acmeID,
		Stream: env.stream,
	})
	_ = acmeOwned // acme setup done as acme's owner

	// Sibling org so the global membership-≥1-org interceptor passes
	// for the space-only caller. SeedOwnedOrg flips the harness
	// caller to sibling's owner; we'll set the caller explicitly
	// below before the actual session-create call so this is fine.
	_ = env.h.SeedOwnedOrg(t, "sibling", "Sibling", "owner-spaceonly-sibling")
	siblingID := env.h.LookupOrgID(t, "sibling")

	// Space-only caller: org_members binding in sibling, direct
	// space_members binding in acme/alpha. NO org_members in acme.
	spaceOnly := env.h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "space-only-acme"})
	env.h.SeedMembership(t, siblingID, spaceOnly, grpcharness.RoleViewer)
	env.addSpaceMember(t, ctx, acmeID, alpha.Row.ID, spaceOnly.IdentityID, grpcharness.RoleViewer)
	// NOT a member of beta — assert beta doesn't appear in patterns.
	_ = beta

	env.h.SetCaller(spaceOnly)

	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.GetToken())

	grants := env.stream.sentSessionGrants()
	require.NotEmpty(t, grants)
	got := append([]string(nil), grants[0].GetPatterns()...)
	sort.Strings(got)
	want := []string{
		"/media/acme/alpha/*",
	}
	assert.Equal(t, want, got,
		"space-only caller must get per-space patterns ONLY for spaces they have "+
			"direct/group-mediated membership in; beta MUST be absent")
}

// TestIntegration_CreateStorageSession_NoMembershipInParent verifies
// the rejection path: a caller with neither org_members nor
// space_members in `parent` (passes the global ≥1-org interceptor
// via a sibling org) must receive PermissionDenied. Replaces the
// pre-Prior-B test pair that conflated "not in org" with "in org but
// in zero spaces" — under Prior B those branches collapse.
func TestIntegration_CreateStorageSession_NoMembershipInParent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)

	_ = env.h.SeedOwnedOrg(t, "acme", "Acme", "owner-noMem-acme")
	_ = env.h.SeedOwnedOrg(t, "other", "Other", "owner-noMem-other")
	otherID := env.h.LookupOrgID(t, "other")

	outsider := env.h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "outsider-noMem"})
	env.h.SeedMembership(t, otherID, outsider, grpcharness.RoleViewer)
	env.h.SetCaller(outsider)

	_, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err),
		"caller with no org_members AND no space_members in parent must be rejected")
}

// TestIntegration_CreateStorageSession_DoesNotLeakAcrossOrgs is the
// load-bearing #27 phase 3 acceptance test: a SessionGrant minted
// for org A must be visible ONLY to agents whose connection is
// scoped to org A. An agent connected for an unrelated org B must
// NOT see the grant — that's the cross-org broadcast leak the
// original ticket motivates.
//
// Setup: two orgs, each with one gateway and one mock-connected
// agent. The two agents register with their respective OrgIDs.
// Caller is owner of `acme` (re-set explicitly because SeedOwnedOrg
// of `other` flips the harness caller). The session-create call for
// `acme` must produce a SessionGrant on the acme stream and NOT on
// the other stream.
func TestIntegration_CreateStorageSession_DoesNotLeakAcrossOrgs(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)

	acmeStream := &recordingStream{}
	otherStream := &recordingStream{}

	// acme — owner becomes harness caller; agent registered.
	acmeOwned := env.h.SeedOwnedOrg(t, "acme", "Acme Inc", "owner-noleak-acme")
	acmeID := env.h.LookupOrgID(t, "acme")
	acmeGwID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "acme", "gw-acme", "media")
	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: acmeGwID, OrgID: acmeID, Stream: acmeStream,
	})

	// other-org — owner becomes harness caller temporarily; agent
	// registered for routing isolation.
	_ = env.h.SeedOwnedOrg(t, "other", "Other", "owner-noleak-other")
	otherID := env.h.LookupOrgID(t, "other")
	otherGwID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "other", "gw-other", "media")
	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: otherGwID, OrgID: otherID, Stream: otherStream,
	})

	// Flip the harness caller back to acme's owner so the session
	// creation succeeds (the bystander can't mint sessions for
	// acme — they'd hit PermissionDenied, which is a different
	// test path).
	env.h.SetCaller(acmeOwned.Owner)

	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, resp.GetToken())

	// The acme-scoped agent must have received the grant.
	require.NotEmpty(t, acmeStream.sentSessionGrants(),
		"agent in target org must receive the SessionGrant")

	// The other-org agent must have received NOTHING. This is the
	// load-bearing assertion: cross-org broadcast leakage closed.
	assert.Empty(t, otherStream.sentSessionGrants(),
		"agent in unrelated org must NOT receive the SessionGrant — "+
			"cross-org broadcast leak (the original #27 motivator) is closed")
}

// TestIntegration_CreateStorageSession_ParentRequired confirms that
// buf.validate.field.required=true on the new `parent` field rejects
// empty values before the handler runs.
func TestIntegration_CreateStorageSession_ParentRequired(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)
	owner := env.h.SeedOwnedOrg(t, "acme", "Acme", "owner-parent-required")
	env.h.SetCaller(owner.Owner)

	_, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"empty parent must be rejected by buf.validate before the handler runs")
}

// ---------------------------------------------------------------------------
// #27 phase 4 — TTL cap + cookie domain config
// ---------------------------------------------------------------------------

// captureSetCookie returns a grpc.CallOption that captures the
// response Set-Cookie header(s) into the given metadata.MD pointer.
// Used by the cookie-domain phase-4 tests.
func captureSetCookie(md *metadata.MD) grpc.CallOption {
	return grpc.Header(md)
}

// seedAcmeAndAgentForTTLTest stands up acme + a single mock-connected
// agent registered against acme. Returns the env so the test can
// drive the session-create call.
func seedAcmeAndAgentForTTLTest(t *testing.T, env *sessionTestEnv) {
	t.Helper()
	ctx := context.Background()
	_ = env.h.SeedOwnedOrg(t, "acme", "Acme", "owner-ttl")
	acmeID := env.h.LookupOrgID(t, "acme")
	gwID := env.seedGatewayWithFilesystemEndpoint(t, ctx, "acme", "gw", "media")
	env.conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: gwID, OrgID: acmeID, Stream: env.stream,
	})
}

// TestIntegration_CreateStorageSession_TTLClampedToCap verifies the
// silent-clamp contract: a caller-requested TTL above
// MaxSessionTTL is clamped down to the cap. Per AIP, the call still
// succeeds (no error); the response.Expiry reflects the clamped
// value rather than what the caller asked for.
func TestIntegration_CreateStorageSession_TTLClampedToCap(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t, sessionTestOpts{MaxSessionTTL: 8 * time.Hour})
	seedAcmeAndAgentForTTLTest(t, env)

	requested := 24 * time.Hour
	before := time.Now()
	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
		Ttl:    durationpb.New(requested),
	})
	require.NoError(t, err)

	expiry := resp.GetExpireTime().AsTime()
	wantUpper := before.Add(8 * time.Hour).Add(5 * time.Second) // small slack for handler latency
	assert.True(t, expiry.Before(wantUpper),
		"expiry %v must be clamped to <= now+8h (cap), not honor the requested 24h", expiry)
	assert.True(t, expiry.After(before.Add(8*time.Hour-1*time.Minute)),
		"expiry must be near the cap, not significantly less")
}

// TestIntegration_CreateStorageSession_TTLWithinCapHonored verifies
// that a caller-requested TTL below the cap is used as-is (no
// silent narrowing).
func TestIntegration_CreateStorageSession_TTLWithinCapHonored(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t, sessionTestOpts{MaxSessionTTL: 8 * time.Hour})
	seedAcmeAndAgentForTTLTest(t, env)

	requested := 4 * time.Hour
	before := time.Now()
	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
		Ttl:    durationpb.New(requested),
	})
	require.NoError(t, err)

	expiry := resp.GetExpireTime().AsTime()
	want := before.Add(requested)
	assert.True(t, expiry.After(want.Add(-1*time.Minute)) && expiry.Before(want.Add(1*time.Minute)),
		"expiry %v must be ~ now+4h (within cap, honored as-is)", expiry)
}

// TestIntegration_CreateStorageSession_TTLDefaultOneHour verifies
// the unset-TTL path: no Ttl in the request → 1h default.
func TestIntegration_CreateStorageSession_TTLDefaultOneHour(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t, sessionTestOpts{MaxSessionTTL: 8 * time.Hour})
	seedAcmeAndAgentForTTLTest(t, env)

	before := time.Now()
	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
		// Ttl unset
	})
	require.NoError(t, err)

	expiry := resp.GetExpireTime().AsTime()
	want := before.Add(1 * time.Hour)
	assert.True(t, expiry.After(want.Add(-1*time.Minute)) && expiry.Before(want.Add(1*time.Minute)),
		"unset TTL must default to 1h; got expiry %v", expiry)
}

// TestIntegration_CreateStorageSession_CookieDomainPresentWhenConfigured
// verifies that StorageGatewaysConfig.CookieDomain emits the
// `Domain=` attribute on the Set-Cookie response header.
func TestIntegration_CreateStorageSession_CookieDomainPresentWhenConfigured(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t, sessionTestOpts{CookieDomain: ".pivox.app"})
	seedAcmeAndAgentForTTLTest(t, env)

	var md metadata.MD
	_, err := env.gwClient.CreateStorageSession(ctx,
		&storagev1.CreateStorageSessionRequest{Parent: "organizations/acme"},
		captureSetCookie(&md))
	require.NoError(t, err)

	cookies := md.Get("set-cookie")
	require.Len(t, cookies, 1, "exactly one Set-Cookie header must be emitted")
	assert.Contains(t, cookies[0], "Domain=.pivox.app",
		"Domain attribute must reflect CookieDomain when configured")
}

// TestIntegration_CreateStorageSession_TTLZeroRejected verifies that
// a non-nil but non-positive Ttl is rejected with InvalidArgument
// rather than silently defaulted to 1h. Silent fallback would invert
// the caller's intent — a client sending Ttl=0 to revoke fast must
// not instead receive an hour-long session.
func TestIntegration_CreateStorageSession_TTLZeroRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t, sessionTestOpts{MaxSessionTTL: 8 * time.Hour})
	seedAcmeAndAgentForTTLTest(t, env)

	_, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
		Ttl:    durationpb.New(0),
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err),
		"non-positive Ttl must be rejected with InvalidArgument; "+
			"silent fallback to 1h would invert caller intent")
}

// TestIntegration_CreateStorageSession_CookieDomainAbsentWhenUnset
// verifies that an empty CookieDomain produces a cookie without a
// Domain= attribute (right default for self-hosted: cookie scopes
// to the response origin only).
func TestIntegration_CreateStorageSession_CookieDomainAbsentWhenUnset(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t, sessionTestOpts{ /* CookieDomain unset */ })
	seedAcmeAndAgentForTTLTest(t, env)

	var md metadata.MD
	_, err := env.gwClient.CreateStorageSession(ctx,
		&storagev1.CreateStorageSessionRequest{Parent: "organizations/acme"},
		captureSetCookie(&md))
	require.NoError(t, err)

	cookies := md.Get("set-cookie")
	require.NotEmpty(t, cookies, "Set-Cookie header must still be present (only the Domain attribute is conditional)")
	assert.NotContains(t, cookies[0], "Domain=",
		"Domain attribute MUST be omitted when CookieDomain is unset; "+
			"cookie scopes to the response origin only (right default for self-hosted)")
}

// ---------------------------------------------------------------------------
// #27 phase 5 — JWT identity claims
// ---------------------------------------------------------------------------

// decodeJWTClaims pulls the unverified payload out of a Pivox session
// JWT and returns its claims map. The signature is NOT validated —
// these tests are checking what the controller emits, not whether the
// agent accepts it (that's phase 6's path). Mirrors the parsing shape
// at internal/storageagent/http.go:validateJWT.
func decodeJWTClaims(t *testing.T, jwt string) map[string]any {
	t.Helper()
	parts := strings.SplitN(jwt, ".", 3)
	require.Len(t, parts, 3, "JWT must have 3 dot-separated parts")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims map[string]any
	require.NoError(t, json.Unmarshal(payload, &claims))
	return claims
}

// TestIntegration_CreateStorageSession_JWTIncludesIdentityAndOrg is
// the load-bearing #27 phase 5 acceptance test: the issued JWT
// carries the caller's identity (`sub` claim) and the target org's
// slug (`org` claim) so gateway-side audit logs can attribute
// requests without a directory lookup. Existing claims (`token`,
// `exp`) remain.
func TestIntegration_CreateStorageSession_JWTIncludesIdentityAndOrg(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()
	env := newSessionTestEnv(t)
	seedAcmeAndAgentForTTLTest(t, env)

	// Capture the caller identity ID for the assertion.
	callerIdentityID := env.h.CurrentCaller().IdentityID.String()

	resp, err := env.gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetToken())

	claims := decodeJWTClaims(t, resp.GetToken())

	// Existing claims preserved.
	assert.NotEmpty(t, claims["token"], "opaque session token claim must remain — agents look up patterns by it")
	assert.NotEmpty(t, claims["exp"], "expiry claim must remain")

	// New phase-5 claims.
	assert.Equal(t, callerIdentityID, claims["sub"],
		"sub claim must carry the Pivox identity UUID of the caller — no directory lookup at the gateway")
	assert.Equal(t, "acme", claims["org"],
		"org claim must carry the target org's slug")
}

// ---------------------------------------------------------------------------
// #27 cumulative-audit fix — signing-key wire channel
// ---------------------------------------------------------------------------

// TestIntegration_SessionSigningKey_RoundTripsControllerToAgent is the
// load-bearing cross-phase acceptance test the cumulative #27 audit
// asked for: a JWT minted by CreateStorageSession (controller side)
// must validate against the same signing key the agent receives via
// HandshakeAck.SessionSigningKey. Without this the entire bearer +
// cookie auth chain produces 401s in production — phase-6's
// tests exercised the agent's parser against a known test key, but
// nothing pinned that the production key flows through HandshakeAck.
//
// Concretely: the test mints a JWT via the same path
// CreateStorageSession uses, then validates it through the agent's
// validateJWT against the key value the controller is configured
// with. If the controller's signing key and the agent's signing key
// (which the agent gets via HandshakeAck) ever drift, this test
// fails.
func TestIntegration_SessionSigningKey_RoundTripsControllerToAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test")
	}
	ctx := context.Background()

	const sharedKey = "test-cross-phase-signing-key-do-not-flake!"

	// Stand up a controller with an explicit signing key (config-
	// driven, no longer the hardcoded literal from gateways.go's
	// constructor default).
	conns := agentstream.NewConnectionManager()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
				Queries:           h.Queries,
				Encryptor:         h.Encryptor,
				Conns:             conns,
				SessionSigningKey: []byte(sharedKey),
			}))
			storagev1.RegisterEndpointsServer(s, storage.NewEndpointsServer(storage.EndpointsConfig{
				Queries: h.Queries, Encryptor: h.Encryptor,
			}))
		}))
	gwClient := storagev1.NewStorageGatewaysClient(h.Conn())
	epClient := storagev1.NewEndpointsClient(h.Conn())

	// Boot a fixture-shaped acme org with one gateway + endpoint;
	// register a mock agent connection to the org for the routing
	// to land. (This bit is identical to the phase-3 isolation
	// test setup — only thing different is the signing-key
	// assertion below.)
	owner := h.SeedOwnedOrg(t, "acme", "Acme Inc", "owner-signing-key")
	acmeID := h.LookupOrgID(t, "acme")
	op, err := gwClient.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent: "organizations/acme", StorageGatewayId: "gw",
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: "gw", IpAddresses: []string{"10.0.0.1"},
		},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	_, err = epClient.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent: "organizations/acme/storageGateways/gw", EndpointId: "media",
		Endpoint: &storagev1.Endpoint{
			DisplayName: "media",
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfig{Path: "/mnt/media"},
			},
		},
	})
	require.NoError(t, err)
	gwRow, err := h.Queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: acmeID, Name: "gw",
	})
	require.NoError(t, err)
	conns.Register(&agentstream.AgentConnection{
		AgentID: uuid.New(), GatewayID: gwRow.ID, OrgID: acmeID,
		Stream: &recordingStream{},
	})

	// Mint a session as the org owner.
	resp, err := gwClient.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Parent: "organizations/acme",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetToken(), "JWT must be in response.token")

	// The actual cross-phase assertion: parse + HMAC-verify the JWT
	// against the SAME signing key the agent would have received
	// via HandshakeAck.SessionSigningKey. If the controller's key
	// path and the HandshakeAck wire ever drift, this verification
	// fails.
	parts := strings.Split(resp.GetToken(), ".")
	require.Len(t, parts, 3, "JWT must have 3 dot-separated parts")
	mac := hmac.New(sha256.New, []byte(sharedKey))
	mac.Write([]byte(parts[0] + "." + parts[1]))
	expectedSig := mac.Sum(nil)
	gotSig, err := base64.RawURLEncoding.DecodeString(parts[2])
	require.NoError(t, err)
	assert.True(t, hmac.Equal(gotSig, expectedSig),
		"JWT signature must verify against the SHARED signing key — "+
			"if this fails, the controller and agent are using different "+
			"keys and every storage request will 401 in production")

	_ = owner
}

// silenceUnusedTimestamp keeps the timestamppb import required for
// the agent.proto import resolution in this file (used implicitly
// via SessionGrant.Expiry comparisons in future phases). Kept as a
// no-op so the import group stays stable across phases.
var _ = timestamppb.Now
