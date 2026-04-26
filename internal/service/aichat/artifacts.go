package aichat

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/server"
)

func (s *Server) GetArtifact(ctx context.Context, req *aiv1.GetArtifactRequest) (*aiv1.Artifact, error) {
	orgName, convName, artName, err := parseArtifactName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	uid := server.MustAuthenticatedUID(ctx)

	conv, err := s.resolveConversation(ctx, orgName, convName, uid)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetArtifactByName(ctx, db.GetArtifactByNameParams{
		ConversationID: conv.ID,
		Name:           artName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	convFullName := buildConversationName(orgName, convName)
	return convert.ArtifactToProto(row, convFullName), nil
}

func (s *Server) ListArtifacts(ctx context.Context, req *aiv1.ListArtifactsRequest) (*aiv1.ListArtifactsResponse, error) {
	orgName, convName, err := parseArtifactParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetParent())
	}

	uid := server.MustAuthenticatedUID(ctx)

	conv, err := s.resolveConversation(ctx, orgName, convName, uid)
	if err != nil {
		return nil, err
	}

	rows, err := filter.Query(ctx, s.db, s.artifactFilter, filter.QueryParams{
		Filter:   req.GetFilter(),
		ParentID: conv.ID.String(),
		OrderBy:  req.GetOrderBy(),
		PageSize: req.GetPageSize(),
		Cursor:   req.GetPageToken(),
		Codec:    s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanArtifacts(rows)
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

	convFullName := buildConversationName(orgName, convName)
	artifacts := make([]*aiv1.Artifact, 0, len(results))
	for _, r := range results {
		artifacts = append(artifacts, convert.ArtifactToProto(r, convFullName))
	}

	return &aiv1.ListArtifactsResponse{
		Artifacts:     artifacts,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Server) DeleteArtifact(ctx context.Context, req *aiv1.DeleteArtifactRequest) (*emptypb.Empty, error) {
	orgName, convName, artName, err := parseArtifactName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	uid := server.MustAuthenticatedUID(ctx)

	conv, err := s.resolveConversation(ctx, orgName, convName, uid)
	if err != nil {
		return nil, err
	}

	row, err := s.queries.GetArtifactByName(ctx, db.GetArtifactByNameParams{
		ConversationID: conv.ID,
		Name:           artName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	// Check for children if force is not set.
	if !req.GetForce() {
		count, err := s.queries.CountArtifactVersionsByArtifact(ctx, row.ID)
		if err != nil {
			return nil, apierr.Internal("database error")
		}
		if count > 0 {
			return nil, apierr.FailedPrecondition(fmt.Sprintf("artifact has %d version(s); set force=true to delete", count))
		}
	}

	if err := s.queries.DeleteArtifact(ctx, row.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	return &emptypb.Empty{}, nil
}

// resolveConversation resolves (orgName, convName) to the DB row and verifies
// the authenticated user owns it. All code paths that load a conversation
// should go through this helper so ownership is enforced identically.
func (s *Server) resolveConversation(ctx context.Context, orgName, convName, uid string) (db.AiConversation, error) {
	orgID, err := s.resolveOrg(ctx, orgName)
	if err != nil {
		return db.AiConversation{}, err
	}

	conv, err := s.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: orgID,
		Name:  convName,
	})
	if err != nil {
		return db.AiConversation{}, apierr.HandleResourceError(err, "Conversation", buildConversationName(orgName, convName))
	}
	if conv.CreatedBy != uid {
		// Don't leak existence of other users' conversations — surface as
		// NotFound (same result HandleResourceError produces for missing rows).
		return db.AiConversation{}, apierr.HandleResourceError(pgx.ErrNoRows, "Conversation", buildConversationName(orgName, convName))
	}
	return conv, nil
}
