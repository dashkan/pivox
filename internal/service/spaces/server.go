package spaces

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/resource"
	"github.com/dashkan/pivox/internal/server"
)

// TxBeginner abstracts transaction creation for testability.
// *pgxpool.Pool satisfies this interface. Same shape as the
// organizations service's local TxBeginner.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type SpacesServer struct {
	apiv1.UnimplementedSpacesServer
	db       db.DBTX
	pool     TxBeginner
	queries  db.Querier
	filter   *filter.ResourceFilter
	codec    *appkey.Codec
	resolver *permission.Resolver
	caller   server.CallerIdentityResolver
}

// NewSpacesServer constructs the server. `resolver` and `caller` are
// only consumed by the IAM-shaped handlers (TestIamPermissions and
// space-scope Member CRUD). `pool` is used for tx-wrapped writes
// (CreateMember principal validation + insert atomicity); tests that
// only exercise reads may pass nil.
func NewSpacesServer(pool db.DBTX, txPool TxBeginner, queries db.Querier, codec *appkey.Codec, resolver *permission.Resolver, caller server.CallerIdentityResolver) *SpacesServer {
	return &SpacesServer{
		db:       pool,
		pool:     txPool,
		queries:  queries,
		filter:   filter.SpaceFilter(),
		codec:    codec,
		resolver: resolver,
		caller:   caller,
	}
}

// parseSpaceName parses "organizations/{org}/spaces/{space}" and returns (orgName, spaceName).
func parseSpaceName(name string) (string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" {
		return "", "", fmt.Errorf("invalid space name %q: expected organizations/*/spaces/*", name)
	}
	return parts[1], parts[3], nil
}

func (s *SpacesServer) GetSpace(ctx context.Context, req *apiv1.GetSpaceRequest) (*apiv1.Space, error) {
	orgName, spaceName, err := parseSpaceName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	space, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	return convert.SpaceToProto(space, orgName), nil
}

func (s *SpacesServer) ListSpaces(ctx context.Context, req *apiv1.ListSpacesRequest) (*apiv1.ListSpacesResponse, error) {
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
		Codec:       s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanSpaces(rows)
	if err != nil {
		return nil, apierr.Internal("database error")
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
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		results = results[:pageSize]
	}

	spaces := make([]*apiv1.Space, 0, len(results))
	for _, r := range results {
		spaces = append(spaces, convert.SpaceToProto(r, orgName))
	}

	return &apiv1.ListSpacesResponse{
		Spaces:        spaces,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *SpacesServer) CreateSpace(ctx context.Context, req *apiv1.CreateSpaceRequest) (*longrunningpb.Operation, error) {
	space := req.GetSpace()

	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}
	orgName, _ := resource.ParseSegment(req.GetParent())

	spaceName := req.GetSpaceId()
	if spaceName == "" {
		spaceName = uuid.New().String()[:8]
	}

	var labelsJSON json.RawMessage
	if labels := space.GetLabels(); labels != nil {
		labelsJSON, _ = json.Marshal(labels)
	} else {
		labelsJSON = json.RawMessage("{}")
	}

	result, err := s.queries.CreateSpace(ctx, db.CreateSpaceParams{
		ID:          uuid.New(),
		OrgID:       orgID,
		Name:        spaceName,
		DisplayName: space.GetDisplayName(),
		Labels:      labelsJSON,
		CreatedBy:   "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", "")
	}

	return lro.DoneOperation(convert.SpaceToProto(result, orgName))
}

func (s *SpacesServer) UpdateSpace(ctx context.Context, req *apiv1.UpdateSpaceRequest) (*longrunningpb.Operation, error) {
	space := req.GetSpace()
	orgName, spaceName, err := parseSpaceName(space.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", space.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", space.GetName())
	}

	updateParams := db.UpdateSpaceParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: space.GetDisplayName(), Valid: true}
			case "labels":
				labelsJSON, err := json.Marshal(space.GetLabels())
				if err != nil {
					return nil, apierr.Internal("failed to marshal labels")
				}
				updateParams.Labels = labelsJSON
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: space.GetDisplayName(), Valid: true}
		if labels := space.GetLabels(); labels != nil {
			labelsJSON, _ := json.Marshal(labels)
			updateParams.Labels = labelsJSON
		} else {
			updateParams.Labels = existing.Labels
		}
	}

	result, err := s.queries.UpdateSpace(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", space.GetName())
	}

	return lro.DoneOperation(convert.SpaceToProto(result, orgName))
}

func (s *SpacesServer) DeleteSpace(ctx context.Context, req *apiv1.DeleteSpaceRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, err := parseSpaceName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}

	result, err := s.queries.SoftDeleteSpace(ctx, db.SoftDeleteSpaceParams{
		ID:        existing.ID,
		DeletedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	return lro.DoneOperation(convert.SpaceToProto(result, orgName))
}

func (s *SpacesServer) UndeleteSpace(ctx context.Context, req *apiv1.UndeleteSpaceRequest) (*longrunningpb.Operation, error) {
	orgName, spaceName, err := parseSpaceName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{OrgID: org.ID, Name: spaceName})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}

	result, err := s.queries.UndeleteSpace(ctx, db.UndeleteSpaceParams{
		ID:        existing.ID,
		UpdatedBy: "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetName())
	}
	return lro.DoneOperation(convert.SpaceToProto(result, orgName))
}
