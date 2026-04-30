//go:build dev

package grpcharness_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestHarness_Smoke verifies the harness wires up the production
// interceptor chain end-to-end. Three signals:
//
//  1. An unauthenticated request is rejected by AuthInterceptor.
//  2. An authenticated-but-memberless caller is rejected by the
//     MembershipRequiredInterceptor for non-allowlisted methods.
//  3. CreateOrganization (allowlisted) succeeds for the same
//     authenticated-but-memberless caller and bootstraps an org
//     with the founder as owner.
//
// If any of these break, every Step 9 integration test breaks too —
// keeping this smoke test green is the gating signal that the
// harness-as-foundation is sound.
func TestHarness_Smoke(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
		require.NoError(t, err)
		// Caller is required by the constructor but the smoke
		// test only exercises CreateOrganization which resolves
		// the caller via ReadUID, not the Caller resolver.
		// Wire a real CallerIdentityResolver from the harness
		// queries to satisfy the required-field check.
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Auth:       h.Auth,
			Codec:      codec,
			ReadUID:    server.AuthenticatedUID,
			Caller:     callerIdentity,
			LROManager: h.LROManager,
			Encryptor:  h.Encryptor,
		}))
	}))

	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	t.Run("unauthenticated rejected", func(t *testing.T) {
		// No SetCaller — outgoing context carries no bearer token.
		_, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
			Name: "organizations/anything",
		})
		require.Error(t, err)
		assert.Equal(t, codes.Unauthenticated, status.Code(err))
	})

	caller := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(caller)

	t.Run("authenticated but memberless rejected on non-allowlisted method", func(t *testing.T) {
		// GetOrganization requires membership; the caller has none yet.
		_, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
			Name: "organizations/anything",
		})
		require.Error(t, err)
		// MembershipRequiredInterceptor returns PermissionDenied
		// (per the production interceptor's contract).
		got := status.Code(err)
		assert.True(t,
			got == codes.PermissionDenied || got == codes.NotFound,
			"expected PermissionDenied or NotFound, got %v", got)
	})

	t.Run("CreateOrganization allowlisted bootstraps owner", func(t *testing.T) {
		op, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
			OrganizationId: "smoke-org",
			Organization:   &apiv1.Organization{DisplayName: "Smoke Test Org"},
		})
		require.NoError(t, err)
		require.True(t, op.GetDone(), "CreateOrganization is sync; LRO should be done immediately")

		var org apiv1.Organization
		require.NoError(t, op.GetResponse().UnmarshalTo(&org))
		assert.Equal(t, "organizations/smoke-org", org.GetName())
		assert.Equal(t, "Smoke Test Org", org.GetDisplayName())
	})

	t.Run("now-member can read own org", func(t *testing.T) {
		// After CreateOrganization, the caller has owner membership
		// and should pass both Membership + Permission gates.
		got, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
			Name: "organizations/smoke-org",
		})
		require.NoError(t, err)
		assert.Equal(t, "Smoke Test Org", got.GetDisplayName())
	})
}
