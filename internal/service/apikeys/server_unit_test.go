package apikeys

import (
	"context"
	"encoding/json"
	"testing"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// apikeyCallerPivoxUUID is the canonical caller pivox_user_id used by
// apikeys-service tests that exercise audit fields on Create/Update
// paths.
var apikeyCallerPivoxUUID = uuid.MustParse("0192a000-cccc-7000-8000-00000000eeee")

func callerCtx() context.Context {
	return server.WithPivoxUserID(context.Background(), apikeyCallerPivoxUUID)
}

var (
	testOrgID = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testKeyID = uuid.MustParse("0192a000-0002-7000-8000-000000000001")
	testOrg   = db.Organization{
		ID:          testOrgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testDBKey = db.ApiKey{
		ID:          testKeyID,
		OrgID:       testOrgID,
		KeyID:       "my-key",
		KeyString:   "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklm",
		DisplayName: "My API Key",
		Annotations: json.RawMessage(`{"env":"prod"}`),
		Etag:        "etag-1",
		Revision:    1,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

func TestUnit_CreateKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateApiKey", mock.Anything, mock.MatchedBy(func(p db.CreateApiKeyParams) bool {
		return p.OrgID == testOrgID && p.DisplayName == "New Key" && p.KeyID == "custom-id" &&
			p.CreatedBy == convert.PgUUID(apikeyCallerPivoxUUID)
	})).Return(db.ApiKey{
		ID:          uuid.New(),
		OrgID:       testOrgID,
		KeyID:       "custom-id",
		KeyString:   "generated-key-string-placeholder-12345",
		DisplayName: "New Key",
		Annotations: json.RawMessage(`{}`),
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}, nil)

	resp, err := srv.CreateKey(ctx, &apiv1.CreateKeyRequest{
		Parent: "organizations/acme",
		Key:    &apiv1.Key{DisplayName: "New Key"},
		KeyId:  "custom-id",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/keys/custom-id", resp.GetName())
	assert.Equal(t, "New Key", resp.GetDisplayName())
	// key_string is never returned in the proto conversion
	assert.Empty(t, resp.GetKeyString())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateKey_InvalidParent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	_, err := srv.CreateKey(ctx, &apiv1.CreateKeyRequest{
		Parent: "bad-parent",
		Key:    &apiv1.Key{DisplayName: "Key"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// HandleResourceError wraps the parse error; it will be Internal or InvalidArgument
	// depending on the error type. For a non-pgx error, it becomes Internal.
	assert.NotEqual(t, codes.OK, st.Code())
}

func TestUnit_GetKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "my-key",
	}).Return(testDBKey, nil)

	resp, err := srv.GetKey(ctx, &apiv1.GetKeyRequest{
		Name: "organizations/acme/keys/my-key",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/keys/my-key", resp.GetName())
	assert.Equal(t, "My API Key", resp.GetDisplayName())
	assert.Equal(t, "etag-1", resp.GetEtag())
	assert.Equal(t, map[string]string{"env": "prod"}, resp.GetAnnotations())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetKey_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "nonexistent",
	}).Return(db.ApiKey{}, pgx.ErrNoRows)

	_, err := srv.GetKey(ctx, &apiv1.GetKeyRequest{
		Name: "organizations/acme/keys/nonexistent",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestUnit_DeleteKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	deletedKey := testDBKey
	deletedKey.DeleteTime = pgtype.Timestamptz{Time: time.Now(), Valid: true}

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "my-key",
	}).Return(testDBKey, nil)
	mockQ.On("SoftDeleteApiKey", mock.Anything, db.SoftDeleteApiKeyParams{
		ID:        testKeyID,
		DeletedBy: convert.PgUUID(apikeyCallerPivoxUUID),
	}).Return(deletedKey, nil)

	resp, err := srv.DeleteKey(ctx, &apiv1.DeleteKeyRequest{
		Name: "organizations/acme/keys/my-key",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/keys/my-key", resp.GetName())
	assert.NotNil(t, resp.GetDeleteTime())
	mockQ.AssertExpectations(t)
}

func TestUnit_UndeleteKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	undeletedKey := testDBKey
	undeletedKey.DeleteTime = pgtype.Timestamptz{} // cleared

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "my-key",
	}).Return(testDBKey, nil)
	mockQ.On("UndeleteApiKey", mock.Anything, db.UndeleteApiKeyParams{
		ID:        testKeyID,
		UpdatedBy: convert.PgUUID(apikeyCallerPivoxUUID),
	}).Return(undeletedKey, nil)

	resp, err := srv.UndeleteKey(ctx, &apiv1.UndeleteKeyRequest{
		Name: "organizations/acme/keys/my-key",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/keys/my-key", resp.GetName())
	assert.Nil(t, resp.GetDeleteTime())
	mockQ.AssertExpectations(t)
}

func TestUnit_LookupKey_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	mockQ.On("LookupApiKeyByKeyString", mock.Anything, "the-secret-key-string").Return(testDBKey, nil)
	mockQ.On("GetOrganization", mock.Anything, testOrgID).Return(testOrg, nil)

	resp, err := srv.LookupKey(ctx, &apiv1.LookupKeyRequest{
		KeyString: "the-secret-key-string",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme", resp.GetParent())
	assert.Equal(t, "organizations/acme/keys/my-key", resp.GetName())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateKey_WithFieldMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	updatedKey := testDBKey
	updatedKey.DisplayName = "Updated Name"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "my-key",
	}).Return(testDBKey, nil)
	mockQ.On("UpdateApiKey", mock.Anything, mock.MatchedBy(func(p db.UpdateApiKeyParams) bool {
		return p.ID == testKeyID &&
			p.DisplayName.Valid &&
			p.DisplayName.String == "Updated Name" &&
			p.Annotations == nil // not in mask, should not be set
	})).Return(updatedKey, nil)

	resp, err := srv.UpdateKey(ctx, &apiv1.UpdateKeyRequest{
		Key: &apiv1.Key{
			Name:        "organizations/acme/keys/my-key",
			DisplayName: "Updated Name",
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name"},
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "Updated Name", resp.GetDisplayName())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetKeyString_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "my-key",
	}).Return(testDBKey, nil)

	resp, err := srv.GetKeyString(ctx, &apiv1.GetKeyStringRequest{
		Name: "organizations/acme/keys/my-key",
	})

	require.NoError(t, err)
	assert.Equal(t, testDBKey.KeyString, resp.GetKeyString())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateKey_NoMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := &ApiKeysServer{queries: mockQ}
	ctx := callerCtx()

	updatedKey := testDBKey
	updatedKey.DisplayName = "New Name"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
		OrgID: testOrgID,
		KeyID: "my-key",
	}).Return(testDBKey, nil)
	mockQ.On("UpdateApiKey", mock.Anything, mock.MatchedBy(func(p db.UpdateApiKeyParams) bool {
		return p.ID == testKeyID &&
			p.DisplayName.Valid &&
			p.DisplayName.String == "New Name" &&
			p.Annotations != nil // annotations set because non-nil in request
	})).Return(updatedKey, nil)

	resp, err := srv.UpdateKey(ctx, &apiv1.UpdateKeyRequest{
		Key: &apiv1.Key{
			Name:        "organizations/acme/keys/my-key",
			DisplayName: "New Name",
			Annotations: map[string]string{"env": "staging"},
		},
		// No UpdateMask — all fields updated
	})

	require.NoError(t, err)
	assert.Equal(t, "New Name", resp.GetDisplayName())
	mockQ.AssertExpectations(t)
}

