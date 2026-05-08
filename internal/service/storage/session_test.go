package storage_test

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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

func newSessionTestEnv(t *testing.T) *sessionTestEnv {
	t.Helper()
	conns := agentstream.NewConnectionManager()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithSpacesServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
				Queries: h.Queries, Encryptor: h.Encryptor, Conns: conns,
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
				Filesystem: &storagev1.FileSystemConfiguration{
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

// silenceUnusedTimestamp keeps the timestamppb import required for
// the agent.proto import resolution in this file (used implicitly
// via SessionGrant.Expiry comparisons in future phases). Kept as a
// no-op so the import group stays stable across phases.
var _ = timestamppb.Now
