package aichat

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/server"
)

// ContentHandler serves inline artifact version content over HTTP.
// Asset-backed versions are not served here — clients use the asset system.
type ContentHandler struct {
	queries db.Querier
	logger  *slog.Logger
}

// NewContentHandler creates a new content handler.
func NewContentHandler(queries db.Querier, logger *slog.Logger) *ContentHandler {
	return &ContentHandler{queries: queries, logger: logger}
}

func (h *ContentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	uid, ok := server.AuthenticatedUID(ctx)
	if !ok || uid == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Parse path: /v1/organizations/{org}/conversations/{conv}/artifacts/{art}/versions/{ver}:content
	orgName, convName, artName, verName, ok := parseContentPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}

	// Resolve through the resource hierarchy.
	org, err := h.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	conv, err := h.queries.GetConversationByName(ctx, db.GetConversationByNameParams{
		OrgID: org.ID,
		Name:  convName,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// Verify the caller owns the conversation.
	if conv.CreatedBy != uid {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	art, err := h.queries.GetArtifactByName(ctx, db.GetArtifactByNameParams{
		ConversationID: conv.ID,
		Name:           artName,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}

	row, err := h.queries.GetArtifactVersionForContent(ctx, db.GetArtifactVersionForContentParams{
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

// parseContentPath extracts org, conv, artifact, version from:
// /v1/organizations/{org}/conversations/{conv}/artifacts/{art}/versions/{ver}:content
func parseContentPath(path string) (org, conv, art, ver string, ok bool) {
	// Strip ":content" suffix.
	path, found := strings.CutSuffix(path, ":content")
	if !found {
		return "", "", "", "", false
	}

	// Strip leading "/v1/" prefix.
	path, found = strings.CutPrefix(path, "/v1/")
	if !found {
		return "", "", "", "", false
	}

	// Now parse: organizations/{org}/conversations/{conv}/artifacts/{art}/versions/{ver}
	parts := strings.Split(path, "/")
	if len(parts) != 8 ||
		parts[0] != "organizations" ||
		parts[2] != "conversations" ||
		parts[4] != "artifacts" ||
		parts[6] != "versions" {
		return "", "", "", "", false
	}
	return parts[1], parts[3], parts[5], parts[7], true
}
