package spaces

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

var (
	testOrgID  = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testProjID = uuid.MustParse("0192a000-0003-7000-8000-000000000001")
	testOrg    = db.Organization{
		ID:          testOrgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testDBSpace = db.Space{
		ID:          testProjID,
		OrgID:       testOrgID,
		Name:        "my-space",
		DisplayName: "My Space",
		Labels:      json.RawMessage(`{"env":"prod"}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-proj",
		Revision:    1,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

// memberTestCtx returns a context pre-populated with the same
// ResolvedOrg + ResolvedSpace the permission interceptor would attach
// in production. Member handlers now read these from context instead
// of issuing fresh slug-resolution lookups, so unit tests must seed
// them. spaceID is parameterized because most member tests pin a
// specific id to assert the right downstream queries fired.
func memberTestCtx(spaceID uuid.UUID) context.Context {
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   testOrgID,
		Slug: testOrg.Name,
		Row:  testOrg,
	})
	ctx = server.WithResolvedSpaceForTest(ctx, &server.ResolvedSpace{
		ID:   spaceID,
		Slug: "news",
	})
	return ctx
}

func TestUnit_GetSpace_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)

	resp, err := srv.GetSpace(ctx, &apiv1.GetSpaceRequest{
		Name: "organizations/acme/spaces/my-space",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/spaces/my-space", resp.GetName())
	assert.Equal(t, "My Space", resp.GetDisplayName())
	assert.Equal(t, apiv1.Space_ACTIVE, resp.GetState())
	assert.Equal(t, "etag-proj", resp.GetEtag())
	assert.Equal(t, map[string]string{"env": "prod"}, resp.GetLabels())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetSpace_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	_, err := srv.GetSpace(ctx, &apiv1.GetSpaceRequest{
		Name: "invalid/format",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
}

func TestUnit_GetSpace_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "nonexistent",
	}).Return(db.Space{}, pgx.ErrNoRows)

	_, err := srv.GetSpace(ctx, &apiv1.GetSpaceRequest{
		Name: "organizations/acme/spaces/nonexistent",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestUnit_CreateSpace_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateSpace", mock.Anything, mock.MatchedBy(func(p db.CreateSpaceParams) bool {
		return p.OrgID == testOrgID && p.Name == "new-proj" && p.DisplayName == "New Space"
	})).Return(db.Space{
		ID:          uuid.New(),
		OrgID:       testOrgID,
		Name:        "new-proj",
		DisplayName: "New Space",
		Labels:      json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}, nil)

	resp, err := srv.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/acme",
		Space:   &apiv1.Space{DisplayName: "New Space"},
		SpaceId: "new-proj",
	})

	require.NoError(t, err)
	// CreateSpace returns a longrunning.Operation (DoneOperation wrapping the space)
	assert.True(t, resp.GetDone(), "operation should be done (synchronous)")
	assert.NotNil(t, resp.GetResponse(), "operation should contain a response")
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateSpace_InvalidParent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	_, err := srv.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent: "bad/parent/format",
		Space:  &apiv1.Space{DisplayName: "Test"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_UpdateSpace_WithFieldMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	updatedSpace := testDBSpace
	updatedSpace.DisplayName = "Updated Name"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)
	mockQ.On("UpdateSpace", mock.Anything, mock.MatchedBy(func(p db.UpdateSpaceParams) bool {
		return p.ID == testProjID &&
			p.DisplayName.Valid &&
			p.DisplayName.String == "Updated Name" &&
			p.Labels == nil // not in mask
	})).Return(updatedSpace, nil)

	resp, err := srv.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:        "organizations/acme/spaces/my-space",
			DisplayName: "Updated Name",
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteSpace_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	deletedSpace := testDBSpace
	deletedSpace.DeleteTime = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	deletedSpace.State = db.ResourceStateDELETEREQUESTED

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)
	mockQ.On("SoftDeleteSpace", mock.Anything, db.SoftDeleteSpaceParams{
		ID:        testProjID,
		DeletedBy: "",
	}).Return(deletedSpace, nil)

	resp, err := srv.DeleteSpace(ctx, &apiv1.DeleteSpaceRequest{
		Name: "organizations/acme/spaces/my-space",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateSpace_NoMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	updatedSpace := testDBSpace
	updatedSpace.DisplayName = "Updated Name"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)
	mockQ.On("UpdateSpace", mock.Anything, mock.MatchedBy(func(p db.UpdateSpaceParams) bool {
		return p.ID == testProjID &&
			p.DisplayName.Valid &&
			p.DisplayName.String == "Updated Name" &&
			p.Labels != nil // labels preserved from existing when nil in request
	})).Return(updatedSpace, nil)

	resp, err := srv.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:        "organizations/acme/spaces/my-space",
			DisplayName: "Updated Name",
		},
		// No UpdateMask — full update
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UndeleteSpace_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	undeletedSpace := testDBSpace
	undeletedSpace.DeleteTime = pgtype.Timestamptz{} // cleared
	undeletedSpace.State = db.ResourceStateACTIVE

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)
	mockQ.On("UndeleteSpace", mock.Anything, db.UndeleteSpaceParams{
		ID:        testProjID,
		UpdatedBy: "",
	}).Return(undeletedSpace, nil)

	resp, err := srv.UndeleteSpace(ctx, &apiv1.UndeleteSpaceRequest{
		Name: "organizations/acme/spaces/my-space",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateSpace_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.UpdateSpaceRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid space name format",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req: &apiv1.UpdateSpaceRequest{
				Space: &apiv1.Space{Name: "bad/format"},
			},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateSpaceRequest{
				Space: &apiv1.Space{Name: "organizations/acme/spaces/my-space"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "space not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
					OrgID: testOrgID,
					Name:  "missing-proj",
				}).Return(db.Space{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateSpaceRequest{
				Space: &apiv1.Space{Name: "organizations/acme/spaces/missing-proj"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "update query fails",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
					OrgID: testOrgID,
					Name:  "my-space",
				}).Return(testDBSpace, nil)
				mockQ.On("UpdateSpace", mock.Anything, mock.MatchedBy(func(p db.UpdateSpaceParams) bool {
					return p.ID == testProjID
				})).Return(db.Space{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateSpaceRequest{
				Space: &apiv1.Space{
					Name:        "organizations/acme/spaces/my-space",
					DisplayName: "New Name",
				},
			},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)

			_, err := srv.UpdateSpace(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_DeleteSpace_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.DeleteSpaceRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid space name format",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req:        &apiv1.DeleteSpaceRequest{Name: "bad/format"},
			wantCode:   codes.Internal,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteSpaceRequest{Name: "organizations/acme/spaces/my-space"},
			wantCode: codes.NotFound,
		},
		{
			name: "space not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
					OrgID: testOrgID,
					Name:  "missing-proj",
				}).Return(db.Space{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteSpaceRequest{Name: "organizations/acme/spaces/missing-proj"},
			wantCode: codes.NotFound,
		},
		{
			name: "soft delete query fails",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
					OrgID: testOrgID,
					Name:  "my-space",
				}).Return(testDBSpace, nil)
				mockQ.On("SoftDeleteSpace", mock.Anything, db.SoftDeleteSpaceParams{
					ID:        testProjID,
					DeletedBy: "",
				}).Return(db.Space{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteSpaceRequest{Name: "organizations/acme/spaces/my-space"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)

			_, err := srv.DeleteSpace(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_UndeleteSpace_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.UndeleteSpaceRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid space name format",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req:        &apiv1.UndeleteSpaceRequest{Name: "bad/format"},
			wantCode:   codes.Internal,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteSpaceRequest{Name: "organizations/acme/spaces/my-space"},
			wantCode: codes.NotFound,
		},
		{
			name: "space not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
					OrgID: testOrgID,
					Name:  "missing-proj",
				}).Return(db.Space{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteSpaceRequest{Name: "organizations/acme/spaces/missing-proj"},
			wantCode: codes.NotFound,
		},
		{
			name: "undelete query fails",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
					OrgID: testOrgID,
					Name:  "my-space",
				}).Return(testDBSpace, nil)
				mockQ.On("UndeleteSpace", mock.Anything, db.UndeleteSpaceParams{
					ID:        testProjID,
					UpdatedBy: "",
				}).Return(db.Space{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteSpaceRequest{Name: "organizations/acme/spaces/my-space"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)

			_, err := srv.UndeleteSpace(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_ListSpaces_InvalidParent(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.ListSpacesRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid parent prefix",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req:        &apiv1.ListSpacesRequest{Parent: "badprefix/acme"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.ListSpacesRequest{Parent: "organizations/acme"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)

			_, err := srv.ListSpaces(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_UpdateSpace_LabelsMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	updatedSpace := testDBSpace
	updatedSpace.Labels = json.RawMessage(`{"tier":"gold"}`)

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)
	mockQ.On("UpdateSpace", mock.Anything, mock.MatchedBy(func(p db.UpdateSpaceParams) bool {
		return p.ID == testProjID && p.Labels != nil && !p.DisplayName.Valid
	})).Return(updatedSpace, nil)

	resp, err := srv.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:   "organizations/acme/spaces/my-space",
			Labels: map[string]string{"tier": "gold"},
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"labels"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateSpace_NoMaskWithLabels(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	updatedSpace := testDBSpace
	updatedSpace.Labels = json.RawMessage(`{"region":"us"}`)

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetSpaceByName", mock.Anything, db.GetSpaceByNameParams{
		OrgID: testOrgID,
		Name:  "my-space",
	}).Return(testDBSpace, nil)
	mockQ.On("UpdateSpace", mock.Anything, mock.MatchedBy(func(p db.UpdateSpaceParams) bool {
		return p.ID == testProjID && p.Labels != nil
	})).Return(updatedSpace, nil)

	resp, err := srv.UpdateSpace(ctx, &apiv1.UpdateSpaceRequest{
		Space: &apiv1.Space{
			Name:   "organizations/acme/spaces/my-space",
			Labels: map[string]string{"region": "us"},
		},
		// No UpdateMask — full update with explicit labels
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetSpace_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "unknown-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetSpace(ctx, &apiv1.GetSpaceRequest{
		Name: "organizations/unknown-org/spaces/my-space",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateSpace_AutoGeneratedID(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateSpace", mock.Anything, mock.MatchedBy(func(p db.CreateSpaceParams) bool {
		return p.OrgID == testOrgID && p.Name != "" && len(p.Name) == 8
	})).Return(db.Space{
		ID:          uuid.New(),
		OrgID:       testOrgID,
		Name:        "abcd1234",
		DisplayName: "Auto ID Space",
		Labels:      json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}, nil)

	resp, err := srv.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent: "organizations/acme",
		Space:  &apiv1.Space{DisplayName: "Auto ID Space"},
		// No SpaceId — server auto-generates one
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateSpace_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewSpacesServer(nil, nil, mockQ, nil, nil, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateSpace", mock.Anything, mock.MatchedBy(func(p db.CreateSpaceParams) bool {
		return p.OrgID == testOrgID
	})).Return(db.Space{}, pgx.ErrNoRows)

	_, err := srv.CreateSpace(ctx, &apiv1.CreateSpaceRequest{
		Parent:  "organizations/acme",
		Space:   &apiv1.Space{DisplayName: "Test"},
		SpaceId: "dup-proj",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
	mockQ.AssertExpectations(t)
}