func TestUnit_GenerateKeyString(t *testing.T) {
	key := generateKeyString()
	assert.Len(t, key, 39, "generated key should be 39 characters")

	for _, c := range key {
		assert.True(t, unicode.IsLetter(c) || unicode.IsDigit(c),
			"character %q should be alphanumeric", c)
	}

	// Verify uniqueness (two keys should differ)
	key2 := generateKeyString()
	assert.NotEqual(t, key, key2, "two generated keys should be different")
}

// --- CreateKey error paths ---

func TestUnit_CreateKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.CreateKeyRequest
		wantCode codes.Code
	}{
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req: &apiv1.CreateKeyRequest{
				Parent: "organizations/acme",
				Key:    &apiv1.Key{DisplayName: "Key"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "db error on CreateApiKey",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("CreateApiKey", mock.Anything, mock.Anything).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req: &apiv1.CreateKeyRequest{
				Parent: "organizations/acme",
				Key:    &apiv1.Key{DisplayName: "Key"},
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.CreateKey(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- GetKeyString error paths ---

func TestUnit_GetKeyString_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.GetKeyStringRequest
		wantCode codes.Code
	}{
		{
			name:     "invalid name",
			setup:    func(mockQ *mocks.MockQuerier) {},
			req:      &apiv1.GetKeyStringRequest{Name: "bad-name"},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.GetKeyStringRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
		{
			name: "key not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req:      &apiv1.GetKeyStringRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.GetKeyString(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- UpdateKey error paths ---

func TestUnit_UpdateKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.UpdateKeyRequest
		wantCode codes.Code
	}{
		{
			name:  "invalid key name",
			setup: func(mockQ *mocks.MockQuerier) {},
			req: &apiv1.UpdateKeyRequest{
				Key: &apiv1.Key{Name: "bad-name"},
			},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateKeyRequest{
				Key: &apiv1.Key{Name: "organizations/acme/keys/my-key"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "key not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateKeyRequest{
				Key: &apiv1.Key{Name: "organizations/acme/keys/my-key"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "db error on UpdateApiKey",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(testDBKey, nil)
				mockQ.On("UpdateApiKey", mock.Anything, mock.Anything).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateKeyRequest{
				Key: &apiv1.Key{
					Name:        "organizations/acme/keys/my-key",
					DisplayName: "New Name",
				},
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.UpdateKey(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- DeleteKey error paths ---

func TestUnit_DeleteKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.DeleteKeyRequest
		wantCode codes.Code
	}{
		{
			name:     "invalid key name",
			setup:    func(mockQ *mocks.MockQuerier) {},
			req:      &apiv1.DeleteKeyRequest{Name: "bad-name"},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
		{
			name: "key not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
		{
			name: "db error on SoftDeleteApiKey",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(testDBKey, nil)
				mockQ.On("SoftDeleteApiKey", mock.Anything, db.SoftDeleteApiKeyParams{
					ID:        testKeyID,
					DeletedBy: convert.PgUUID(apikeyCallerPivoxUUID),
				}).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.DeleteKey(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- UndeleteKey error paths ---

func TestUnit_UndeleteKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.UndeleteKeyRequest
		wantCode codes.Code
	}{
		{
			name:     "invalid key name",
			setup:    func(mockQ *mocks.MockQuerier) {},
			req:      &apiv1.UndeleteKeyRequest{Name: "bad-name"},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
		{
			name: "key not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
		{
			name: "db error on UndeleteApiKey",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
				mockQ.On("GetApiKeyByOrgAndKeyID", mock.Anything, db.GetApiKeyByOrgAndKeyIDParams{
					OrgID: testOrgID,
					KeyID: "my-key",
				}).Return(testDBKey, nil)
				mockQ.On("UndeleteApiKey", mock.Anything, db.UndeleteApiKeyParams{
					ID:        testKeyID,
					UpdatedBy: convert.PgUUID(apikeyCallerPivoxUUID),
				}).Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.UndeleteKey(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- LookupKey error paths ---

func TestUnit_LookupKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.LookupKeyRequest
		wantCode codes.Code
	}{
		{
			name: "key string not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("LookupApiKeyByKeyString", mock.Anything, "unknown-key").Return(db.ApiKey{}, pgx.ErrNoRows)
			},
			req:      &apiv1.LookupKeyRequest{KeyString: "unknown-key"},
			wantCode: codes.NotFound,
		},
		{
			name: "org not found after key lookup",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("LookupApiKeyByKeyString", mock.Anything, "the-secret-key-string").Return(testDBKey, nil)
				mockQ.On("GetOrganization", mock.Anything, testOrgID).Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.LookupKeyRequest{KeyString: "the-secret-key-string"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.LookupKey(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- ListKeys error paths ---

func TestUnit_ListKeys_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.ListKeysRequest
		wantCode codes.Code
	}{
		{
			name:     "invalid parent",
			setup:    func(mockQ *mocks.MockQuerier) {},
			req:      &apiv1.ListKeysRequest{Parent: "bad-parent"},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.ListKeysRequest{Parent: "organizations/acme"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.ListKeys(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

// --- GetKey error paths ---

func TestUnit_GetKey_ErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(mockQ *mocks.MockQuerier)
		req      *apiv1.GetKeyRequest
		wantCode codes.Code
	}{
		{
			name:     "invalid key name",
			setup:    func(mockQ *mocks.MockQuerier) {},
			req:      &apiv1.GetKeyRequest{Name: "bad-name"},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setup: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.GetKeyRequest{Name: "organizations/acme/keys/my-key"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			srv := &ApiKeysServer{queries: mockQ}
			tc.setup(mockQ)

			_, err := srv.GetKey(callerCtx(), tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}
