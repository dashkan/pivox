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
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	assetsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/assets/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"

	"log/slog"
)

type AssetsServer struct {
	assetsv1.UnimplementedAssetsServer
	db      db.DBTX
	queries db.Querier
	audit   *audit.Resolver
}

// Config is the constructor input for AssetsServer.
type Config struct {
	// Pool is the database pool used for reads. Required.
	Pool db.DBTX
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewAssetsServer constructs the server from cfg. Panics on a missing
// required field.
func NewAssetsServer(cfg Config) *AssetsServer {
	if cfg.Pool == nil {
		panic("assets: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("assets: Config.Queries is required")
	}
	return &AssetsServer{
		db:      cfg.Pool,
		queries: cfg.Queries,
		audit:   cfg.AuditResolver,
	}
}

// resolveAssetActors gathers created_by/updated_by/deleted_by UUIDs
// across the page and resolves them in a single batched call.
func (s *AssetsServer) resolveAssetActors(ctx context.Context, rows []db.Asset) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*3)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
		if r.DeletedBy.Valid {
			ids = append(ids, r.DeletedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve asset actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
}

// resolveAssetVersionActors gathers created_by UUIDs (versions are
// immutable so no updated_by).
func (s *AssetsServer) resolveAssetVersionActors(ctx context.Context, rows []db.AssetVersion) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve asset version actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
}

// parseAssetName parses "organizations/{org}/spaces/{space}/assets/{asset}".
func parseAssetName(name string) (orgName, spaceName, assetName string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "spaces" || parts[4] != "assets" {
		return "", "", "", fmt.Errorf("invalid asset name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseAssetParent parses "organizations/{org}/spaces/{space}".
func parseAssetParent(parent string) (orgName, spaceName string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" {
		return "", "", fmt.Errorf("invalid parent %q", parent)
	}
	return parts[1], parts[3], nil
}

// resolveSpace resolves org name + space name to space UUID.
func (s *AssetsServer) resolveSpace(ctx context.Context, orgName, spaceName string) (uuid.UUID, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	space, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceName})
	if err != nil {
		return uuid.Nil, apierr.HandleResourceError(err, "Space", fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName))
	}
	return space.ID, nil
}

func (s *AssetsServer) GetAsset(ctx context.Context, req *assetsv1.GetAssetRequest) (*assetsv1.Asset, error) {
	orgName, spaceName, assetName, err := parseAssetName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}
	asset, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{SpaceID: spaceID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, err := s.resolveAssetActors(ctx, []db.Asset{asset})
	if err != nil {
		return nil, err
	}
	proto := convert.AssetToProto(asset, parentName, actors)

	// Populate latest version and version count.
	count, err := s.queries.CountAssetVersions(ctx, asset.ID)
	if err == nil {
		proto.VersionCount = int32(count)
	}
	latestVersion, err := s.queries.GetLatestAssetVersion(ctx, asset.ID)
	if err == nil {
		assetFullName := fmt.Sprintf("%s/assets/%s", parentName, assetName)
		versionActors, vErr := s.resolveAssetVersionActors(ctx, []db.AssetVersion{latestVersion})
		if vErr != nil {
			return nil, vErr
		}
		proto.LatestVersion = convert.AssetVersionToProto(latestVersion, assetFullName, versionActors)
		// Populate renditions on the latest version.
		renditions, err := s.queries.ListAssetRenditions(ctx, latestVersion.ID)
		if err == nil {
			proto.LatestVersion.Renditions = convert.RenditionsToProto(renditions)
		}
	}

	return proto, nil
}

