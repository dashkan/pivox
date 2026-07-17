package operations_test

import (
	"context"
	"strings"
	"testing"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/operations"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// Operations object-level authz E2E. Closes the cross-tenant IDOR where
// any authenticated caller could Get/List/Cancel/Delete any operation by
// id. Each op carries a scope (space_id ?? org_id ?? created_by); the
// server trims visibility to it.

func newOperationsHarness(t *testing.T) *grpcharness.Harness {
	return grpcharness.New(t,
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			permResolver := permission.NewResolver(h.Queries)
			codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
			require.NoError(t, err)
			apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
				Pool:       h.Pool,
				Queries:    h.Queries,
				Codec:      codec,
				Resolver:   permResolver,
				LROManager: h.LROManager,
			}))
			longrunningpb.RegisterOperationsServer(s, operations.NewOperationsServer(operations.Config{
				LRO:      h.LROManager,
				Pool:     h.Pool,
				Queries:  h.Queries,
				Codec:    codec,
				Resolver: permResolver,
			}))
		}),
		grpcharness.WithSpacesServer(),
	)
}

// containsOp reports whether ops includes one named name.
func containsOp(ops []*longrunningpb.Operation, name string) bool {
	for _, o := range ops {
		if o.GetName() == name {
			return true
		}
	}
	return false
}

// seedOp inserts an operation row directly with a pinned scope, returning
// its AIP-151 name. Bypasses NewLro so a test can fix (org, space,
// created_by) without standing up a worker.
func seedOp(t *testing.T, h *grpcharness.Harness, parent string, orgID, spaceID, createdBy pgtype.UUID) string {
	t.Helper()
	id := uuid.New()
	_, err := h.Queries.CreateOperation(context.Background(), db.CreateOperationParams{
		ID:        id,
		Parent:    parent,
		OrgID:     orgID,
		SpaceID:   spaceID,
		CreatedBy: createdBy,
	})
	require.NoError(t, err)
	return parent + "/operations/" + id.String()
}

func TestE2E_Operations_GetScopeAuthz(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newOperationsHarness(t)
	ctx := context.Background()
	ops := longrunningpb.NewOperationsClient(h.Conn())

	orgA := h.SeedOwnedOrg(t, "ops-org-a", "Ops Org A", "ownera")
	orgB := h.SeedOwnedOrg(t, "ops-org-b", "Ops Org B", "ownerb")

	orgOp := seedOp(t, h, "organizations/ops-org-a",
		convert.PgUUID(orgA.Row.ID), pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
	acctOp := seedOp(t, h, "accounts/me",
		pgtype.UUID{}, pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))

	t.Run("org member sees the org op", func(t *testing.T) {
		h.SetCaller(orgA.Owner)
		got, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: orgOp})
		require.NoError(t, err)
		assert.Equal(t, orgOp, got.GetName())
	})
	t.Run("non-member gets NotFound for the org op", func(t *testing.T) {
		h.SetCaller(orgB.Owner)
		_, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: orgOp})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
	t.Run("creator sees the account op", func(t *testing.T) {
		h.SetCaller(orgA.Owner)
		_, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: acctOp})
		require.NoError(t, err)
	})
	t.Run("non-creator gets NotFound for the account op", func(t *testing.T) {
		h.SetCaller(orgB.Owner)
		_, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: acctOp})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
	t.Run("cancel is gated by the same rule", func(t *testing.T) {
		h.SetCaller(orgB.Owner)
		_, err := ops.CancelOperation(ctx, &longrunningpb.CancelOperationRequest{Name: orgOp})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}

// TestE2E_Operations_ListPagination_RoundTrips proves keyset pagination
// works past the first page: a page_size=2 request over three visible ops
// returns two ops plus a non-empty next_page_token, and resuming with that
// token returns the remaining op with no overlap — while a cross-tenant op
// never appears on any page (the authorization trim survives the migration).
//
// Against the pre-migration handler this fails: ListOperations accepted
// page_size but never returned a next_page_token, so page two was
// unreachable and the caller could not see all three ops.
func TestE2E_Operations_ListPagination_RoundTrips(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newOperationsHarness(t)
	ctx := context.Background()
	ops := longrunningpb.NewOperationsClient(h.Conn())

	orgA := h.SeedOwnedOrg(t, "ops-pg-a", "Ops Pg A", "pga")
	orgB := h.SeedOwnedOrg(t, "ops-pg-b", "Ops Pg B", "pgb")

	// Three account-scoped ops visible only to orgA's owner.
	want := map[string]bool{}
	for range 3 {
		name := seedOp(t, h, "accounts/me",
			pgtype.UUID{}, pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
		want[name] = true
	}
	// A cross-tenant op that must never surface on any page.
	orgBOp := seedOp(t, h, "organizations/ops-pg-b",
		convert.PgUUID(orgB.Row.ID), pgtype.UUID{}, convert.PgUUID(orgB.Owner.IdentityID))

	h.SetCaller(orgA.Owner)

	page1, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{PageSize: 2})
	require.NoError(t, err)
	require.Len(t, page1.GetOperations(), 2, "first page must honor page_size=2")
	require.NotEmpty(t, page1.GetNextPageToken(), "a further page exists, so a token must be returned")

	page2, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{
		PageSize:  2,
		PageToken: page1.GetNextPageToken(),
	})
	require.NoError(t, err)
	require.Len(t, page2.GetOperations(), 1, "second page must return the remaining op")

	seen := map[string]bool{}
	for _, o := range page1.GetOperations() {
		assert.False(t, seen[o.GetName()], "no op may repeat across pages")
		seen[o.GetName()] = true
	}
	for _, o := range page2.GetOperations() {
		assert.False(t, seen[o.GetName()], "no op may repeat across pages")
		seen[o.GetName()] = true
	}
	assert.Equal(t, want, seen, "the union of both pages must be exactly the three visible ops")
	assert.False(t, seen[orgBOp], "cross-tenant op must never appear")
}

