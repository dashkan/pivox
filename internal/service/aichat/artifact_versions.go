package aichat

import (
	"context"

	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func (s *Server) GetArtifactVersion(ctx context.Context, req *aiv1.GetArtifactVersionRequest) (*aiv1.ArtifactVersion, error) {
	orgName, convName, artName, verName, err := parseArtifactVersionName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	art, err := s.resolveArtifact(ctx, orgName, convName, artName)
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

	artFullName := buildArtifactName(orgName, convName, artName)
	return convert.ArtifactVersionToProtoAi(row, artFullName), nil
}

func (s *Server) ListArtifactVersions(ctx context.Context, req *aiv1.ListArtifactVersionsRequest) (*aiv1.ListArtifactVersionsResponse, error) {
	orgName, convName, artName, err := parseArtifactVersionParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetParent())
	}

	art, err := s.resolveArtifact(ctx, orgName, convName, artName)
	if err != nil {
		return nil, err
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 50
	}
	if pageSize > 100 {
		pageSize = 100
	}

	rows, err := s.queries.ListArtifactVersionsByArtifact(ctx, db.ListArtifactVersionsByArtifactParams{
		ArtifactID: art.ID,
		Limit:      pageSize + 1,
		Offset:     0,
	})
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken = rows[pageSize].ID.String()
		rows = rows[:pageSize]
	}

	versions := make([]*aiv1.ArtifactVersion, 0, len(rows))
	for _, r := range rows {
		pb := &aiv1.ArtifactVersion{
			Name:       buildArtifactVersionName(orgName, convName, artName, r.Name),
			CreateTime: timestamppb.New(r.CreateTime),
		}
		if r.InlineContentType.Valid {
			pb.Content = &aiv1.ArtifactVersion_Inline{
				Inline: &aiv1.InlineContent{
					MimeType:  r.InlineContentType.String,
					SizeBytes: r.InlineSizeBytes.Int64,
					// Data intentionally omitted in list responses.
				},
			}
		} else if r.AssetVersionName.Valid {
			pb.Content = &aiv1.ArtifactVersion_AssetVersion{
				AssetVersion: r.AssetVersionName.String,
			}
		}
		versions = append(versions, pb)
	}

	return &aiv1.ListArtifactVersionsResponse{
		Versions:      versions,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Server) DeleteArtifactVersion(ctx context.Context, req *aiv1.DeleteArtifactVersionRequest) (*emptypb.Empty, error) {
	orgName, convName, artName, verName, err := parseArtifactVersionName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "ArtifactVersion", req.GetName())
	}

	art, err := s.resolveArtifact(ctx, orgName, convName, artName)
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

	// If this is the last version, delete the parent artifact too.
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
				"artifact", buildArtifactName(orgName, convName, artName),
				"error", err)
		}
	}

	return &emptypb.Empty{}, nil
}

// resolveArtifact resolves org + conversation + artifact names to the DB row.
func (s *Server) resolveArtifact(ctx context.Context, orgName, convName, artName string) (db.AiArtifact, error) {
	conv, err := s.resolveConversation(ctx, orgName, convName)
	if err != nil {
		return db.AiArtifact{}, err
	}

	art, err := s.queries.GetArtifactByName(ctx, db.GetArtifactByNameParams{
		ConversationID: conv.ID,
		Name:           artName,
	})
	if err != nil {
		return db.AiArtifact{}, apierr.HandleResourceError(err, "Artifact", buildArtifactName(orgName, convName, artName))
	}
	return art, nil
}
