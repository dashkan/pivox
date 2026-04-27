package aichat

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

func TestContentHandler_InlineHappyPath(t *testing.T) {
	q := new(mocks.MockQuerier)
	h := NewContentHandler(q, slog.Default())

	org := testOrg()
	conv := testConversation(org.ID, "user1")
	artID := uuid.New()

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, db.GetConversationByNameParams{
		OrgID: org.ID, Name: "conv1",
	}).Return(conv, nil)
	q.On("GetArtifactByName", mock.Anything, db.GetArtifactByNameParams{
		ConversationID: conv.ID, Name: "art1",
	}).Return(db.AiArtifact{ID: artID, ConversationID: conv.ID, Name: "art1", CreateTime: time.Now(), UpdateTime: time.Now()}, nil)
	q.On("GetArtifactVersionForContent", mock.Anything, db.GetArtifactVersionForContentParams{
		ArtifactID: artID, Name: "v1",
	}).Return(db.GetArtifactVersionForContentRow{
		ID:                uuid.New(),
		ArtifactID:        artID,
		InlineData:        []byte("print('hello')"),
		InlineContentType: pgtype.Text{String: "text/x-python", Valid: true},
		InlineSizeBytes:   pgtype.Int8{Int64: 14, Valid: true},
	}, nil)

	req := httptest.NewRequest("GET",
		"/v1/organizations/acme/conversations/conv1/artifacts/art1/versions/v1:content", nil)
	req = req.WithContext(server.WithAuthenticatedUID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/x-python", w.Header().Get("Content-Type"))
	assert.Equal(t, "14", w.Header().Get("Content-Length"))
	assert.Equal(t, `"v1"`, w.Header().Get("ETag"))
	assert.Equal(t, "print('hello')", w.Body.String())
}

func TestContentHandler_AssetBacked(t *testing.T) {
	q := new(mocks.MockQuerier)
	h := NewContentHandler(q, slog.Default())

	org := testOrg()
	conv := testConversation(org.ID, "user1")
	artID := uuid.New()

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("GetArtifactByName", mock.Anything, mock.Anything).Return(
		db.AiArtifact{ID: artID, CreateTime: time.Now(), UpdateTime: time.Now()}, nil)
	q.On("GetArtifactVersionForContent", mock.Anything, mock.Anything).Return(db.GetArtifactVersionForContentRow{
		ID:               uuid.New(),
		ArtifactID:       artID,
		AssetVersionName: pgtype.Text{String: "organizations/acme/spaces/p1/assets/a1/versions/1", Valid: true},
	}, nil)

	req := httptest.NewRequest("GET",
		"/v1/organizations/acme/conversations/conv1/artifacts/art1/versions/v1:content", nil)
	req = req.WithContext(server.WithAuthenticatedUID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "asset-backed")
}

func TestContentHandler_Unauthorized(t *testing.T) {
	q := new(mocks.MockQuerier)
	h := NewContentHandler(q, slog.Default())

	req := httptest.NewRequest("GET",
		"/v1/organizations/acme/conversations/conv1/artifacts/art1/versions/v1:content", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestContentHandler_WrongUser(t *testing.T) {
	q := new(mocks.MockQuerier)
	h := NewContentHandler(q, slog.Default())

	org := testOrg()
	conv := testConversation(org.ID, "other-user")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)

	req := httptest.NewRequest("GET",
		"/v1/organizations/acme/conversations/conv1/artifacts/art1/versions/v1:content", nil)
	req = req.WithContext(server.WithAuthenticatedUID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestContentHandler_IfNoneMatch(t *testing.T) {
	q := new(mocks.MockQuerier)
	h := NewContentHandler(q, slog.Default())

	org := testOrg()
	conv := testConversation(org.ID, "user1")
	artID := uuid.New()

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(conv, nil)
	q.On("GetArtifactByName", mock.Anything, mock.Anything).Return(
		db.AiArtifact{ID: artID, CreateTime: time.Now(), UpdateTime: time.Now()}, nil)
	q.On("GetArtifactVersionForContent", mock.Anything, mock.Anything).Return(db.GetArtifactVersionForContentRow{
		ID:                uuid.New(),
		ArtifactID:        artID,
		InlineData:        []byte("data"),
		InlineContentType: pgtype.Text{String: "text/plain", Valid: true},
		InlineSizeBytes:   pgtype.Int8{Int64: 4, Valid: true},
	}, nil)

	req := httptest.NewRequest("GET",
		"/v1/organizations/acme/conversations/conv1/artifacts/art1/versions/v1:content", nil)
	req.Header.Set("If-None-Match", `"v1"`)
	req = req.WithContext(server.WithAuthenticatedUID(req.Context(), "user1"))
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotModified, w.Code)
}

func TestParseContentPath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		org    string
		conv   string
		art    string
		ver    string
		wantOK bool
	}{
		{"valid", "/v1/organizations/acme/conversations/c1/artifacts/a1/versions/v1:content",
			"acme", "c1", "a1", "v1", true},
		{"no suffix", "/v1/organizations/acme/conversations/c1/artifacts/a1/versions/v1",
			"", "", "", "", false},
		{"bad prefix", "/organizations/acme/conversations/c1/artifacts/a1/versions/v1:content",
			"", "", "", "", false},
		{"short path", "/v1/organizations/acme/conversations/c1:content",
			"", "", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, conv, art, ver, ok := parseContentPath(tt.path)
			require.Equal(t, tt.wantOK, ok)
			if ok {
				assert.Equal(t, tt.org, org)
				assert.Equal(t, tt.conv, conv)
				assert.Equal(t, tt.art, art)
				assert.Equal(t, tt.ver, ver)
			}
		})
	}
}
