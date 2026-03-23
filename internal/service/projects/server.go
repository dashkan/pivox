package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	iampb "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/iam/v1"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox-server/internal/apierr"
	"github.com/dashkan/pivox-server/internal/convert"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/filter"
	"github.com/dashkan/pivox-server/internal/iam"
	"github.com/dashkan/pivox-server/internal/lro"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox-server/internal/resource"
)

type ProjectsServer struct {
	apiv1.UnimplementedProjectsServer
	db      db.DBTX
	queries *db.Queries
	iam     *iam.Helper
	filter  *filter.ResourceFilter
}

func NewProjectsServer(pool db.DBTX, queries *db.Queries, iam *iam.Helper) *ProjectsServer {
	return &ProjectsServer{
		db:      pool,
		queries: queries,
		iam:     iam,
		filter:  filter.ProjectFilter(),
	}
}

// parseProjectName parses "organizations/{org}/projects/{project}" and returns (orgName, projectName).
func parseProjectName(name string) (string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "projects" {
		return "", "", fmt.Errorf("invalid project name %q: expected organizations/*/projects/*", name)
	}
	return parts[1], parts[3], nil
}

func (s *ProjectsServer) GetProject(ctx context.Context, req *apiv1.GetProjectRequest) (*apiv1.Project, error) {
	orgName, projectName, err := parseProjectName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	project, err := s.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: projectName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}
	return convert.ProjectToProto(project, orgName), nil
}

func (s *ProjectsServer) ListProjects(ctx context.Context, req *apiv1.ListProjectsRequest) (*apiv1.ListProjectsResponse, error) {
	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}

	// Extract org name from parent for proto conversion.
	orgName, _ := resource.ParseSegment(req.GetParent())

	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:      req.GetFilter(),
		ParentID:    orgID.String(),
		OrderBy:     req.GetOrderBy(),
		PageSize:    req.GetPageSize(),
		Cursor:      req.GetPageToken(),
		ShowDeleted: req.GetShowDeleted(),
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	results, err := filter.ScanProjects(rows)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var nextPageToken string
	if int32(len(results)) > pageSize {
		nextPageToken = results[pageSize].ID.String()
		results = results[:pageSize]
	}

	projects := make([]*apiv1.Project, 0, len(results))
	for _, r := range results {
		projects = append(projects, convert.ProjectToProto(r, orgName))
	}

	return &apiv1.ListProjectsResponse{
		Projects:      projects,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *ProjectsServer) CreateProject(ctx context.Context, req *apiv1.CreateProjectRequest) (*longrunningpb.Operation, error) {
	project := req.GetProject()

	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}
	orgName, _ := resource.ParseSegment(req.GetParent())

	projectName := req.GetProjectId()
	if projectName == "" {
		projectName = uuid.New().String()[:8]
	}

	var labelsJSON json.RawMessage
	if labels := project.GetLabels(); labels != nil {
		labelsJSON, _ = json.Marshal(labels)
	} else {
		labelsJSON = json.RawMessage("{}")
	}

	result, err := s.queries.CreateProject(ctx, db.CreateProjectParams{
		ID:          uuid.New(),
		OrgID:       orgID,
		Name:        projectName,
		DisplayName: project.GetDisplayName(),
		Labels:      labelsJSON,
		CreatedBy:   "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", "")
	}

	return lro.DoneOperation(convert.ProjectToProto(result, orgName))
}

func (s *ProjectsServer) UpdateProject(ctx context.Context, req *apiv1.UpdateProjectRequest) (*longrunningpb.Operation, error) {
	project := req.GetProject()
	orgName, projectName, err := parseProjectName(project.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", project.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: projectName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", project.GetName())
	}

	updateParams := db.UpdateProjectParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: project.GetDisplayName(), Valid: true}
			case "labels":
				labelsJSON, err := json.Marshal(project.GetLabels())
				if err != nil {
					return nil, apierr.Internal("failed to marshal labels")
				}
				updateParams.Labels = labelsJSON
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: project.GetDisplayName(), Valid: true}
		if labels := project.GetLabels(); labels != nil {
			labelsJSON, _ := json.Marshal(labels)
			updateParams.Labels = labelsJSON
		} else {
			updateParams.Labels = existing.Labels
		}
	}

	result, err := s.queries.UpdateProject(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", project.GetName())
	}

	return lro.DoneOperation(convert.ProjectToProto(result, orgName))
}

func (s *ProjectsServer) DeleteProject(ctx context.Context, req *apiv1.DeleteProjectRequest) (*longrunningpb.Operation, error) {
	orgName, projectName, err := parseProjectName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: projectName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}

	result, err := s.queries.SoftDeleteProject(ctx, db.SoftDeleteProjectParams{
		ID:        existing.ID,
		DeletedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}
	return lro.DoneOperation(convert.ProjectToProto(result, orgName))
}

func (s *ProjectsServer) UndeleteProject(ctx context.Context, req *apiv1.UndeleteProjectRequest) (*longrunningpb.Operation, error) {
	orgName, projectName, err := parseProjectName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: projectName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}

	result, err := s.queries.UndeleteProject(ctx, db.UndeleteProjectParams{
		ID:        existing.ID,
		UpdatedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetName())
	}
	return lro.DoneOperation(convert.ProjectToProto(result, orgName))
}

func (s *ProjectsServer) GetIamPolicy(ctx context.Context, req *iampb.GetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.GetIamPolicy(ctx, req)
}

func (s *ProjectsServer) SetIamPolicy(ctx context.Context, req *iampb.SetIamPolicyRequest) (*iampb.Policy, error) {
	return s.iam.SetIamPolicy(ctx, req)
}

func (s *ProjectsServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	return s.iam.TestIamPermissions(ctx, req)
}
