//go:build dev

package organizations_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/iam"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil"
)

// noopAuthService stubs out the authn.Service interface for tests.
type noopAuthService struct{}

func (n noopAuthService) VerifyToken(_ context.Context, _ string) (*authn.Identity, error) {
	return &authn.Identity{UID: "test-user"}, nil
}

func (n noopAuthService) CreateCustomToken(_ context.Context, uid string) (string, error) {
	return "custom-token-" + uid, nil
}

// testReadUID is the AuthContextReader used by integration tests. It
// always returns the canonical test caller's UID, since these tests
// build the gRPC server directly without the production AuthInterceptor
// in the chain. The matching `accounts` row is seeded by `seedTestCaller`.
const testCallerUID = "test-user"

func testReadUID(_ context.Context) (string, bool) { return testCallerUID, true }

// seedTestCaller upserts a `firebase_identities` row for testCallerUID
// so `CreateOrganization`'s `GetFirebaseIdentityByUID` lookup
// succeeds. Returns the seeded identity's id.
func seedTestCaller(t *testing.T, queries db.Querier) uuid.UUID {
	t.Helper()
	identity, err := queries.UpsertFirebaseIdentity(context.Background(), db.UpsertFirebaseIdentityParams{
		FirebaseUid:   testCallerUID,
		Email:         "test@example.com",
		EmailVerified: true,
		DisplayName:   "Test Caller",
	})
	require.NoError(t, err)
	return identity.ID
}

func TestIntegration_CreateOrganization_DuplicateName(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	iamHelper := iam.NewHelper(queries)
	seedTestCaller(t, queries)

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(pool, queries, iamHelper, noopAuthService{}, nil, testReadUID))
	})

	client := apiv1.NewOrganizationsClient(conn)
	ctx := context.Background()

	// Create the first org.
	_, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "dupetest",
		Organization:   &apiv1.Organization{DisplayName: "First Org"},
	})
	require.NoError(t, err)

	// Create the second org with the same name.
	_, err = client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "dupetest",
		Organization:   &apiv1.Organization{DisplayName: "Duplicate Org"},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.AlreadyExists, st.Code())
}

func TestIntegration_Organizations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	iamHelper := iam.NewHelper(queries)
	seedTestCaller(t, queries)

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(pool, queries, iamHelper, noopAuthService{}, nil, testReadUID))
	})

	client := apiv1.NewOrganizationsClient(conn)
	ctx := context.Background()

	var createdOrgName string

	t.Run("CreateOrganization", func(t *testing.T) {
		op, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
			OrganizationId: "testorg",
			Organization: &apiv1.Organization{
				DisplayName: "Test Organization",
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var org apiv1.Organization
		require.NoError(t, op.GetResponse().UnmarshalTo(&org))
		assert.Equal(t, "organizations/testorg", org.GetName())
		assert.Equal(t, "Test Organization", org.GetDisplayName())
		createdOrgName = org.GetName()
	})

	t.Run("GetOrganization", func(t *testing.T) {
		resp, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
			Name: createdOrgName,
		})
		require.NoError(t, err)
		assert.Equal(t, createdOrgName, resp.GetName())
		assert.Equal(t, "Test Organization", resp.GetDisplayName())
	})

	t.Run("ListOrganizations", func(t *testing.T) {
		resp, err := client.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.GetOrganizations()), 1)

		found := false
		for _, o := range resp.GetOrganizations() {
			if o.GetName() == createdOrgName {
				found = true
			}
		}
		assert.True(t, found, "created org should appear in list")
	})

	t.Run("GetIamPolicy_Empty", func(t *testing.T) {
		resp, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
			Name: createdOrgName,
		})
		require.NoError(t, err)
		// No bindings set yet.
		assert.Empty(t, resp.GetBindings())
	})

	t.Run("SetIamPolicy", func(t *testing.T) {
		resp, err := client.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
			Resource: createdOrgName,
			Policy: &iampb.Policy{
				Bindings: []*iampb.Binding{
					{
						Role:    "roles/admin",
						Members: []string{"user:alice@example.com"},
					},
				},
			},
		})
		require.NoError(t, err)
		require.Len(t, resp.GetBindings(), 1)
		assert.Equal(t, "roles/admin", resp.GetBindings()[0].GetRole())
		assert.Contains(t, resp.GetBindings()[0].GetMembers(), "user:alice@example.com")
		assert.NotEmpty(t, resp.GetEtag())
	})

	t.Run("GetIamPolicy_WithBindings", func(t *testing.T) {
		resp, err := client.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
			Name: createdOrgName,
		})
		require.NoError(t, err)
		require.Len(t, resp.GetBindings(), 1)
		assert.Equal(t, "roles/admin", resp.GetBindings()[0].GetRole())
	})
}
