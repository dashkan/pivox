package assets

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
)

type AssetsServer struct {
	assetsv1.UnimplementedAssetsServer
	db      db.DBTX
	queries *db.Queries
}

func NewAssetsServer(pool db.DBTX, queries *db.Queries) *AssetsServer {
	return &AssetsServer{
		db:      pool,
		queries: queries,
	}
}

// parseAssetName parses "organizations/{org}/projects/{project}/assets/{asset}".
func parseAssetName(name string) (orgName, projectName, assetName string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "projects" || parts[4] != "assets" {
		return "", "", "", fmt.Errorf("invalid asset name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseAssetParent parses "organizations/{org}/projects/{project}".
func parseAssetParent(parent string) (orgName, projectName string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "projects" {
		return "", "", fmt.Errorf("invalid parent %q", parent)
	}
	return parts[1], parts[3], nil
}

// resolveProject resolves org name + project name to project UUID.
func (s *AssetsServer) resolveProject(ctx context.Context, orgName, projectName string) (uuid.UUID, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	project, err := s.queries.GetProjectByName(ctx, db.GetProjectByNameParams{OrgID: org.ID, Name: projectName})
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Project", fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName))
	}
	return project.ID, nil
}

func (s *AssetsServer) GetAsset(ctx context.Context, req *assetsv1.GetAssetRequest) (*assetsv1.Asset, error) {
	orgName, projectName, assetName, err := parseAssetName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}
	asset, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{ProjectID: projectID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	proto := convert.AssetToProto(asset, parentName)

	// Populate latest version and version count.
	count, err := s.queries.CountAssetVersions(ctx, asset.ID)
	if err == nil {
		proto.VersionCount = int32(count)
	}
	latestVersion, err := s.queries.GetLatestAssetVersion(ctx, asset.ID)
	if err == nil {
		assetFullName := fmt.Sprintf("%s/assets/%s", parentName, assetName)
		proto.LatestVersion = convert.AssetVersionToProto(latestVersion, assetFullName)
		// Populate renditions on the latest version.
		renditions, err := s.queries.ListAssetRenditions(ctx, latestVersion.ID)
		if err == nil {
			proto.LatestVersion.Renditions = convert.RenditionsToProto(renditions)
		}
	}

	return proto, nil
}

func (s *AssetsServer) ListAssets(ctx context.Context, req *assetsv1.ListAssetsRequest) (*assetsv1.ListAssetsResponse, error) {
	orgName, projectName, err := parseAssetParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetParent())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var rows []db.Asset
	if req.GetShowDeleted() {
		rows, err = s.queries.ListAssetsByProjectWithDeleted(ctx, db.ListAssetsByProjectWithDeletedParams{
			ProjectID: projectID,
			Limit:     pageSize + 1,
			Offset:    0,
		})
	} else {
		rows, err = s.queries.ListAssetsByProject(ctx, db.ListAssetsByProjectParams{
			ProjectID: projectID,
			Limit:     pageSize + 1,
			Offset:    0,
		})
	}
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken = rows[pageSize].ID.String()
		rows = rows[:pageSize]
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	assets := make([]*assetsv1.Asset, 0, len(rows))
	for _, r := range rows {
		assets = append(assets, convert.AssetToProto(r, parentName))
	}

	return &assetsv1.ListAssetsResponse{
		Assets:        assets,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *AssetsServer) CreateAsset(ctx context.Context, req *assetsv1.CreateAssetRequest) (*longrunningpb.Operation, error) {
	asset := req.GetAsset()
	orgName, projectName, err := parseAssetParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Project", req.GetParent())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	isPlaceholder := req.GetEndpoint() == "" && req.GetFilename() == ""

	assetName := uuid.New().String()[:12]

	var endpointID pgtype.UUID
	if req.GetEndpoint() != "" {
		// Resolve endpoint — for now just extract the ID portion.
		// Full resolution would lookup the endpoint by name.
		endpointID = pgtype.UUID{Valid: false}
	}

	state := db.AssetStatePLACEHOLDER
	if !isPlaceholder {
		state = db.AssetStatePROCESSING
	}

	var annotationsJSON json.RawMessage
	if ann := asset.GetAnnotations(); ann != nil {
		annotationsJSON, _ = json.Marshal(ann)
	} else {
		annotationsJSON = json.RawMessage("{}")
	}

	result, err := s.queries.CreateAsset(ctx, db.CreateAssetParams{
		ID:          uuid.New(),
		ProjectID:   projectID,
		EndpointID:  endpointID,
		Name:        assetName,
		DisplayName: asset.GetDisplayName(),
		ImportPath:  "",
		Filename:    req.GetFilename(),
		State:       state,
		Annotations: annotationsJSON,
		CreatedBy:   "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", "")
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)

	if isPlaceholder {
		return lro.DoneOperation(convert.AssetToProto(result, parentName))
	}

	// Non-placeholder: for now just mark as ACTIVE (real pipeline later).
	_ = s.queries.UpdateAssetState(ctx, db.UpdateAssetStateParams{
		ID:    result.ID,
		State: db.AssetStateACTIVE,
	})
	result.State = db.AssetStateACTIVE
	return lro.DoneOperation(convert.AssetToProto(result, parentName))
}

func (s *AssetsServer) UpdateAsset(ctx context.Context, req *assetsv1.UpdateAssetRequest) (*longrunningpb.Operation, error) {
	asset := req.GetAsset()
	orgName, projectName, assetName, err := parseAssetName(asset.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", asset.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{ProjectID: projectID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", asset.GetName())
	}

	updateParams := db.UpdateAssetParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: asset.GetDisplayName(), Valid: true}
			case "annotations":
				ann, _ := json.Marshal(asset.GetAnnotations())
				updateParams.Annotations = ann
			case "expire_time":
				if asset.GetExpireTime() != nil {
					updateParams.ExpireTime = pgtype.Timestamptz{Time: asset.GetExpireTime().AsTime(), Valid: true}
				}
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: asset.GetDisplayName(), Valid: true}
	}

	result, err := s.queries.UpdateAsset(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", asset.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return lro.DoneOperation(convert.AssetToProto(result, parentName))
}

func (s *AssetsServer) DeleteAsset(ctx context.Context, req *assetsv1.DeleteAssetRequest) (*longrunningpb.Operation, error) {
	orgName, projectName, assetName, err := parseAssetName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{ProjectID: projectID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	err = s.queries.SoftDeleteAsset(ctx, db.SoftDeleteAssetParams{
		ID:        existing.ID,
		DeletedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	existing.State = db.AssetStateDELETEREQUESTED
	return lro.DoneOperation(convert.AssetToProto(existing, parentName))
}

func (s *AssetsServer) UndeleteAsset(ctx context.Context, req *assetsv1.UndeleteAssetRequest) (*longrunningpb.Operation, error) {
	orgName, projectName, assetName, err := parseAssetName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	projectID, err := s.resolveProject(ctx, orgName, projectName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{ProjectID: projectID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	if !existing.DeleteTime.Valid {
		return nil, apierr.FailedPrecondition("asset is not deleted")
	}

	err = s.queries.UndeleteAsset(ctx, existing.ID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	// Re-fetch to get updated state.
	updated, err := s.queries.GetAsset(ctx, existing.ID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/projects/%s", orgName, projectName)
	return lro.DoneOperation(convert.AssetToProto(updated, parentName))
}
