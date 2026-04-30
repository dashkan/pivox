package aichat

import (
	"context"
	"log/slog"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
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
	conversationFilter    *filter.ResourceFilter
	messageFilter         *filter.ResourceFilter
	artifactFilter        *filter.ResourceFilter
	artifactVersionFilter *filter.ResourceFilter
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
// ownership audit-bypass path may pass nil.
func NewServer(pool db.DBTX, queries db.Querier, llm model.LanguageModel, toolRegistry *tools.Registry, codec *appkey.Codec, resolver *permission.Resolver, logger *slog.Logger) *Server {
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
		conversationFilter:    filter.ConversationFilter(),
		messageFilter:         filter.MessageFilter(),
		artifactFilter:        filter.ArtifactFilter(),
		artifactVersionFilter: filter.ArtifactVersionFilter(),
	}
}
