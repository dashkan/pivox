package aichat

import (
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// peerUserID is the path-bound user-uuid used by tests where the
// caller is impersonating a path that points at a different user
// (the "wrong owner" case the post-Phase-7 ownership check exists
// to catch).
var peerUserID = uuid.MustParse("0192a000-0099-7000-8000-000000000099")

func peerConvPath(convName string) string {
	return "organizations/acme/users/" + peerUserID.String() + "/conversations/" + convName
}

func peerMessagePath(conv, msg string) string {
	return peerConvPath(conv) + "/messages/" + msg
}

func peerArtifactPath(conv, art string) string {
	return peerConvPath(conv) + "/artifacts/" + art
}

func peerArtifactVersionPath(conv, art, ver string) string {
	return peerArtifactPath(conv, art) + "/versions/" + ver
}

// peerConversation returns a fixture conversation whose creator is
// peerUserID — i.e. the row the path resolves to but that the
// caller (fixedUserID) is NOT the creator of.
func peerConversation(orgID uuid.UUID, name string) db.AiConversation {
	return db.AiConversation{
		ID:         uuid.New(),
		OrgID:      orgID,
		CreatedBy:  peerUserID,
		Name:       name,
		CreateTime: time.Now(),
		UpdateTime: time.Now(),
		Etag:       "etag",
	}
}

// --- Path-vs-row creator mismatch (forged-path defense) ---

func TestGetConversation_PathRowMismatch(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	// Path claims peerUserID owns conv1 but the row says someone else.
	row := peerConversation(org.ID, "conv1")
	row.CreatedBy = uuid.MustParse("0192a000-1111-7000-8000-000000001111")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.GetConversation(ctx, &aiv1.GetConversationRequest{
		Name: peerConvPath("conv1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code(), "path-vs-row mismatch must surface NotFound, not leak ownership")
}

// --- Caller-vs-path checks across Get/List handlers (no audit bypass) ---
//
// All Get/List handlers route through resolveConversation +
// verifyOwnerOrAllPerm. With nil permission resolver, the audit
// bypass disables and any caller != pathUser must get NotFound.

func TestGetConversation_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.GetConversation(ctx, &aiv1.GetConversationRequest{
		Name: peerConvPath("conv1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetMessage_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.GetMessage(ctx, &aiv1.GetMessageRequest{
		Name: peerMessagePath("conv1", "msg1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestListMessages_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.ListMessages(ctx, &aiv1.ListMessagesRequest{
		Parent: peerConvPath("conv1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetArtifact_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.GetArtifact(ctx, &aiv1.GetArtifactRequest{
		Name: peerArtifactPath("conv1", "art1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestListArtifacts_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.ListArtifacts(ctx, &aiv1.ListArtifactsRequest{
		Parent: peerConvPath("conv1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestGetArtifactVersion_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.GetArtifactVersion(ctx, &aiv1.GetArtifactVersionRequest{
		Name: peerArtifactVersionPath("conv1", "art1", "v1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

func TestListConversations_CallerNotPathUser_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := NewServer(nil, q, nil, nil, nil, nil, slog.Default())

	org := testOrg()
	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.ListConversations(ctx, &aiv1.ListConversationsRequest{
		Parent: "organizations/acme/users/" + peerUserID.String(),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

// --- Audit bypass: caller carries `*All` perm and CAN reach a peer's row ---
//
// These tests exercise the path that was entirely uncovered before:
// admin/owner audits a peer's conversation by carrying the
// `ai.conversations.readAll` (or `deleteAll`) permission. The
// permission resolver returns `admin` as an effective role; `Has`
// checks that role against the perm.

func TestGetConversation_AuditBypass_AdminWithReadAll(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	srv := NewServer(nil, q, nil, nil, nil, resolver, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID:              org.ID,
		FirebaseIdentityID: fixedUserID,
	}).Return([]string{permission.RoleAdmin}, nil)

	ctx := authenticatedCtx("caller")
	resp, err := srv.GetConversation(ctx, &aiv1.GetConversationRequest{
		Name: peerConvPath("conv1"),
	})
	require.NoError(t, err)
	assert.Equal(t, peerConvPath("conv1"), resp.GetName(),
		"audit-bypass must return the peer's conversation, not the caller's path")
}

func TestDeleteConversation_AuditBypass_OwnerWithDeleteAll(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	srv := NewServer(nil, q, nil, nil, nil, resolver, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)
	// Only owner (not admin) carries deleteAll — the audit-class
	// permission is locked tighter than read for legal/cleanup use.
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID:              org.ID,
		FirebaseIdentityID: fixedUserID,
	}).Return([]string{permission.RoleOwner}, nil)
	q.On("DeleteConversation", mock.Anything, row.ID).Return(nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.DeleteConversation(ctx, &aiv1.DeleteConversationRequest{
		Name: peerConvPath("conv1"),
	})
	require.NoError(t, err, "owner must be able to delete a peer's conversation via deleteAll")
}

func TestDeleteConversation_AdminCannotDeletePeer(t *testing.T) {
	// Admins do NOT carry deleteAll — only owners do. Without that
	// perm, deleting a peer's conversation must surface NotFound.
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	srv := NewServer(nil, q, nil, nil, nil, resolver, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)
	q.On("GetEffectiveOrgRoles", mock.Anything, db.GetEffectiveOrgRolesParams{
		OrgID:              org.ID,
		FirebaseIdentityID: fixedUserID,
	}).Return([]string{permission.RoleAdmin}, nil)

	ctx := authenticatedCtx("caller")
	_, err := srv.DeleteConversation(ctx, &aiv1.DeleteConversationRequest{
		Name: peerConvPath("conv1"),
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}

// --- Update / Summarize disable audit bypass entirely (creator-only) ---

func TestUpdateConversation_OwnerCannotEditPeer(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := permission.NewResolver(q)
	srv := NewServer(nil, q, nil, nil, nil, resolver, slog.Default())

	org := testOrg()
	row := peerConversation(org.ID, "conv1")

	q.On("GetOrganizationByName", mock.Anything, "acme").Return(org, nil)
	q.On("GetConversationByName", mock.Anything, mock.Anything).Return(row, nil)
	// Even an owner shouldn't be able to UPDATE another user's
	// conversation — the audit-class perms cover read/delete only.
	// Update passes allPerm="" so the resolver isn't consulted, but
	// we still wire it in case a future change adds a perm.

	ctx := authenticatedCtx("caller")
	_, err := srv.UpdateConversation(ctx, &aiv1.UpdateConversationRequest{
		Conversation: &aiv1.Conversation{
			Name:  peerConvPath("conv1"),
			Title: "hijacked",
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
	})
	require.Error(t, err)
	st, _ := status.FromError(err)
	assert.Equal(t, codes.NotFound, st.Code())
}
