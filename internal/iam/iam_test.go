package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	db "github.com/dashkan/pivox/internal/db/generated"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ---------- GetIamPolicy ----------

func TestGetIamPolicy_Success(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	policy := &iampb.Policy{
		Bindings: []*iampb.Binding{
			{Role: "roles/editor", Members: []string{"user:alice@example.com"}},
		},
	}
	policyJSON, err := protojson.Marshal(policy)
	require.NoError(t, err)

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{
			ResourceID: orgID,
			Policy:     json.RawMessage(policyJSON),
			Etag:       "etag-abc",
		}, nil)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/myorg"})
	require.NoError(t, err)
	require.Len(t, got.Bindings, 1)
	assert.Equal(t, "roles/editor", got.Bindings[0].Role)
	assert.Equal(t, []string{"user:alice@example.com"}, got.Bindings[0].Members)
	assert.Equal(t, "etag-abc", got.Etag)

	mockQ.AssertExpectations(t)
}

func TestGetIamPolicy_NotFound(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{}, pgx.ErrNoRows)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/myorg"})
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got.Bindings)
	assert.Empty(t, got.Etag)

	mockQ.AssertExpectations(t)
}

func TestGetIamPolicy_InvalidName(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "invalid"})
	require.Error(t, err)
	assert.Nil(t, got)

	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestGetIamPolicy_OrgResolution(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").
		Return(db.Organization{ID: orgID, Name: "acme"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{
			ResourceID: orgID,
			Policy:     json.RawMessage(`{}`),
			Etag:       "e1",
		}, nil)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/acme"})
	require.NoError(t, err)
	assert.Equal(t, "e1", got.Etag)

	// Verify GetOrganizationByName was called with "acme".
	mockQ.AssertCalled(t, "GetOrganizationByName", mock.Anything, "acme")
	mockQ.AssertCalled(t, "GetIamPolicy", mock.Anything, orgID)
	mockQ.AssertExpectations(t)
}

// ---------- SetIamPolicy ----------

func TestSetIamPolicy_Success(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	inputPolicy := &iampb.Policy{
		Bindings: []*iampb.Binding{
			{Role: "roles/viewer", Members: []string{"user:bob@example.com"}},
		},
	}

	// The result from the DB after upsert.
	resultPolicyJSON, err := protojson.Marshal(&iampb.Policy{
		Bindings: []*iampb.Binding{
			{Role: "roles/viewer", Members: []string{"user:bob@example.com"}},
		},
	})
	require.NoError(t, err)

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == orgID && p.ResourceType == "organizations"
	})).Return(db.IamPolicy{
		ResourceID: orgID,
		Policy:     json.RawMessage(resultPolicyJSON),
		Etag:       "new-etag",
	}, nil)

	h := NewHelper(mockQ)
	got, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/myorg",
		Policy:   inputPolicy,
	})
	require.NoError(t, err)
	require.Len(t, got.Bindings, 1)
	assert.Equal(t, "roles/viewer", got.Bindings[0].Role)
	assert.Equal(t, "new-etag", got.Etag)

	mockQ.AssertExpectations(t)
}

func TestSetIamPolicy_EtagMismatch(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{
			ResourceID: orgID,
			Policy:     json.RawMessage(`{}`),
			Etag:       "current-etag",
		}, nil)

	h := NewHelper(mockQ)
	_, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/myorg",
		Policy: &iampb.Policy{
			Etag: "stale-etag",
		},
	})
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())

	mockQ.AssertExpectations(t)
}

func TestSetIamPolicy_NilPolicy(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	// Expect upsert with an empty policy (marshaled from &iampb.Policy{}).
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == orgID
	})).Return(db.IamPolicy{
		ResourceID: orgID,
		Policy:     json.RawMessage(`{}`),
		Etag:       "fresh-etag",
	}, nil)

	h := NewHelper(mockQ)
	got, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/myorg",
		Policy:   nil, // nil policy
	})
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, "fresh-etag", got.Etag)
	assert.Empty(t, got.Bindings)

	mockQ.AssertExpectations(t)
}

// ---------- TestIamPermissions ----------

func TestTestIamPermissions(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)

	h := NewHelper(mockQ)
	perms := []string{"iam.policy.get", "iam.policy.set", "resourcemanager.projects.delete"}

	resp, err := h.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    "organizations/myorg",
		Permissions: perms,
	})
	require.NoError(t, err)
	assert.Equal(t, perms, resp.Permissions)
}

// ---------- Error paths ----------

func TestGetIamPolicy_DBError(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{}, fmt.Errorf("connection refused"))

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/myorg"})

	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestSetIamPolicy_DBError(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	// The etag check path calls GetIamPolicy, which returns a generic error.
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{}, fmt.Errorf("disk full"))

	h := NewHelper(mockQ)
	_, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/myorg",
		Policy:   &iampb.Policy{Etag: "some-etag"},
	})

	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_ProjectLookupFailure(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").
		Return(db.Organization{ID: orgID, Name: "acme"}, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{OrgID: orgID, Name: "missing"}).
		Return(db.Project{}, pgx.ErrNoRows)

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/acme/projects/missing"})

	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_TagKeyParseError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "tagKeys/not-a-uuid"})

	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// ---------- resolveResourceID (tested indirectly) ----------