// TestE2E_Operations_ListFilter_Done proves the AIP-160 filter is wired: a
// `done = true` filter narrows the result to completed ops only. Against the
// pre-migration handler this fails: the filter was dropped and every visible
// op was returned regardless.
func TestE2E_Operations_ListFilter_Done(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newOperationsHarness(t)
	ctx := context.Background()
	ops := longrunningpb.NewOperationsClient(h.Conn())

	orgA := h.SeedOwnedOrg(t, "ops-flt-a", "Ops Flt A", "flta")

	pendingOp := seedOp(t, h, "accounts/me",
		pgtype.UUID{}, pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
	doneOp := seedOp(t, h, "accounts/me",
		pgtype.UUID{}, pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
	// Mark doneOp complete directly — the handler under test doesn't own
	// completion, and this keeps the filter assertion independent of a worker.
	doneID, err := lro.ParseOperationName(doneOp)
	require.NoError(t, err)
	_, err = h.Pool.Exec(ctx, `UPDATE operations SET done = true WHERE id = $1`, doneID)
	require.NoError(t, err)

	h.SetCaller(orgA.Owner)
	resp, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{Filter: "done = true"})
	require.NoError(t, err)

	assert.True(t, containsOp(resp.GetOperations(), doneOp), "done op must match the filter")
	assert.False(t, containsOp(resp.GetOperations(), pendingOp), "pending op must be filtered out")
}

func TestE2E_Operations_ListTrimsToVisibleScopes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newOperationsHarness(t)
	ctx := context.Background()
	ops := longrunningpb.NewOperationsClient(h.Conn())

	orgA := h.SeedOwnedOrg(t, "ops-list-a", "Ops List A", "lista")
	orgB := h.SeedOwnedOrg(t, "ops-list-b", "Ops List B", "listb")

	orgAOp := seedOp(t, h, "organizations/ops-list-a",
		convert.PgUUID(orgA.Row.ID), pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
	acctAOp := seedOp(t, h, "accounts/me",
		pgtype.UUID{}, pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
	orgBOp := seedOp(t, h, "organizations/ops-list-b",
		convert.PgUUID(orgB.Row.ID), pgtype.UUID{}, convert.PgUUID(orgB.Owner.IdentityID))

	h.SetCaller(orgA.Owner)
	resp, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{})
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, o := range resp.GetOperations() {
		seen[o.GetName()] = true
	}
	assert.True(t, seen[orgAOp], "ownerA should see orgA's op")
	assert.True(t, seen[acctAOp], "ownerA should see their own account op")
	assert.False(t, seen[orgBOp], "ownerA must NOT see orgB's op (cross-tenant)")
}

// TestE2E_Operations_SpaceScopeInheritance covers the space-scoped branch
// and specifically the org→space inheritance path: a member with only an
// org binding (no space_members row) must see a space-scoped op via their
// inherited org role. This exercises the second EXISTS in
// ListAuthorizedOperations and the resolver's space-scope union, the most
// complex authz artifact in the feature.
func TestE2E_Operations_SpaceScopeInheritance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newOperationsHarness(t)
	ctx := context.Background()
	ops := longrunningpb.NewOperationsClient(h.Conn())

	orgA := h.SeedOwnedOrg(t, "ops-space-a", "Ops Space A", "spacea")
	space := h.SeedOwnedSpace(t, "ops-space-a", "dev", "Dev Space")

	// An org editor with NO direct space membership — visibility must
	// come purely from org→space inheritance.
	inheritor := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "space-inheritor"})
	h.SeedMembership(t, orgA.Row.ID, inheritor, grpcharness.RoleEditor)

	orgB := h.SeedOwnedOrg(t, "ops-space-b", "Ops Space B", "spaceb")

	spaceOp := seedOp(t, h, "organizations/ops-space-a/spaces/dev",
		pgtype.UUID{}, convert.PgUUID(space.Row.ID), convert.PgUUID(orgA.Owner.IdentityID))

	t.Run("inherited org member sees the space op (Get)", func(t *testing.T) {
		h.SetCaller(inheritor)
		got, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: spaceOp})
		require.NoError(t, err)
		assert.Equal(t, spaceOp, got.GetName())
	})
	t.Run("inherited org member sees the space op (List)", func(t *testing.T) {
		h.SetCaller(inheritor)
		resp, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{})
		require.NoError(t, err)
		assert.True(t, containsOp(resp.GetOperations(), spaceOp),
			"org member should see the space op via inheritance")
	})
	t.Run("outsider gets NotFound for the space op", func(t *testing.T) {
		h.SetCaller(orgB.Owner)
		_, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: spaceOp})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
	t.Run("outsider's list excludes the space op", func(t *testing.T) {
		h.SetCaller(orgB.Owner)
		resp, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{})
		require.NoError(t, err)
		assert.False(t, containsOp(resp.GetOperations(), spaceOp),
			"outsider must not see another tenant's space op")
	})
}

