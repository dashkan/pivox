package projects

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
	"github.com/dashkan/pivox/internal/iam"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
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
	testDBProject = db.Project{
		ID:          testProjID,
		OrgID:       testOrgID,
		Name:        "my-project",
		DisplayName: "My Project",
		Labels:      json.RawMessage(`{"env":"prod"}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-proj",
		Revision:    1,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

func TestUnit_GetProject_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)

	resp, err := srv.GetProject(ctx, &apiv1.GetProjectRequest{
		Name: "organizations/acme/projects/my-project",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/projects/my-project", resp.GetName())
	assert.Equal(t, "My Project", resp.GetDisplayName())
	assert.Equal(t, apiv1.Project_ACTIVE, resp.GetState())
	assert.Equal(t, "etag-proj", resp.GetEtag())
	assert.Equal(t, map[string]string{"env": "prod"}, resp.GetLabels())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetProject_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	_, err := srv.GetProject(ctx, &apiv1.GetProjectRequest{
		Name: "invalid/format",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
}

func TestUnit_GetProject_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "nonexistent",
	}).Return(db.Project{}, pgx.ErrNoRows)

	_, err := srv.GetProject(ctx, &apiv1.GetProjectRequest{
		Name: "organizations/acme/projects/nonexistent",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestUnit_CreateProject_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateProject", mock.Anything, mock.MatchedBy(func(p db.CreateProjectParams) bool {
		return p.OrgID == testOrgID && p.Name == "new-proj" && p.DisplayName == "New Project"
	})).Return(db.Project{
		ID:          uuid.New(),
		OrgID:       testOrgID,
		Name:        "new-proj",
		DisplayName: "New Project",
		Labels:      json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}, nil)

	resp, err := srv.CreateProject(ctx, &apiv1.CreateProjectRequest{
		Parent:    "organizations/acme",
		Project:   &apiv1.Project{DisplayName: "New Project"},
		ProjectId: "new-proj",
	})

	require.NoError(t, err)
	// CreateProject returns a longrunning.Operation (DoneOperation wrapping the project)
	assert.True(t, resp.GetDone(), "operation should be done (synchronous)")
	assert.NotNil(t, resp.GetResponse(), "operation should contain a response")
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateProject_InvalidParent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	_, err := srv.CreateProject(ctx, &apiv1.CreateProjectRequest{
		Parent:  "bad/parent/format",
		Project: &apiv1.Project{DisplayName: "Test"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_UpdateProject_WithFieldMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	updatedProject := testDBProject
	updatedProject.DisplayName = "Updated Name"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("UpdateProject", mock.Anything, mock.MatchedBy(func(p db.UpdateProjectParams) bool {
		return p.ID == testProjID &&
			p.DisplayName.Valid &&
			p.DisplayName.String == "Updated Name" &&
			p.Labels == nil // not in mask
	})).Return(updatedProject, nil)

	resp, err := srv.UpdateProject(ctx, &apiv1.UpdateProjectRequest{
		Project: &apiv1.Project{
			Name:        "organizations/acme/projects/my-project",
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

func TestUnit_DeleteProject_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	deletedProject := testDBProject
	deletedProject.DeleteTime = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	deletedProject.State = db.ResourceStateDELETEREQUESTED

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("SoftDeleteProject", mock.Anything, db.SoftDeleteProjectParams{
		ID:        testProjID,
		DeletedBy: "",
	}).Return(deletedProject, nil)

	resp, err := srv.DeleteProject(ctx, &apiv1.DeleteProjectRequest{
		Name: "organizations/acme/projects/my-project",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateProject_NoMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	updatedProject := testDBProject
	updatedProject.DisplayName = "Updated Name"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("UpdateProject", mock.Anything, mock.MatchedBy(func(p db.UpdateProjectParams) bool {
		return p.ID == testProjID &&
			p.DisplayName.Valid &&
			p.DisplayName.String == "Updated Name" &&
			p.Labels != nil // labels preserved from existing when nil in request
	})).Return(updatedProject, nil)

	resp, err := srv.UpdateProject(ctx, &apiv1.UpdateProjectRequest{
		Project: &apiv1.Project{
			Name:        "organizations/acme/projects/my-project",
			DisplayName: "Updated Name",
		},
		// No UpdateMask — full update
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetIamPolicy(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewProjectsServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("GetIamPolicy", mock.Anything, testProjID).Return(db.IamPolicy{}, pgx.ErrNoRows)

	resp, err := srv.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Name: "organizations/acme/projects/my-project",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Empty(t, resp.GetBindings())
	mockQ.AssertExpectations(t)
}

func TestUnit_SetIamPolicy(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewProjectsServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == testProjID && p.ResourceType == "organizations"
	})).Return(db.IamPolicy{
		ResourceID: testProjID,
		Policy:     json.RawMessage(`{}`),
		Etag:       "new-etag",
	}, nil)

	resp, err := srv.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/acme/projects/my-project",
		Policy:   &iampb.Policy{},
	})

	require.NoError(t, err)
	assert.Equal(t, "new-etag", resp.GetEtag())
	mockQ.AssertExpectations(t)
}

func TestUnit_TestIamPermissions(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	srv := NewProjectsServer(nil, mockQ, iamHelper)
	ctx := context.Background()

	perms := []string{"pivox.projects.get", "pivox.projects.delete"}
	resp, err := srv.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    "organizations/acme/projects/my-project",
		Permissions: perms,
	})

	require.NoError(t, err)
	assert.Equal(t, perms, resp.GetPermissions())
}

func TestUnit_UndeleteProject_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	undeletedProject := testDBProject
	undeletedProject.DeleteTime = pgtype.Timestamptz{} // cleared
	undeletedProject.State = db.ResourceStateACTIVE

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("UndeleteProject", mock.Anything, db.UndeleteProjectParams{
		ID:        testProjID,
		UpdatedBy: "",
	}).Return(undeletedProject, nil)

	resp, err := srv.UndeleteProject(ctx, &apiv1.UndeleteProjectRequest{
		Name: "organizations/acme/projects/my-project",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateProject_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		setupMocks   func(*mocks.MockQuerier)
		req          *apiv1.UpdateProjectRequest
		wantCode     codes.Code
	}{
		{
			name:       "invalid project name format",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req: &apiv1.UpdateProjectRequest{
				Project: &apiv1.Project{Name: "bad/format"},
			},
			wantCode: codes.Internal,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateProjectRequest{
				Project: &apiv1.Project{Name: "organizations/acme/projects/my-project"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "project not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
					OrgID: testOrgID,
					Name:  "missing-proj",
				}).Return(db.Project{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateProjectRequest{
				Project: &apiv1.Project{Name: "organizations/acme/projects/missing-proj"},
			},
			wantCode: codes.NotFound,
		},
		{
			name: "update query fails",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
					OrgID: testOrgID,
					Name:  "my-project",
				}).Return(testDBProject, nil)
				mockQ.On("UpdateProject", mock.Anything, mock.MatchedBy(func(p db.UpdateProjectParams) bool {
					return p.ID == testProjID
				})).Return(db.Project{}, pgx.ErrNoRows)
			},
			req: &apiv1.UpdateProjectRequest{
				Project: &apiv1.Project{
					Name:        "organizations/acme/projects/my-project",
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
			srv := NewProjectsServer(nil, mockQ, nil)

			_, err := srv.UpdateProject(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_DeleteProject_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.DeleteProjectRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid project name format",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req:        &apiv1.DeleteProjectRequest{Name: "bad/format"},
			wantCode:   codes.Internal,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteProjectRequest{Name: "organizations/acme/projects/my-project"},
			wantCode: codes.NotFound,
		},
		{
			name: "project not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
					OrgID: testOrgID,
					Name:  "missing-proj",
				}).Return(db.Project{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteProjectRequest{Name: "organizations/acme/projects/missing-proj"},
			wantCode: codes.NotFound,
		},
		{
			name: "soft delete query fails",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
					OrgID: testOrgID,
					Name:  "my-project",
				}).Return(testDBProject, nil)
				mockQ.On("SoftDeleteProject", mock.Anything, db.SoftDeleteProjectParams{
					ID:        testProjID,
					DeletedBy: "",
				}).Return(db.Project{}, pgx.ErrNoRows)
			},
			req:      &apiv1.DeleteProjectRequest{Name: "organizations/acme/projects/my-project"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewProjectsServer(nil, mockQ, nil)

			_, err := srv.DeleteProject(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_UndeleteProject_ErrorPaths(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.UndeleteProjectRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid project name format",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req:        &apiv1.UndeleteProjectRequest{Name: "bad/format"},
			wantCode:   codes.Internal,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteProjectRequest{Name: "organizations/acme/projects/my-project"},
			wantCode: codes.NotFound,
		},
		{
			name: "project not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
					OrgID: testOrgID,
					Name:  "missing-proj",
				}).Return(db.Project{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteProjectRequest{Name: "organizations/acme/projects/missing-proj"},
			wantCode: codes.NotFound,
		},
		{
			name: "undelete query fails",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(testOrg, nil)
				mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
					OrgID: testOrgID,
					Name:  "my-project",
				}).Return(testDBProject, nil)
				mockQ.On("UndeleteProject", mock.Anything, db.UndeleteProjectParams{
					ID:        testProjID,
					UpdatedBy: "",
				}).Return(db.Project{}, pgx.ErrNoRows)
			},
			req:      &apiv1.UndeleteProjectRequest{Name: "organizations/acme/projects/my-project"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewProjectsServer(nil, mockQ, nil)

			_, err := srv.UndeleteProject(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_ListProjects_InvalidParent(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name       string
		setupMocks func(*mocks.MockQuerier)
		req        *apiv1.ListProjectsRequest
		wantCode   codes.Code
	}{
		{
			name:       "invalid parent prefix",
			setupMocks: func(mockQ *mocks.MockQuerier) {},
			req:        &apiv1.ListProjectsRequest{Parent: "badprefix/acme"},
			wantCode:   codes.InvalidArgument,
		},
		{
			name: "org not found",
			setupMocks: func(mockQ *mocks.MockQuerier) {
				mockQ.On("GetOrganizationByName", mock.Anything, "acme").
					Return(db.Organization{}, pgx.ErrNoRows)
			},
			req:      &apiv1.ListProjectsRequest{Parent: "organizations/acme"},
			wantCode: codes.NotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mockQ := new(mocks.MockQuerier)
			tc.setupMocks(mockQ)
			srv := NewProjectsServer(nil, mockQ, nil)

			_, err := srv.ListProjects(ctx, tc.req)

			require.Error(t, err)
			st, ok := status.FromError(err)
			require.True(t, ok)
			assert.Equal(t, tc.wantCode, st.Code())
			mockQ.AssertExpectations(t)
		})
	}
}

func TestUnit_UpdateProject_LabelsMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	updatedProject := testDBProject
	updatedProject.Labels = json.RawMessage(`{"tier":"gold"}`)

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("UpdateProject", mock.Anything, mock.MatchedBy(func(p db.UpdateProjectParams) bool {
		return p.ID == testProjID && p.Labels != nil && !p.DisplayName.Valid
	})).Return(updatedProject, nil)

	resp, err := srv.UpdateProject(ctx, &apiv1.UpdateProjectRequest{
		Project: &apiv1.Project{
			Name:   "organizations/acme/projects/my-project",
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

func TestUnit_UpdateProject_NoMaskWithLabels(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	updatedProject := testDBProject
	updatedProject.Labels = json.RawMessage(`{"region":"us"}`)

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("GetProjectByName", mock.Anything, db.GetProjectByNameParams{
		OrgID: testOrgID,
		Name:  "my-project",
	}).Return(testDBProject, nil)
	mockQ.On("UpdateProject", mock.Anything, mock.MatchedBy(func(p db.UpdateProjectParams) bool {
		return p.ID == testProjID && p.Labels != nil
	})).Return(updatedProject, nil)

	resp, err := srv.UpdateProject(ctx, &apiv1.UpdateProjectRequest{
		Project: &apiv1.Project{
			Name:   "organizations/acme/projects/my-project",
			Labels: map[string]string{"region": "us"},
		},
		// No UpdateMask — full update with explicit labels
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetProject_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "unknown-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetProject(ctx, &apiv1.GetProjectRequest{
		Name: "organizations/unknown-org/projects/my-project",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateProject_AutoGeneratedID(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateProject", mock.Anything, mock.MatchedBy(func(p db.CreateProjectParams) bool {
		return p.OrgID == testOrgID && p.Name != "" && len(p.Name) == 8
	})).Return(db.Project{
		ID:          uuid.New(),
		OrgID:       testOrgID,
		Name:        "abcd1234",
		DisplayName: "Auto ID Project",
		Labels:      json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Now(),
		UpdateTime:  time.Now(),
	}, nil)

	resp, err := srv.CreateProject(ctx, &apiv1.CreateProjectRequest{
		Parent:  "organizations/acme",
		Project: &apiv1.Project{DisplayName: "Auto ID Project"},
		// No ProjectId — server auto-generates one
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateProject_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := NewProjectsServer(nil, mockQ, nil)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("CreateProject", mock.Anything, mock.MatchedBy(func(p db.CreateProjectParams) bool {
		return p.OrgID == testOrgID
	})).Return(db.Project{}, pgx.ErrNoRows)

	_, err := srv.CreateProject(ctx, &apiv1.CreateProjectRequest{
		Parent:    "organizations/acme",
		Project:   &apiv1.Project{DisplayName: "Test"},
		ProjectId: "dup-proj",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())
	mockQ.AssertExpectations(t)
}