func TestResolveResourceID_Organization(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").
		Return(db.Organization{ID: orgID, Name: "acme"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{}, pgx.ErrNoRows)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/acme"})
	require.NoError(t, err)
	assert.NotNil(t, got)

	mockQ.AssertCalled(t, "GetOrganizationByName", mock.Anything, "acme")
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_Project(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	projID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").
		Return(db.Organization{ID: orgID, Name: "acme"}, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{OrgID: orgID, Name: "myproject"}).
		Return(db.Project{ID: projID, OrgID: orgID, Name: "myproject"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, projID).
		Return(db.IamPolicy{}, pgx.ErrNoRows)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/acme/projects/myproject"})
	require.NoError(t, err)
	assert.NotNil(t, got)

	mockQ.AssertCalled(t, "GetOrganizationByName", mock.Anything, "acme")
	mockQ.AssertCalled(t, "GetProjectByName", mock.Anything, db.GetProjectByNameParams{OrgID: orgID, Name: "myproject"})
	mockQ.AssertCalled(t, "GetIamPolicy", mock.Anything, projID)
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_TagKey(t *testing.T) {
	ctx := context.Background()
	tagKeyID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetIamPolicy", mock.Anything, tagKeyID).
		Return(db.IamPolicy{
			ResourceID: tagKeyID,
			Policy:     json.RawMessage(`{}`),
			Etag:       "tk-etag",
		}, nil)

	h := NewHelper(mockQ)
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "tagKeys/" + tagKeyID.String()})
	require.NoError(t, err)
	assert.Equal(t, "tk-etag", got.Etag)

	mockQ.AssertCalled(t, "GetIamPolicy", mock.Anything, tagKeyID)
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_TagValue(t *testing.T) {
	ctx := context.Background()
	tagKeyID := uuid.New()
	tagValueID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetIamPolicy", mock.Anything, tagValueID).
		Return(db.IamPolicy{
			ResourceID: tagValueID,
			Policy:     json.RawMessage(`{}`),
			Etag:       "tv-etag",
		}, nil)

	h := NewHelper(mockQ)
	name := "tagKeys/" + tagKeyID.String() + "/tagValues/" + tagValueID.String()
	got, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: name})
	require.NoError(t, err)
	assert.Equal(t, "tv-etag", got.Etag)

	mockQ.AssertCalled(t, "GetIamPolicy", mock.Anything, tagValueID)
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_UnknownType(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "unknown/resource"})
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

// --- Additional coverage tests ---

func TestSetIamPolicy_UpsertDBError(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == orgID
	})).Return(db.IamPolicy{}, fmt.Errorf("disk full"))

	h := NewHelper(mockQ)
	_, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/myorg",
		Policy: &iampb.Policy{
			Bindings: []*iampb.Binding{
				{Role: "roles/viewer", Members: []string{"user:bob@example.com"}},
			},
		},
	})

	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_OrgLookupError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "badorg").
		Return(db.Organization{}, fmt.Errorf("connection refused"))

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/badorg"})
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_ProjectOrgLookupError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "badorg").
		Return(db.Organization{}, fmt.Errorf("connection refused"))

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{Name: "organizations/badorg/projects/myproj"})
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_ProjectDBError(t *testing.T) {
	ctx := context.Background()
	orgID := uuid.New()
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").
		Return(db.Organization{ID: orgID, Name: "acme"}, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{OrgID: orgID, Name: "myproj"}).
		Return(db.Project{}, fmt.Errorf("connection refused"))

	h := NewHelper(mockQ)
	_, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/acme/projects/myproj",
		Policy:   &iampb.Policy{},
	})
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestResolveResourceID_TagValueParseError(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)

	h := NewHelper(mockQ)
	_, err := h.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Name: "tagKeys/" + uuid.New().String() + "/tagValues/not-a-uuid",
	})
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSetIamPolicy_InvalidResource(t *testing.T) {
	ctx := context.Background()
	mockQ := new(mocks.MockQuerier)

	h := NewHelper(mockQ)
	_, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "invalid",
		Policy:   &iampb.Policy{},
	})
	require.Error(t, err)
	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestSetIamPolicy_EtagCheckNoExistingPolicy(t *testing.T) {
	// When etag is provided but no existing policy exists (ErrNoRows),
	// we should proceed (etag check passes).
	ctx := context.Background()
	orgID := uuid.New()

	resultPolicyJSON, err := protojson.Marshal(&iampb.Policy{})
	require.NoError(t, err)

	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetOrganizationByName", mock.Anything, "myorg").
		Return(db.Organization{ID: orgID, Name: "myorg"}, nil)
	mockQ.On("GetIamPolicy", mock.Anything, orgID).
		Return(db.IamPolicy{}, pgx.ErrNoRows)
	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == orgID
	})).Return(db.IamPolicy{
		ResourceID: orgID,
		Policy:     json.RawMessage(resultPolicyJSON),
		Etag:       "new-etag",
	}, nil)

	h := NewHelper(mockQ)
	got, err := h.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/myorg",
		Policy:   &iampb.Policy{Etag: "some-etag"},
	})
	require.NoError(t, err)
	assert.Equal(t, "new-etag", got.Etag)
	mockQ.AssertExpectations(t)
}
