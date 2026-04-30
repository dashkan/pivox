package aichat

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/service/aichat/tools"
)

// Server implements the AiChat gRPC service.
type Server struct {
	aiv1.UnimplementedAiChatServer
	db                    db.DBTX
	queries               db.Querier
	model                 model.LanguageModel
	tools                 *tools.Registry
	logger                *slog.Logger
	codec                 *appkey.Codec
	resolver              *permission.Resolver
	audit                 *audit.Resolver
	conversationFilter    *filter.ResourceFilter
	messageFilter         *filter.ResourceFilter
	artifactFilter        *filter.ResourceFilter
	artifactVersionFilter *filter.ResourceFilter
}

// resolveConversationActors gathers the union of created_by/
// updated_by UUIDs across the page and resolves them in a single
// batched call. Returns nil when no audit resolver is wired.
func (s *Server) resolveConversationActors(ctx context.Context, rows []db.AiConversation) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		ids = append(ids, r.CreatedBy)
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve conversation actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
}

// resolveArtifactActors gathers created_by/updated_by UUIDs across
// a page of ai_artifacts.
func (s *Server) resolveArtifactActors(ctx context.Context, rows []db.AiArtifact) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve artifact actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
}

// resolveArtifactVersionActors gathers created_by UUIDs (versions
// have no updated_by — they're immutable).
func (s *Server) resolveArtifactVersionActors(ctx context.Context, rows []db.AiArtifactVersion) (map[uuid.UUID]*typespb.Actor, error) {
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
		slog.ErrorContext(ctx, "resolve artifact version actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
}

// getArtifactByName / getArtifactVersionForContent are tiny query
// adapters that let ContentHandler depend on a small interface
// (`conversationResolver`) instead of the full Server struct, so
// content_handler tests can stub them without standing up a real
// Server (model, codec, filters, ...).
func (s *Server) getArtifactByName(ctx context.Context, params db.GetArtifactByNameParams) (db.AiArtifact, error) {
	return s.queries.GetArtifactByName(ctx, params)
}

func (s *Server) getArtifactVersionForContent(ctx context.Context, params db.GetArtifactVersionForContentParams) (db.GetArtifactVersionForContentRow, error) {
	return s.queries.GetArtifactVersionForContent(ctx, params)
}

// NewServer creates a new AiChat service server. `resolver` is
// consumed by the per-resource ownership checks to ask "does the
// caller carry the `*All` audit permission?" — a regular member
// can only act on their own conversations; an admin/owner can
// audit/clean up any user's. Tests that don't exercise the
// ownership audit-bypass path may pass nil. `auditResolver`
// inflates audit-field UUIDs into Actor protos; nil leaves Actor
// fields unset (acceptable in tests).
func NewServer(pool db.DBTX, queries db.Querier, llm model.LanguageModel, toolRegistry *tools.Registry, codec *appkey.Codec, resolver *permission.Resolver, auditResolver *audit.Resolver, logger *slog.Logger) *Server {
	if toolRegistry == nil {
		toolRegistry = tools.NewRegistry()
	}
	return &Server{
		db:                    pool,
		queries:               queries,
		model:                 llm,
		tools:                 toolRegistry,
		logger:                logger,
		codec:                 codec,
		resolver:              resolver,
		audit:                 auditResolver,
		conversationFilter:    filter.ConversationFilter(),
		messageFilter:         filter.MessageFilter(),
		artifactFilter:        filter.ArtifactFilter(),
		artifactVersionFilter: filter.ArtifactVersionFilter(),
	}
}
