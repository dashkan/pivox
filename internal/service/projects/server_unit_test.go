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
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
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
