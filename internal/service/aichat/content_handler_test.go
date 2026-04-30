package aichat

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// stubResolver lets tests stand in for *Server's
// resolveConversation/getArtifact... methods without dragging in the
// model, codec, filters, etc. — the unit tests focus on the HTTP
// handler contract, not the gRPC ownership wiring (which has its
// own coverage in conversations_test.go and friends).
type stubResolver struct {
	conv     db.AiConversation
	convErr  error
	art      db.AiArtifact
	artErr   error
	row      db.GetArtifactVersionForContentRow
	rowErr   error
	convCall struct {
		orgName  string
		pathUser uuid.UUID
		convName string
		allPerm  string
		called   bool
	}
}

func (s *stubResolver) resolveConversation(_ context.Context, orgName string, pathUser uuid.UUID, convName, allPerm string) (db.AiConversation, error) {
	s.convCall.orgName = orgName
	s.convCall.pathUser = pathUser
	s.convCall.convName = convName
	s.convCall.allPerm = allPerm
	s.convCall.called = true
	return s.conv, s.convErr
}

func (s *stubResolver) getArtifactByName(_ context.Context, _ db.GetArtifactByNameParams) (db.AiArtifact, error) {
	return s.art, s.artErr
}

func (s *stubResolver) getArtifactVersionForContent(_ context.Context, _ db.GetArtifactVersionForContentParams) (db.GetArtifactVersionForContentRow, error) {
	return s.row, s.rowErr
}

func newContentHandlerForTest(stub *stubResolver) *ContentHandler {
	return &ContentHandler{resolver: stub, logger: slog.Default()}
}

func contentPath(user uuid.UUID) string {
	return "/v1/organizations/acme/users/" + user.String() +
		"/conversations/conv1/artifacts/art1/versions/v1:content"
}

func TestContentHandler_InlineHappyPath(t *testing.T) {
	user := fixedUserID
	artID := uuid.New()
	stub := &stubResolver{
		conv: db.AiConversation{ID: uuid.New(), CreatedBy: user, Name: "conv1"},
		art:  db.AiArtifact{ID: artID, Name: "art1", CreateTime: time.Now(), UpdateTime: time.Now()},
		row: db.GetArtifactVersionForContentRow{
			ID:                uuid.New(),
			ArtifactID:        artID,
			InlineData:        []byte("print('hello')"),
			InlineContentType: pgtype.Text{String: "text/x-python", Valid: true},
			InlineSizeBytes:   pgtype.Int8{Int64: 14, Valid: true},
		},
	}
	h := newContentHandlerForTest(stub)

	req := httptest.NewRequest("GET", contentPath(user), nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/x-python", w.Header().Get("Content-Type"))
	assert.Equal(t, "14", w.Header().Get("Content-Length"))
	assert.Equal(t, `"v1"`, w.Header().Get("ETag"))
	assert.Equal(t, "print('hello')", w.Body.String())

	// resolveConversation was called with the audit perm so admins can
	// fetch a peer's content for legal/audit. The check itself happens
	// inside resolveConversation (covered by conversations_test.go).
	require.True(t, stub.convCall.called)
	assert.Equal(t, "acme", stub.convCall.orgName)
	assert.Equal(t, user, stub.convCall.pathUser)
	assert.Equal(t, "conv1", stub.convCall.convName)
	assert.Equal(t, "ai.conversations.readAll", stub.convCall.allPerm)
}

func TestContentHandler_AssetBacked(t *testing.T) {
	user := fixedUserID
	stub := &stubResolver{
		conv: db.AiConversation{ID: uuid.New(), CreatedBy: user},
		art:  db.AiArtifact{ID: uuid.New(), CreateTime: time.Now(), UpdateTime: time.Now()},
		row: db.GetArtifactVersionForContentRow{
			ID:               uuid.New(),
			AssetVersionName: pgtype.Text{String: "organizations/acme/spaces/p1/assets/a1/versions/1", Valid: true},
		},
	}
	h := newContentHandlerForTest(stub)

	req := httptest.NewRequest("GET", contentPath(user), nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "asset-backed")
}

func TestContentHandler_NotFoundPropagated(t *testing.T) {
	// resolveConversation surfaces NotFound (existence-probe defense) for
	// missing conversations OR for path-vs-row creator mismatches. Either
	// way, the HTTP handler must return 404, not leak a different code.
	user := uuid.New()
	stub := &stubResolver{convErr: apierr.NotFound("Conversation", "x")}
	h := newContentHandlerForTest(stub)

	req := httptest.NewRequest("GET", contentPath(user), nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestContentHandler_PermissionDeniedAs403(t *testing.T) {
	user := uuid.New()
	stub := &stubResolver{convErr: apierr.PermissionDenied("not yours")}
	h := newContentHandlerForTest(stub)

	req := httptest.NewRequest("GET", contentPath(user), nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestContentHandler_IfNoneMatch(t *testing.T) {
	user := fixedUserID
	artID := uuid.New()
	stub := &stubResolver{
		conv: db.AiConversation{ID: uuid.New(), CreatedBy: user},
		art:  db.AiArtifact{ID: artID, CreateTime: time.Now(), UpdateTime: time.Now()},
		row: db.GetArtifactVersionForContentRow{
			ID:                uuid.New(),
			ArtifactID:        artID,
			InlineData:        []byte("data"),
			InlineContentType: pgtype.Text{String: "text/plain", Valid: true},
			InlineSizeBytes:   pgtype.Int8{Int64: 4, Valid: true},
		},
	}
	h := newContentHandlerForTest(stub)

	req := httptest.NewRequest("GET", contentPath(user), nil)
	req.Header.Set("If-None-Match", `"v1"`)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotModified, w.Code)
}

func TestParseContentPath(t *testing.T) {
	user := uuid.New()
	tests := []struct {
		name    string
		path    string
		wantOrg string
		// Empty wantUser means the path should fail to parse.
		wantUser uuid.UUID
		wantConv string
		wantArt  string
		wantVer  string
		wantOK   bool
	}{
		{
			name:     "valid",
			path:     "/v1/organizations/acme/users/" + user.String() + "/conversations/c1/artifacts/a1/versions/v1:content",
			wantOrg:  "acme",
			wantUser: user,
			wantConv: "c1",
			wantArt:  "a1",
			wantVer:  "v1",
			wantOK:   true,
		},
		{
			name: "no suffix",
			path: "/v1/organizations/acme/users/" + user.String() + "/conversations/c1/artifacts/a1/versions/v1",
		},
		{
			name: "bad prefix",
			path: "/organizations/acme/users/" + user.String() + "/conversations/c1/artifacts/a1/versions/v1:content",
		},
		{
			name: "old shape (no users segment)",
			path: "/v1/organizations/acme/conversations/c1/artifacts/a1/versions/v1:content",
		},
		{
			name: "non-uuid user",
			path: "/v1/organizations/acme/users/not-a-uuid/conversations/c1/artifacts/a1/versions/v1:content",
		},
		{
			name: "short path",
			path: "/v1/organizations/acme/conversations/c1:content",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, gotUser, conv, art, ver, ok := parseContentPath(tt.path)
			require.Equal(t, tt.wantOK, ok, "parseContentPath ok mismatch")
			if !ok {
				return
			}
			assert.Equal(t, tt.wantOrg, org)
			assert.Equal(t, tt.wantUser, gotUser)
			assert.Equal(t, tt.wantConv, conv)
			assert.Equal(t, tt.wantArt, art)
			assert.Equal(t, tt.wantVer, ver)
		})
	}
}
