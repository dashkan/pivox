package aichat

import (
	"log/slog"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
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
	conversationFilter    *filter.ResourceFilter
	messageFilter         *filter.ResourceFilter
	artifactFilter        *filter.ResourceFilter
	artifactVersionFilter *filter.ResourceFilter
}

// NewServer creates a new AiChat service server.
func NewServer(pool db.DBTX, queries db.Querier, llm model.LanguageModel, toolRegistry *tools.Registry, codec *appkey.Codec, logger *slog.Logger) *Server {
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
		conversationFilter:    filter.ConversationFilter(),
		messageFilter:         filter.MessageFilter(),
		artifactFilter:        filter.ArtifactFilter(),
		artifactVersionFilter: filter.ArtifactVersionFilter(),
	}
}
