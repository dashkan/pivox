package tags

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/iam"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// mockDBTX is a minimal db.DBTX used to inject scan errors into List* tests.
type mockDBTX struct {
	queryErr  error
	queryRows pgx.Rows
}

func (m *mockDBTX) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (m *mockDBTX) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.queryRows, nil
}

func (m *mockDBTX) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	return nil
}

// errRows is a pgx.Rows implementation that immediately returns an error from Err().
type errRows struct {
	err error
}

func (r *errRows) Close()                                       {}
func (r *errRows) Err() error                                   { return r.err }
func (r *errRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *errRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *errRows) Next() bool                                   { return false }
func (r *errRows) Scan(dest ...any) error                       { return r.err }
func (r *errRows) Values() ([]any, error)                       { return nil, r.err }
func (r *errRows) RawValues() [][]byte                          { return nil }
func (r *errRows) Conn() *pgx.Conn                              { return nil }

var (
	testOrgID    = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testTagKeyID = uuid.MustParse("0192a000-0002-7000-8000-000000000001")
	testTagValID = uuid.MustParse("0192a000-0003-7000-8000-000000000001")
	testBindID   = uuid.MustParse("0192a000-0004-7000-8000-000000000001")
	testOrg      = db.Organization{
		ID:          testOrgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testTagKey = db.TagKey{
		ID:             testTagKeyID,
		OrgID:          testOrgID,
		ShortName:      "env",
		NamespacedName: testOrgID.String() + "/env",
		Description:    "Environment tag",
		Annotations:    json.RawMessage(`{}`),
		Etag:           "etag-tk-1",
		Revision:       1,
		CreateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testTagValue = db.TagValue{
		ID:             testTagValID,
		TagKeyID:       testTagKeyID,
		ShortName:      "prod",
		NamespacedName: testOrgID.String() + "/env/prod",
		Description:    "Production",
		Annotations:    json.RawMessage(`{}`),
		Etag:           "etag-tv-1",
		Revision:       1,
		CreateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testTagBinding = db.TagBinding{
		ID:             testBindID,
		ParentResource: "organizations/acme/storageGateways/gw-1",
		TagValueID:     testTagValID,
		Origin:         db.TagBindingOriginUSER,
		Annotations:    json.RawMessage(`{}`),
		Etag:           "etag-tb-1",
		CreateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

// =========================================================================
// TagKeys
// =========================================================================

func TestUnit_CreateTagKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateTagKey", mock.Anything, mock.MatchedBy(func(p db.CreateTagKeyParams) bool {
		return p.OrgID == testOrgID && p.ShortName == "env"
	})).Return(testTagKey, nil)

	resp, err := srv.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
		Parent:   "organizations/acme",
		TagKeyId: "env",
		TagKey:   &apiv1.TagKey{Description: "Environment tag"},
	})

	require.NoError(t, err)
	// CreateTagKey returns an LRO. Verify the inner response.
	assert.True(t, resp.GetDone())
	inner := new(apiv1.TagKey)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "tagKeys/"+testTagKeyID.String(), inner.GetName())
	assert.Equal(t, "Environment tag", inner.GetDescription())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetTagKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)

	resp, err := srv.GetTagKey(ctx, &apiv1.GetTagKeyRequest{
		Name: "tagKeys/" + testTagKeyID.String(),
	})

	require.NoError(t, err)
	assert.Equal(t, "tagKeys/"+testTagKeyID.String(), resp.GetName())
	assert.Equal(t, "Environment tag", resp.GetDescription())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateTagKey_WithMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	updatedKey := testTagKey
	updatedKey.Description = "Updated description"
	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
	mockQ.On("UpdateTagKey", mock.Anything, mock.MatchedBy(func(p db.UpdateTagKeyParams) bool {
		return p.ID == testTagKeyID && p.Description.Valid && p.Description.String == "Updated description"
	})).Return(updatedKey, nil)

	resp, err := srv.UpdateTagKey(ctx, &apiv1.UpdateTagKeyRequest{
		TagKey: &apiv1.TagKey{
			Name:        "tagKeys/" + testTagKeyID.String(),
			Description: "Updated description",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateTagKey_NoMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	updatedKey := testTagKey
	updatedKey.Description = "Full update"
	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
	mockQ.On("UpdateTagKey", mock.Anything, mock.MatchedBy(func(p db.UpdateTagKeyParams) bool {
		return p.ID == testTagKeyID && p.Description.Valid && p.Description.String == "Full update"
	})).Return(updatedKey, nil)

	resp, err := srv.UpdateTagKey(ctx, &apiv1.UpdateTagKeyRequest{
		TagKey: &apiv1.TagKey{
			Name:        "tagKeys/" + testTagKeyID.String(),
			Description: "Full update",
		},
		// No UpdateMask
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteTagKey_WithValues(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
	mockQ.On("CountTagValuesByTagKey", mock.Anything, testTagKeyID).Return(int64(3), nil)

	_, err := srv.DeleteTagKey(ctx, &apiv1.DeleteTagKeyRequest{
		Name: "tagKeys/" + testTagKeyID.String(),
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "existing tag values")
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteTagKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
	mockQ.On("CountTagValuesByTagKey", mock.Anything, testTagKeyID).Return(int64(0), nil)
	mockQ.On("DeleteTagKey", mock.Anything, testTagKeyID).Return(nil)

	resp, err := srv.DeleteTagKey(ctx, &apiv1.DeleteTagKeyRequest{
		Name: "tagKeys/" + testTagKeyID.String(),
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// =========================================================================
// TagValues
// =========================================================================

func TestUnit_GetTagKey_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(db.TagKey{}, pgx.ErrNoRows)

	_, err := srv.GetTagKey(ctx, &apiv1.GetTagKeyRequest{
		Name: "tagKeys/" + testTagKeyID.String(),
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// IAM delegation tests for TagKeys
func TestGetIamPolicy_TagKeys(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetIamPolicy", mock.Anything, testTagKeyID).Return(db.IamPolicy{}, pgx.ErrNoRows)

	resp, err := srv.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Name: "tagKeys/" + testTagKeyID.String(),
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetBindings())
	mockQ.AssertExpectations(t)
}

func TestSetIamPolicy_TagKeys(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == testTagKeyID && p.ResourceType == "tagKeys"
	})).Return(db.IamPolicy{
		ResourceID: testTagKeyID,
		Policy:     json.RawMessage(`{}`),
		Etag:       "new-etag",
	}, nil)

	resp, err := srv.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "tagKeys/" + testTagKeyID.String(),
		Policy:   &iampb.Policy{},
	})

	require.NoError(t, err)
	assert.Equal(t, "new-etag", resp.GetEtag())
	mockQ.AssertExpectations(t)
}

func TestTestIamPermissions_TagKeys(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	perms := []string{"pivox.tagKeys.get", "pivox.tagKeys.delete"}
	resp, err := srv.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    "tagKeys/" + testTagKeyID.String(),
		Permissions: perms,
	})

	require.NoError(t, err)
	assert.Equal(t, perms, resp.GetPermissions())
}

func TestUnit_CreateTagValue_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
	mockQ.On("CreateTagValue", mock.Anything, mock.MatchedBy(func(p db.CreateTagValueParams) bool {
		return p.TagKeyID == testTagKeyID && p.ShortName == "prod"
	})).Return(testTagValue, nil)

	resp, err := srv.CreateTagValue(ctx, &apiv1.CreateTagValueRequest{
		Parent:     "tagKeys/" + testTagKeyID.String(),
		TagValueId: "prod",
		TagValue:   &apiv1.TagValue{Description: "Production"},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(apiv1.TagValue)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Contains(t, inner.GetName(), testTagValID.String())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateTagValue_ParentNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	bogusKeyID := uuid.MustParse("0192a000-9999-7000-8000-000000000001")
	mockQ.On("GetTagKey", mock.Anything, bogusKeyID).Return(db.TagKey{}, pgx.ErrNoRows)

	_, err := srv.CreateTagValue(ctx, &apiv1.CreateTagValueRequest{
		Parent:     "tagKeys/" + bogusKeyID.String(),
		TagValueId: "prod",
		TagValue:   &apiv1.TagValue{Description: "Production"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetTagValue_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)

	resp, err := srv.GetTagValue(ctx, &apiv1.GetTagValueRequest{
		Name: tvName,
	})

	require.NoError(t, err)
	assert.Contains(t, resp.GetName(), testTagValID.String())
	assert.Equal(t, "Production", resp.GetDescription())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateTagValue_WithMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	updatedVal := testTagValue
	updatedVal.Description = "Staging"

	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
	mockQ.On("UpdateTagValue", mock.Anything, mock.MatchedBy(func(p db.UpdateTagValueParams) bool {
		return p.ID == testTagValID && p.Description.Valid && p.Description.String == "Staging"
	})).Return(updatedVal, nil)

	resp, err := srv.UpdateTagValue(ctx, &apiv1.UpdateTagValueRequest{
		TagValue: &apiv1.TagValue{
			Name:        tvName,
			Description: "Staging",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"description"}},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateTagValue_NoMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	updatedVal := testTagValue
	updatedVal.Description = "Full update"

	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
	mockQ.On("UpdateTagValue", mock.Anything, mock.MatchedBy(func(p db.UpdateTagValueParams) bool {
		return p.ID == testTagValID && p.Description.Valid && p.Description.String == "Full update"
	})).Return(updatedVal, nil)

	resp, err := srv.UpdateTagValue(ctx, &apiv1.UpdateTagValueRequest{
		TagValue: &apiv1.TagValue{
			Name:        tvName,
			Description: "Full update",
		},
		// No UpdateMask
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// IAM delegation tests for TagValues
func TestGetIamPolicy_TagValues(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetIamPolicy", mock.Anything, testTagValID).Return(db.IamPolicy{}, pgx.ErrNoRows)

	resp, err := srv.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Name: "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String(),
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetBindings())
	mockQ.AssertExpectations(t)
}

func TestSetIamPolicy_TagValues(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == testTagValID && p.ResourceType == "tagKeys"
	})).Return(db.IamPolicy{
		ResourceID: testTagValID,
		Policy:     json.RawMessage(`{}`),
		Etag:       "tv-etag",
	}, nil)

	resp, err := srv.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String(),
		Policy:   &iampb.Policy{},
	})

	require.NoError(t, err)
	assert.Equal(t, "tv-etag", resp.GetEtag())
	mockQ.AssertExpectations(t)
}

func TestTestIamPermissions_TagValues(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	perms := []string{"pivox.tagValues.get"}
	resp, err := srv.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String(),
		Permissions: perms,
	})

	require.NoError(t, err)
	assert.Equal(t, perms, resp.GetPermissions())
}

func TestUnit_DeleteTagValue_WithBindings(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
	mockQ.On("CountTagBindingsByTagValue", mock.Anything, testTagValID).Return(int64(5), nil)

	_, err := srv.DeleteTagValue(ctx, &apiv1.DeleteTagValueRequest{
		Name: tvName,
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "existing tag bindings")
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteTagValue_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagValuesServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
	mockQ.On("CountTagBindingsByTagValue", mock.Anything, testTagValID).Return(int64(0), nil)
	mockQ.On("DeleteTagValue", mock.Anything, testTagValID).Return(nil)

	resp, err := srv.DeleteTagValue(ctx, &apiv1.DeleteTagValueRequest{
		Name: tvName,
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// =========================================================================
// TagBindings
// =========================================================================

func TestUnit_GetTagBinding_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewTagBindingsServer(nil, mockQ)
	ctx := context.Background()

	mockQ.On("GetTagBinding", mock.Anything, testBindID).Return(testTagBinding, nil)
	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)

	resp, err := srv.GetTagBinding(ctx, &apiv1.GetTagBindingRequest{
		Name: "tagBindings/" + testBindID.String(),
	})

	require.NoError(t, err)
	assert.Contains(t, resp.GetName(), testBindID.String())
	assert.Contains(t, resp.GetTagValue(), testTagValID.String())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateTagBinding_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewTagBindingsServer(nil, mockQ)
	ctx := context.Background()

	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
	mockQ.On("CreateTagBinding", mock.Anything, mock.MatchedBy(func(p db.CreateTagBindingParams) bool {
		return p.TagValueID == testTagValID && p.ParentResource == "organizations/acme/storageGateways/gw-1"
	})).Return(testTagBinding, nil)

	resp, err := srv.CreateTagBinding(ctx, &apiv1.CreateTagBindingRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
		TagBinding: &apiv1.TagBinding{
			TagValue: tvName,
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(apiv1.TagBinding)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Contains(t, inner.GetName(), testBindID.String())
	assert.Equal(t, tvName, inner.GetTagValue())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteTagBinding_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewTagBindingsServer(nil, mockQ)
	ctx := context.Background()

	mockQ.On("GetTagBinding", mock.Anything, testBindID).Return(testTagBinding, nil)
	mockQ.On("DeleteTagBinding", mock.Anything, testBindID).Return(nil)

	resp, err := srv.DeleteTagBinding(ctx, &apiv1.DeleteTagBindingRequest{
		Name: "tagBindings/" + testBindID.String(),
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListEffectiveTags_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewTagBindingsServer(nil, mockQ)
	ctx := context.Background()

	rows := []db.ListEffectiveTagsRow{
		{
			TagValueID:             testTagValID,
			TagKeyID:               testTagKeyID,
			TagValueNamespacedName: testOrgID.String() + "/env/prod",
			TagKeyNamespacedName:   testOrgID.String() + "/env",
		},
	}
	mockQ.On("ListEffectiveTags", mock.Anything, "organizations/acme/storageGateways/gw-1").Return(rows, nil)

	resp, err := srv.ListEffectiveTags(ctx, &apiv1.ListEffectiveTagsRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	require.Len(t, resp.GetEffectiveTags(), 1)
	et := resp.GetEffectiveTags()[0]
	assert.Equal(t, "tagKeys/"+testTagKeyID.String()+"/tagValues/"+testTagValID.String(), et.GetTagValue())
	assert.Equal(t, "tagKeys/"+testTagKeyID.String(), et.GetTagKey())
	mockQ.AssertExpectations(t)
}

// =========================================================================
// TagKeys — error paths
// =========================================================================

func TestUnit_GetTagKey_InvalidName(t *testing.T) {
	tests := []struct {
		name     string
		reqName  string
		wantCode codes.Code
	}{
		{
			name:     "missing slash",
			reqName:  "tagKeys",
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID segment",
			reqName:  "tagKeys/not-a-uuid",
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagKeysServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			_, err := srv.GetTagKey(ctx, &apiv1.GetTagKeyRequest{Name: tc.reqName})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_CreateTagKey_InvalidParent(t *testing.T) {
	tests := []struct {
		name     string
		parent   string
		wantCode codes.Code
	}{
		{
			name:     "wrong collection",
			parent:   "projects/acme",
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "org not found",
			parent:   "organizations/notexist",
			wantCode: codes.NotFound,
		},
		{
			name:     "org db error",
			parent:   "organizations/dberr",
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagKeysServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			switch tc.name {
			case "org not found":
				mockQ.On("GetOrganizationByName", mock.Anything, "notexist").Return(db.Organization{}, pgx.ErrNoRows)
			case "org db error":
				mockQ.On("GetOrganizationByName", mock.Anything, "dberr").Return(db.Organization{}, fmt.Errorf("connection reset"))
			}

			_, err := srv.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
				Parent:   tc.parent,
				TagKeyId: "env",
				TagKey:   &apiv1.TagKey{},
			})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_CreateTagKey_DBError(t *testing.T) {
	tests := []struct {
		name     string
		dbErr    error
		wantCode codes.Code
	}{
		{
			name:     "duplicate key",
			dbErr:    fmt.Errorf("ERROR: duplicate key value violates unique constraint"),
			wantCode: codes.AlreadyExists,
		},
		{
			name:     "internal db error",
			dbErr:    fmt.Errorf("connection reset by peer"),
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagKeysServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
			mockQ.On("CreateTagKey", mock.Anything, mock.Anything).Return(db.TagKey{}, tc.dbErr)

			_, err := srv.CreateTagKey(ctx, &apiv1.CreateTagKeyRequest{
				Parent:   "organizations/acme",
				TagKeyId: "env",
				TagKey:   &apiv1.TagKey{},
			})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_UpdateTagKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		tagKey   *apiv1.TagKey
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name no slash",
			tagKey:   &apiv1.TagKey{Name: "tagKeys"},
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID",
			tagKey:   &apiv1.TagKey{Name: "tagKeys/not-a-uuid"},
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:   "not found",
			tagKey: &apiv1.TagKey{Name: "tagKeys/" + testTagKeyID.String()},
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(db.TagKey{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:   "update db error",
			tagKey: &apiv1.TagKey{Name: "tagKeys/" + testTagKeyID.String(), Description: "new desc"},
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
				mockQ.On("UpdateTagKey", mock.Anything, mock.Anything).Return(db.TagKey{}, fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagKeysServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.UpdateTagKey(ctx, &apiv1.UpdateTagKeyRequest{
				TagKey: tc.tagKey,
			})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_DeleteTagKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		reqName  string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name",
			reqName:  "tagKeys",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID",
			reqName:  "tagKeys/not-a-uuid",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:    "not found",
			reqName: "tagKeys/" + testTagKeyID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(db.TagKey{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "count error",
			reqName: "tagKeys/" + testTagKeyID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
				mockQ.On("CountTagValuesByTagKey", mock.Anything, testTagKeyID).Return(int64(0), fmt.Errorf("db error"))
			},
			wantCode: codes.Internal,
		},
		{
			name:    "delete db error",
			reqName: "tagKeys/" + testTagKeyID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
				mockQ.On("CountTagValuesByTagKey", mock.Anything, testTagKeyID).Return(int64(0), nil)
				mockQ.On("DeleteTagKey", mock.Anything, testTagKeyID).Return(fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagKeysServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.DeleteTagKey(ctx, &apiv1.DeleteTagKeyRequest{Name: tc.reqName})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_ListTagKeys_QueryError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	mockDB := &mockDBTX{queryErr: fmt.Errorf("filter parse error: unknown field")}
	srv := NewTagKeysServer(mockDB, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)

	_, err := srv.ListTagKeys(ctx, &apiv1.ListTagKeysRequest{
		Parent: "organizations/acme",
		Filter: "invalid_field = true",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListTagKeys_ScanError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	scanErr := fmt.Errorf("scan error")
	mockDB := &mockDBTX{queryRows: &errRows{err: scanErr}}
	srv := NewTagKeysServer(mockDB, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)

	_, err := srv.ListTagKeys(ctx, &apiv1.ListTagKeysRequest{
		Parent: "organizations/acme",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListTagKeys_InvalidParent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewTagKeysServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	_, err := srv.ListTagKeys(ctx, &apiv1.ListTagKeysRequest{
		Parent: "projects/acme",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

// =========================================================================
// TagValues — error paths
// =========================================================================

func TestUnit_GetTagValue_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		reqName  string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name format",
			reqName:  "tagKeys/invalid-name",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID in tag value segment",
			reqName:  "tagKeys/" + testTagKeyID.String() + "/tagValues/not-a-uuid",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:    "tag value not found",
			reqName: "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(db.TagValue{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagValuesServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.GetTagValue(ctx, &apiv1.GetTagValueRequest{Name: tc.reqName})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_CreateTagValue_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		parent   string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid parent format",
			parent:   "organizations/acme",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:   "parent tag key internal error",
			parent: "tagKeys/" + testTagKeyID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(db.TagKey{}, fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
		{
			name:   "duplicate tag value",
			parent: "tagKeys/" + testTagKeyID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
				mockQ.On("CreateTagValue", mock.Anything, mock.Anything).Return(db.TagValue{}, fmt.Errorf("duplicate key value violates unique constraint"))
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name:   "create tag value db error",
			parent: "tagKeys/" + testTagKeyID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)
				mockQ.On("CreateTagValue", mock.Anything, mock.Anything).Return(db.TagValue{}, fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagValuesServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.CreateTagValue(ctx, &apiv1.CreateTagValueRequest{
				Parent:     tc.parent,
				TagValueId: "prod",
				TagValue:   &apiv1.TagValue{},
			})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_UpdateTagValue_ErrorPaths(t *testing.T) {
	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	tests := []struct {
		name     string
		tagValue *apiv1.TagValue
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name format",
			tagValue: &apiv1.TagValue{Name: "tagKeys/invalid"},
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID",
			tagValue: &apiv1.TagValue{Name: "tagKeys/" + testTagKeyID.String() + "/tagValues/not-a-uuid"},
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "tag value not found",
			tagValue: &apiv1.TagValue{Name: tvName},
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(db.TagValue{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:     "update db error",
			tagValue: &apiv1.TagValue{Name: tvName, Description: "new"},
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
				mockQ.On("UpdateTagValue", mock.Anything, mock.Anything).Return(db.TagValue{}, fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagValuesServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.UpdateTagValue(ctx, &apiv1.UpdateTagValueRequest{
				TagValue: tc.tagValue,
			})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_DeleteTagValue_ErrorPaths(t *testing.T) {
	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	tests := []struct {
		name     string
		reqName  string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name format",
			reqName:  "tagKeys/invalid",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID",
			reqName:  "tagKeys/" + testTagKeyID.String() + "/tagValues/not-a-uuid",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:    "tag value not found",
			reqName: tvName,
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(db.TagValue{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "count bindings db error",
			reqName: tvName,
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
				mockQ.On("CountTagBindingsByTagValue", mock.Anything, testTagValID).Return(int64(0), fmt.Errorf("db error"))
			},
			wantCode: codes.Internal,
		},
		{
			name:    "delete db error",
			reqName: tvName,
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
				mockQ.On("CountTagBindingsByTagValue", mock.Anything, testTagValID).Return(int64(0), nil)
				mockQ.On("DeleteTagValue", mock.Anything, testTagValID).Return(fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			iamHelper := iam.NewHelper(mockQ)
			srv := NewTagValuesServer(nil, mockQ, iamHelper)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.DeleteTagValue(ctx, &apiv1.DeleteTagValueRequest{Name: tc.reqName})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_ListTagValues_ErrorPaths(t *testing.T) {
	t.Run("invalid parent format", func(t *testing.T) {
		mockQ := new(mocks.MockQuerier)
		iamHelper := iam.NewHelper(mockQ)
		srv := NewTagValuesServer(nil, mockQ, iamHelper)
		ctx := context.Background()

		_, err := srv.ListTagValues(ctx, &apiv1.ListTagValuesRequest{
			Parent: "organizations/acme",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		mockQ.AssertExpectations(t)
	})

	t.Run("parent tag key not found", func(t *testing.T) {
		mockQ := new(mocks.MockQuerier)
		iamHelper := iam.NewHelper(mockQ)
		srv := NewTagValuesServer(nil, mockQ, iamHelper)
		ctx := context.Background()

		mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(db.TagKey{}, pgx.ErrNoRows)

		_, err := srv.ListTagValues(ctx, &apiv1.ListTagValuesRequest{
			Parent: "tagKeys/" + testTagKeyID.String(),
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.NotFound, st.Code())
		mockQ.AssertExpectations(t)
	})

	t.Run("filter query error", func(t *testing.T) {
		mockQ := new(mocks.MockQuerier)
		iamHelper := iam.NewHelper(mockQ)
		mockDB := &mockDBTX{queryErr: fmt.Errorf("filter parse error: unknown field")}
		srv := NewTagValuesServer(mockDB, mockQ, iamHelper)
		ctx := context.Background()

		mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)

		_, err := srv.ListTagValues(ctx, &apiv1.ListTagValuesRequest{
			Parent: "tagKeys/" + testTagKeyID.String(),
			Filter: "invalid_field = true",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		mockQ.AssertExpectations(t)
	})

	t.Run("scan error", func(t *testing.T) {
		mockQ := new(mocks.MockQuerier)
		iamHelper := iam.NewHelper(mockQ)
		scanErr := fmt.Errorf("scan error")
		mockDB := &mockDBTX{queryRows: &errRows{err: scanErr}}
		srv := NewTagValuesServer(mockDB, mockQ, iamHelper)
		ctx := context.Background()

		mockQ.On("GetTagKey", mock.Anything, testTagKeyID).Return(testTagKey, nil)

		_, err := srv.ListTagValues(ctx, &apiv1.ListTagValuesRequest{
			Parent: "tagKeys/" + testTagKeyID.String(),
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		mockQ.AssertExpectations(t)
	})
}

// =========================================================================
// TagBindings — error paths
// =========================================================================

func TestUnit_GetTagBinding_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		reqName  string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name no slash",
			reqName:  "tagBindings",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID",
			reqName:  "tagBindings/not-a-uuid",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:    "binding not found",
			reqName: "tagBindings/" + testBindID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagBinding", mock.Anything, testBindID).Return(db.TagBinding{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "tag value not found after binding fetch",
			reqName: "tagBindings/" + testBindID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagBinding", mock.Anything, testBindID).Return(testTagBinding, nil)
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(db.TagValue{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := NewTagBindingsServer(nil, mockQ)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.GetTagBinding(ctx, &apiv1.GetTagBindingRequest{Name: tc.reqName})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_CreateTagBinding_ErrorPaths(t *testing.T) {
	tvName := "tagKeys/" + testTagKeyID.String() + "/tagValues/" + testTagValID.String()
	tests := []struct {
		name     string
		tagValue string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid tag value name",
			tagValue: "tagKeys/invalid-name",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "tag value not found",
			tagValue: tvName,
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(db.TagValue{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:     "duplicate binding",
			tagValue: tvName,
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
				mockQ.On("CreateTagBinding", mock.Anything, mock.Anything).Return(db.TagBinding{}, fmt.Errorf("duplicate key value violates unique constraint"))
			},
			wantCode: codes.AlreadyExists,
		},
		{
			name:     "create db error",
			tagValue: tvName,
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagValue", mock.Anything, testTagValID).Return(testTagValue, nil)
				mockQ.On("CreateTagBinding", mock.Anything, mock.Anything).Return(db.TagBinding{}, fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := NewTagBindingsServer(nil, mockQ)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.CreateTagBinding(ctx, &apiv1.CreateTagBindingRequest{
				Parent: "organizations/acme/storageGateways/gw-1",
				TagBinding: &apiv1.TagBinding{
					TagValue: tc.tagValue,
				},
			})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_DeleteTagBinding_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		reqName  string
		setup    func(mockQ *mocks.MockQuerier)
		wantCode codes.Code
	}{
		{
			name:     "invalid name no slash",
			reqName:  "tagBindings",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:     "invalid UUID",
			reqName:  "tagBindings/not-a-uuid",
			setup:    func(mockQ *mocks.MockQuerier) {},
			wantCode: codes.Internal,
		},
		{
			name:    "binding not found",
			reqName: "tagBindings/" + testBindID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagBinding", mock.Anything, testBindID).Return(db.TagBinding{}, pgx.ErrNoRows)
			},
			wantCode: codes.NotFound,
		},
		{
			name:    "delete db error",
			reqName: "tagBindings/" + testBindID.String(),
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetTagBinding", mock.Anything, testBindID).Return(testTagBinding, nil)
				mockQ.On("DeleteTagBinding", mock.Anything, testBindID).Return(fmt.Errorf("connection reset"))
			},
			wantCode: codes.Internal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := NewTagBindingsServer(nil, mockQ)
			ctx := context.Background()

			tc.setup(mockQ)

			_, err := srv.DeleteTagBinding(ctx, &apiv1.DeleteTagBindingRequest{Name: tc.reqName})

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_ListTagBindings_ErrorPaths(t *testing.T) {
	t.Run("filter query error", func(t *testing.T) {
		mockQ := new(mocks.MockQuerier)
		mockDB := &mockDBTX{queryErr: fmt.Errorf("filter parse error: unknown field")}
		srv := NewTagBindingsServer(mockDB, mockQ)
		ctx := context.Background()

		_, err := srv.ListTagBindings(ctx, &apiv1.ListTagBindingsRequest{
			Parent: "organizations/acme/storageGateways/gw-1",
			Filter: "invalid_field = true",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		mockQ.AssertExpectations(t)
	})

	t.Run("scan error", func(t *testing.T) {
		mockQ := new(mocks.MockQuerier)
		scanErr := fmt.Errorf("scan error")
		mockDB := &mockDBTX{queryRows: &errRows{err: scanErr}}
		srv := NewTagBindingsServer(mockDB, mockQ)
		ctx := context.Background()

		_, err := srv.ListTagBindings(ctx, &apiv1.ListTagBindingsRequest{
			Parent: "organizations/acme/storageGateways/gw-1",
		})

		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		mockQ.AssertExpectations(t)
	})
}

func TestUnit_ListEffectiveTags_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewTagBindingsServer(nil, mockQ)
	ctx := context.Background()

	mockQ.On("ListEffectiveTags", mock.Anything, "organizations/acme/storageGateways/gw-1").
		Return([]db.ListEffectiveTagsRow{}, fmt.Errorf("connection reset"))

	_, err := srv.ListEffectiveTags(ctx, &apiv1.ListEffectiveTagsRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}
