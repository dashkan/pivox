package aichat

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func (s *Server) GetArtifact(ctx context.Context, req *aiv1.GetArtifactRequest) (*aiv1.Artifact, error) {
	orgName, pathUser, convName, artName, err := parseArtifactName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	conv, err := s.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsReadAll)
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

	convFullName := buildConversationName(orgName, pathUser, convName)
	actors, err := s.resolveArtifactActors(ctx, []db.AiArtifact{row})
	if err != nil {
		return nil, err
	}
	return convert.ArtifactToProto(row, convFullName, actors), nil
}

func (s *Server) ListArtifacts(ctx context.Context, req *aiv1.ListArtifactsRequest) (*aiv1.ListArtifactsResponse, error) {
	orgName, pathUser, convName, err := parseArtifactParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Conversation", req.GetParent())
	}

	conv, err := s.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsReadAll)
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

	convFullName := buildConversationName(orgName, pathUser, convName)
	actors, err := s.resolveArtifactActors(ctx, results)
	if err != nil {
		return nil, err
	}
	artifacts := make([]*aiv1.Artifact, 0, len(results))
	for _, r := range results {
		artifacts = append(artifacts, convert.ArtifactToProto(r, convFullName, actors))
	}

	return &aiv1.ListArtifactsResponse{
		Artifacts:     artifacts,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *Server) DeleteArtifact(ctx context.Context, req *aiv1.DeleteArtifactRequest) (*emptypb.Empty, error) {
	orgName, pathUser, convName, artName, err := parseArtifactName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Artifact", req.GetName())
	}

	// Artifact mutation is creator-only — admins don't edit
	// artifacts they don't own.
	conv, err := s.resolveConversation(ctx, orgName, pathUser, convName, "")
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
