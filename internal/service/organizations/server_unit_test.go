package organizations

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/iam"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

var (
	orgID   = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testOrg = db.Organization{
		ID:          orgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-org-1",
		Revision:    1,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

func newTestServer(q *mocks.MockQuerier) *OrganizationsServer {
	iamHelper := iam.NewHelper(q)
	return &OrganizationsServer{
		queries: q,
		iam:     iamHelper,
		filter:  filter.OrganizationFilter(),
	}
}

// ---------------------------------------------------------------------------
// GetOrganization
// ---------------------------------------------------------------------------

func TestUnit_GetOrganization_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)

	resp, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/acme",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme", resp.GetName())
	assert.Equal(t, "Acme Corp", resp.GetDisplayName())
	assert.Equal(t, "etag-org-1", resp.GetEtag())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetOrganization_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "nonexistent").Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/nonexistent",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetOrganization_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	_, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "invalid",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// ParseSegment fails, HandleResourceError returns Internal for non-pgx errors
	assert.NotEqual(t, codes.OK, st.Code())
}

// ---------------------------------------------------------------------------
// GetIamPolicy -- delegates to iam.Helper
// ---------------------------------------------------------------------------

func TestUnit_GetIamPolicy_Delegated(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	// iam.Helper.resolveResourceID looks up the org by name.
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	// Then GetIamPolicy queries the IAM policy; return ErrNoRows -> empty policy.
	mockQ.On("GetIamPolicy", mock.Anything, orgID).Return(db.IamPolicy{}, pgx.ErrNoRows)

	resp, err := srv.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Name: "organizations/acme",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Empty policy has no bindings.
	assert.Empty(t, resp.GetBindings())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// TestIamPermissions -- returns all requested permissions
// ---------------------------------------------------------------------------

func TestUnit_TestIamPermissions(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	resp, err := srv.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    "organizations/acme",
		Permissions: []string{"pivox.organizations.get", "pivox.organizations.delete"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"pivox.organizations.get", "pivox.organizations.delete"}, resp.GetPermissions())
}
