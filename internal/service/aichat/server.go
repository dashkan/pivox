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
	txer                  db.Txer
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

// Config is the constructor input for the AiChat Server. `Resolver`
// is consumed by the per-resource ownership checks to ask "does the
// caller carry the `*All` audit permission?" — a regular member
// can only act on their own conversations; an admin/owner can
// audit/clean up any user's.
type Config struct {
	// Pool is the database pool — DBTX for filter.Query / non-tx
	// reads and TxBeginner for tx-wrapped delete paths. Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Model is the LLM backing the chat handlers. Required.
	Model model.LanguageModel
	// Tools is the optional tool registry. Nil falls back to an
	// empty default registry.
	Tools *tools.Registry
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// Resolver gates the `*All` audit-bypass permission. Optional;
	// tests that don't exercise the bypass path may pass nil.
	Resolver *permission.Resolver
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
	// Logger is the structured logger. Required.
	Logger *slog.Logger
}

// NewServer constructs the AiChat server from cfg. Panics on a
// missing required field — a startup-time programmer error rather
// than a runtime failure.
func NewServer(cfg Config) *Server {
	if cfg.Pool == nil {
		panic("aichat: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("aichat: Config.Queries is required")
	}
	if cfg.Model == nil {
		panic("aichat: Config.Model is required")
	}
	if cfg.Codec == nil {
		panic("aichat: Config.Codec is required")
	}
	if cfg.Logger == nil {
		panic("aichat: Config.Logger is required")
	}
	toolRegistry := cfg.Tools
	if toolRegistry == nil {
		toolRegistry = tools.NewRegistry()
	}
	return &Server{
		db:                    cfg.Pool,
		txer:                  &db.PoolTxer{Pool: cfg.Pool},
		queries:               cfg.Queries,
		model:                 cfg.Model,
		tools:                 toolRegistry,
		logger:                cfg.Logger,
		codec:                 cfg.Codec,
		resolver:              cfg.Resolver,
		audit:                 cfg.AuditResolver,
		conversationFilter:    filter.ConversationFilter(),
		messageFilter:         filter.MessageFilter(),
		artifactFilter:        filter.ArtifactFilter(),
		artifactVersionFilter: filter.ArtifactVersionFilter(),
	}
}
