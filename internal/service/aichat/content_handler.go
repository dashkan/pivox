package aichat

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
)

// conversationResolver is the small slice of *Server that ContentHandler
// depends on. Pulling it behind an interface lets the unit tests stub
// the conversation resolution + queries without standing up a real
// Server (which would drag in the model, codec, filters, etc.).
type conversationResolver interface {
	resolveConversation(ctx context.Context, orgName string, pathUser uuid.UUID, convName, allPerm string) (db.AiConversation, error)
	getArtifactByName(ctx context.Context, params db.GetArtifactByNameParams) (db.AiArtifact, error)
	getArtifactVersionForContent(ctx context.Context, params db.GetArtifactVersionForContentParams) (db.GetArtifactVersionForContentRow, error)
}

// ContentHandler serves inline artifact version content over HTTP.
// Asset-backed versions are not served here — clients use the asset system.
//
// Ownership reuses the gRPC artifact handlers' resolveConversation
// (path-vs-row creator check + audit-bypass via
// `ai.conversations.readAll`). The HTTP path mirrors the gRPC
// artifact version resource shape post-Phase-7:
//
//	/v1/organizations/{org}/users/{user}/conversations/{conv}/artifacts/{art}/versions/{ver}:content
type ContentHandler struct {
	resolver conversationResolver
	logger   *slog.Logger
}

// ContentHandlerConfig is the constructor input for ContentHandler.
// Suffixed to avoid colliding with the package-level Config used by
// Server.
type ContentHandlerConfig struct {
	// Server provides the queries + permission resolver used for
	// ownership enforcement; without it the handler can't run the
	// path-vs-row creator check that the gRPC artifact handlers
	// depend on. Required.
	Server *Server
	// Logger is the slog logger used for content-write failures.
	// Required.
	Logger *slog.Logger
}

// NewContentHandler constructs a content handler from cfg. Panics on
// a missing required field — startup-time programmer error, fail
// loud on boot.
func NewContentHandler(cfg ContentHandlerConfig) *ContentHandler {
	if cfg.Server == nil {
		panic("aichat: ContentHandlerConfig.Server is required")
	}
	if cfg.Logger == nil {
		panic("aichat: ContentHandlerConfig.Logger is required")
	}
	return &ContentHandler{resolver: cfg.Server, logger: cfg.Logger}
}

func (h *ContentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse the new shape:
	//   /v1/organizations/{org}/users/{user}/conversations/{conv}/artifacts/{art}/versions/{ver}:content
	orgName, pathUser, convName, artName, verName, ok := parseContentPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Mirror the gRPC artifact handlers: path-vs-row creator check
	// plus optional audit-bypass via `ai.conversations.readAll` so
	// admins/owners can pull a peer's artifact for legal/audit. The
	// auth interceptor populates the caller's identity id or rejects
	// the request before we get here.
	conv, err := h.resolver.resolveConversation(ctx, orgName, pathUser, convName, permission.AiConversationsReadAll)
	if err != nil {
		writeAPIError(w, r, err)
		return
	}

	art, err := h.resolver.getArtifactByName(ctx, db.GetArtifactByNameParams{
		ConversationID: conv.ID,
		Name:           artName,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	row, err := h.resolver.getArtifactVersionForContent(ctx, db.GetArtifactVersionForContentParams{
		ArtifactID: art.ID,
		Name:       verName,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Asset-backed versions are not served here.
	if row.AssetVersionName.Valid {
		http.Error(w, "artifact content is asset-backed; fetch via GetAssetVersion", http.StatusNotFound)
		return
	}

	if row.InlineData == nil {
		http.NotFound(w, r)
		return
	}

	// ETag + conditional request.
	etag := `"` + verName + `"`
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	w.Header().Set("Content-Type", row.InlineContentType.String)
	w.Header().Set("Content-Length", strconv.FormatInt(row.InlineSizeBytes.Int64, 10))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := w.Write(row.InlineData); err != nil {
		h.logger.Warn("failed to write artifact content", "error", err)
	}
}

// writeAPIError maps the gRPC-status errors that resolveConversation
// returns onto HTTP status codes. NotFound stays 404 (preserves the
// existence-probe defense in resolveConversation); PermissionDenied
// → 403; anything else degrades to 500.
func writeAPIError(w http.ResponseWriter, r *http.Request, err error) {
	switch status.Code(err) {
	case codes.NotFound:
		http.NotFound(w, r)
	case codes.PermissionDenied:
		http.Error(w, "forbidden", http.StatusForbidden)
	case codes.Unauthenticated:
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	default:
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// parseContentPath extracts org, user-uuid, conv, artifact, version from:
// /v1/organizations/{org}/users/{user}/conversations/{conv}/artifacts/{art}/versions/{ver}:content
func parseContentPath(path string) (org string, user uuid.UUID, conv, art, ver string, ok bool) {
	// Strip ":content" suffix.
	path, found := strings.CutSuffix(path, ":content")
	if !found {
		return "", uuid.Nil, "", "", "", false
	}

	// Strip leading "/v1/" prefix.
	path, found = strings.CutPrefix(path, "/v1/")
	if !found {
		return "", uuid.Nil, "", "", "", false
	}

	// Now parse: organizations/{org}/users/{user}/conversations/{conv}/artifacts/{art}/versions/{ver}
	parts := strings.Split(path, "/")
	if len(parts) != 10 ||
		parts[0] != "organizations" ||
		parts[2] != "users" ||
		parts[4] != "conversations" ||
		parts[6] != "artifacts" ||
		parts[8] != "versions" {
		return "", uuid.Nil, "", "", "", false
	}
	uid, parseErr := uuid.Parse(parts[3])
	if parseErr != nil {
		return "", uuid.Nil, "", "", "", false
	}
	return parts[1], uid, parts[5], parts[7], parts[9], true
}
