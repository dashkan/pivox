package aichat

import (
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