// TestE2E_Operations_GroupBindingResolution covers the group-derived
// resolution branch — a member who holds their role ONLY via a group
// binding (no direct org_members row). This exercises the
// group_id-IN-(group_members) subquery in both EffectiveOrgPermissions
// (the org op) and EffectiveSpacePermissions' inherited-org branch (the
// space op, reached purely through a group binding at the parent org) —
// the structurally-most-complex paths in the resolver, otherwise
// untested.
func TestE2E_Operations_GroupBindingResolution(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	h := newOperationsHarness(t)
	ctx := context.Background()
	ops := longrunningpb.NewOperationsClient(h.Conn())

	orgA := h.SeedOwnedOrg(t, "ops-grp-a", "Ops Grp A", "grpa")
	space := h.SeedOwnedSpace(t, "ops-grp-a", "dev", "Dev Space")

	// Bound to editor at orgA purely through a group — no direct binding.
	member := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "group-only"})
	h.SeedGroupMembership(t, orgA.Row.ID, member, grpcharness.RoleEditor)

	orgB := h.SeedOwnedOrg(t, "ops-grp-b", "Ops Grp B", "grpb")

	orgOp := seedOp(t, h, "organizations/ops-grp-a",
		convert.PgUUID(orgA.Row.ID), pgtype.UUID{}, convert.PgUUID(orgA.Owner.IdentityID))
	spaceOp := seedOp(t, h, "organizations/ops-grp-a/spaces/dev",
		pgtype.UUID{}, convert.PgUUID(space.Row.ID), convert.PgUUID(orgA.Owner.IdentityID))

	t.Run("group-only member sees the org op (org-path group branch)", func(t *testing.T) {
		h.SetCaller(member)
		got, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: orgOp})
		require.NoError(t, err)
		assert.Equal(t, orgOp, got.GetName())
	})
	t.Run("group-only member sees the space op via inherited-org group binding", func(t *testing.T) {
		h.SetCaller(member)
		got, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: spaceOp})
		require.NoError(t, err)
		assert.Equal(t, spaceOp, got.GetName())
	})
	t.Run("group-only member's list includes both", func(t *testing.T) {
		h.SetCaller(member)
		resp, err := ops.ListOperations(ctx, &longrunningpb.ListOperationsRequest{})
		require.NoError(t, err)
		assert.True(t, containsOp(resp.GetOperations(), orgOp), "org op visible via group")
		assert.True(t, containsOp(resp.GetOperations(), spaceOp), "space op visible via inherited group")
	})
	t.Run("outsider still excluded", func(t *testing.T) {
		h.SetCaller(orgB.Owner)
		_, err := ops.GetOperation(ctx, &longrunningpb.GetOperationRequest{Name: orgOp})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})
}
