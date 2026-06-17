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
	return grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		permResolver := permission.NewResolver(h.Queries)
		codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
		require.NoError(t, err)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Auth:       h.Auth,
			Codec:      codec,
			Resolver:   permResolver,
			LROManager: h.LROManager,
			Encryptor:  h.Encryptor,
		}))
		longrunningpb.RegisterOperationsServer(s, operations.NewOperationsServer(operations.Config{
			LRO:      h.LROManager,
			Queries:  h.Queries,
			Resolver: permResolver,
		}))
	}))
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
