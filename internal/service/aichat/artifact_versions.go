package aichat

import (
	"context"

	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func (s *Server) GetArtifactVersion(ctx context.Context, req *aiv1.GetArtifactVersionRequest) (*aiv1.ArtifactVersion, error) {
	orgName, pathUser, convName, artName, verName, err := parseArtifactVersionName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	art, err := s.resolveArtifact(ctx, orgName, pathUser, convName, artName, permission.AiConversationsReadAll)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetArtifactVersionByName(ctx, db.GetArtifactVersionByNameParams{
		ArtifactID: art.ID,
		Name:       verName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	artFullName := buildArtifactName(orgName, pathUser, convName, artName)
	actors, err := s.resolveArtifactVersionActors(ctx, []db.AiArtifactVersion{row})
	if err != nil {
		return nil, err
	}
	return convert.ArtifactVersionToProtoAi(row, artFullName, actors), nil
}

func (s *Server) ListArtifactVersions(ctx context.Context, req *aiv1.ListArtifactVersionsRequest) (*aiv1.ListArtifactVersionsResponse, error) {
	orgName, pathUser, convName, artName, err := parseArtifactVersionParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetParent())
	}

	art, err := s.resolveArtifact(ctx, orgName, pathUser, convName, artName, permission.AiConversationsReadAll)
	if err != nil {
		return nil, err
	}

	rows, err := filter.Query(ctx, s.db, s.artifactVersionFilter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: art.ID.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanArtifactVersions(rows)
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	var nextPageToken string
	if int32(len(results)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, results[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		results = results[:pageSize]
	}

	actors, err := s.resolveArtifactVersionActors(ctx, results)
	if err != nil {
		return nil, err
	}
	artFullName := buildArtifactName(orgName, pathUser, convName, artName)
	versions := make([]*aiv1.ArtifactVersion, 0, len(results))
	for _, r := range results {
		pb := convert.ArtifactVersionToProtoAi(r, artFullName, actors)
		// List responses strip inline `data` to keep payloads small;
		// callers fetch full bytes via :content. Drop the `Data`
		// field while preserving mime + size so the client still
		// knows what's in the version.
		if inline := pb.GetInline(); inline != nil {
			inline.Data = nil
		}
		versions = append(versions, pb)
	}

	return &aiv1.ListArtifactVersionsResponse{
		Versions:      versions,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Server) DeleteArtifactVersion(ctx context.Context, req *aiv1.DeleteArtifactVersionRequest) (*emptypb.Empty, error) {
	orgName, pathUser, convName, artName, verName, err := parseArtifactVersionName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	art, err := s.resolveArtifact(ctx, orgName, pathUser, convName, artName, "")
	if err != nil {
		return nil, err
	}

	ver, err := s.queries.GetArtifactVersionByName(ctx, db.GetArtifactVersionByNameParams{
		ArtifactID: art.ID,
		Name:       verName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	isOnly, err := s.queries.IsOnlyArtifactVersion(ctx, art.ID)
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	if err := s.queries.DeleteArtifactVersion(ctx, ver.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	if isOnly {
		if err := s.queries.DeleteArtifact(ctx, art.ID); err != nil {
			s.logger.Warn("failed to cascade delete parent artifact",
				"artifact", buildArtifactName(orgName, pathUser, convName, artName),
				"error", err)
		}
	}

	return &emptypb.Empty{}, nil
}

// resolveArtifact resolves org + conversation + artifact names to
// the DB row, verifying ownership through resolveConversation.
// Pass `allPerm = ""` for creator-only operations.
func (s *Server) resolveArtifact(ctx context.Context, orgName string, pathUser uuid.UUID, convName, artName, allPerm string) (db.AiArtifact, error) {
	conv, err := s.resolveConversation(ctx, orgName, pathUser, convName, allPerm)
	if err != nil {
		return db.AiArtifact{}, err
	}

	art, err := s.queries.GetArtifactByName(ctx, db.GetArtifactByNameParams{
		ConversationID: conv.ID,
		Name:           artName,
	})
	if err != nil {
		return db.AiArtifact{}, apierr.HandleResourceError(err, "Artifact", buildArtifactName(orgName, pathUser, convName, artName))
	}
	return art, nil
}