func (s *AssetsServer) ListAssets(ctx context.Context, req *assetsv1.ListAssetsRequest) (*assetsv1.ListAssetsResponse, error) {
	orgName, spaceName, err := parseAssetParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetParent())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
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
		rows, err = s.queries.ListAssetsBySpaceWithDeleted(ctx, db.ListAssetsBySpaceWithDeletedParams{
			SpaceID: spaceID,
			Limit:   pageSize + 1,
			Offset:  0,
		})
	} else {
		rows, err = s.queries.ListAssetsBySpace(ctx, db.ListAssetsBySpaceParams{
			SpaceID: spaceID,
			Limit:   pageSize + 1,
			Offset:  0,
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

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, err := s.resolveAssetActors(ctx, rows)
	if err != nil {
		return nil, err
	}
	assets := make([]*assetsv1.Asset, 0, len(rows))
	for _, r := range rows {
		assets = append(assets, convert.AssetToProto(r, parentName, actors))
	}

	return &assetsv1.ListAssetsResponse{
		Assets:        assets,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *AssetsServer) CreateAsset(ctx context.Context, req *assetsv1.CreateAssetRequest) (*longrunningpb.Operation, error) {
	asset := req.GetAsset()
	orgName, spaceName, err := parseAssetParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetParent())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
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
		SpaceID:     spaceID,
		EndpointID:  endpointID,
		Name:        assetName,
		DisplayName: asset.GetDisplayName(),
		ImportPath:  "",
		Filename:    req.GetFilename(),
		State:       state,
		Annotations: annotationsJSON,
		CreatedBy:   pgtype.UUID{},
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", "")
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)

	if isPlaceholder {
		actors, resolveErr := s.resolveAssetActors(ctx, []db.Asset{result})
		if resolveErr != nil {
			slog.WarnContext(ctx, "create asset: actor resolution failed; returning proto without audit actors",
				"asset_id", result.ID, "error", resolveErr)
			actors = nil
		}
		return lro.DoneOperation(convert.AssetToProto(result, parentName, actors))
	}

	// Non-placeholder: for now just mark as ACTIVE (real pipeline later).
	_ = s.queries.UpdateAssetState(ctx, db.UpdateAssetStateParams{
		ID:    result.ID,
		State: db.AssetStateACTIVE,
	})
	result.State = db.AssetStateACTIVE
	actors, resolveErr := s.resolveAssetActors(ctx, []db.Asset{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create asset: actor resolution failed; returning proto without audit actors",
			"asset_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.AssetToProto(result, parentName, actors))
}

func (s *AssetsServer) UpdateAsset(ctx context.Context, req *assetsv1.UpdateAssetRequest) (*longrunningpb.Operation, error) {
	asset := req.GetAsset()
	orgName, spaceName, assetName, err := parseAssetName(asset.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", asset.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{SpaceID: spaceID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", asset.GetName())
	}

	updateParams := db.UpdateAssetParams{
		ID:        existing.ID,
		UpdatedBy: pgtype.UUID{},
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

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveAssetActors(ctx, []db.Asset{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update asset: actor resolution failed; returning proto without audit actors",
			"asset_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.AssetToProto(result, parentName, actors))
}

func (s *AssetsServer) DeleteAsset(ctx context.Context, req *assetsv1.DeleteAssetRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, assetName, err := parseAssetName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{SpaceID: spaceID, Name: assetName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	err = s.queries.SoftDeleteAsset(ctx, db.SoftDeleteAssetParams{
		ID:        existing.ID,
		DeletedBy: pgtype.UUID{},
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	existing.State = db.AssetStateDELETEREQUESTED
	actors, resolveErr := s.resolveAssetActors(ctx, []db.Asset{existing})
	if resolveErr != nil {
		slog.WarnContext(ctx, "delete asset: actor resolution failed; returning proto without audit actors",
			"asset_id", existing.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.AssetToProto(existing, parentName, actors))
}

func (s *AssetsServer) UndeleteAsset(ctx context.Context, req *assetsv1.UndeleteAssetRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, assetName, err := parseAssetName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Asset", req.GetName())
	}
	spaceID, err := s.resolveSpace(ctx, orgName, spaceName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetAssetByName(ctx, db.GetAssetByNameParams{SpaceID: spaceID, Name: assetName})
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

	parentName := fmt.Sprintf("organizations/%s/spaces/%s", orgName, spaceName)
	actors, resolveErr := s.resolveAssetActors(ctx, []db.Asset{updated})
	if resolveErr != nil {
		slog.WarnContext(ctx, "undelete asset: actor resolution failed; returning proto without audit actors",
			"asset_id", updated.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.AssetToProto(updated, parentName, actors))
}
