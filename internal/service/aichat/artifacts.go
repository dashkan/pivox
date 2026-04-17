package aichat

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func (s *Server) GetArtifact(ctx context.Context, req *aiv1.GetArtifactRequest) (*aiv1.Artifact, error) {
	orgName, convName, artName, err := parseArtifactName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	conv, err := s.resolveConversation(ctx, orgName, convName)
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

	conv, err := s.resolveConversation(ctx, orgName, convName)
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

	rows, err := s.queries.ListArtifactsByConversation(ctx, db.ListArtifactsByConversationParams{
		ConversationID: conv.ID,
		Limit:          pageSize + 1,
		Offset:         0,
	})
	if err != nil {
		return nil, apierr.Internal("database error")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken = rows[pageSize].ID.String()
		rows = rows[:pageSize]
	}

	convFullName := buildConversationName(orgName, convName)
	artifacts := make([]*aiv1.Artifact, 0, len(rows))
	for _, r := range rows {
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

	conv, err := s.resolveConversation(ctx, orgName, convName)
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
			return nil, status.Errorf(codes.FailedPrecondition,
				"artifact has %d version(s); set force=true to delete", count)
		}
	}

	if err := s.queries.DeleteArtifact(ctx, row.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	return &emptypb.Empty{}, nil
}

// resolveConversation resolves org name + conversation name to the DB row.
func (s *Server) resolveConversation(ctx context.Context, orgName, convName string) (db.AiConversation, error) {
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
	return conv, nil
}
