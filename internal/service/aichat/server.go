package aichat

import (
	"log/slog"

	db "github.com/dashkan/pivox/internal/db/generated"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/service/aichat/model"
	"github.com/dashkan/pivox/internal/service/aichat/tools"
)

// Server implements the AiChat gRPC service.
type Server struct {
	aiv1.UnimplementedAiChatServer
	db      db.DBTX
	queries db.Querier
	model   model.LanguageModel
	tools   *tools.Registry
	logger  *slog.Logger
}

// NewServer creates a new AiChat service server.
func NewServer(pool db.DBTX, queries db.Querier, llm model.LanguageModel, toolRegistry *tools.Registry, logger *slog.Logger) *Server {
	if toolRegistry == nil {
		toolRegistry = tools.NewRegistry()
	}
	return &Server{
		db:      pool,
		queries: queries,
		model:   llm,
		tools:   toolRegistry,
		logger:  logger,
	}
}
