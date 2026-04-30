package aichat

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testUserUUID = uuid.MustParse("0192a000-0002-7000-8000-000000000001")

func TestParseConversationName(t *testing.T) {
	uidSeg := testUserUUID.String()
	tests := []struct {
		name    string
		input   string
		org     string
		conv    string
		wantErr bool
	}{
		{"valid", "organizations/acme/users/" + uidSeg + "/conversations/abc123", "acme", "abc123", false},
		{"old shape (no users)", "organizations/acme/conversations/abc123", "", "", true},
		{"non-uuid user", "organizations/acme/users/notauuid/conversations/abc", "", "", true},
		{"empty", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, user, conv, err := parseConversationName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.org, org)
			assert.Equal(t, testUserUUID, user)
			assert.Equal(t, tt.conv, conv)
		})
	}
}

func TestParseConversationParent(t *testing.T) {
	uidSeg := testUserUUID.String()
	tests := []struct {
		name    string
		input   string
		org     string
		wantErr bool
	}{
		{"valid", "organizations/acme/users/" + uidSeg, "acme", false},
		{"old shape (no users)", "organizations/acme", "", true},
		{"non-uuid user", "organizations/acme/users/notauuid", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, user, err := parseConversationParent(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.org, org)
			assert.Equal(t, testUserUUID, user)
		})
	}
}

func TestParseMessageName(t *testing.T) {
	uidSeg := testUserUUID.String()
	org, user, conv, msg, err := parseMessageName("organizations/acme/users/" + uidSeg + "/conversations/c1/messages/m1")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "m1", msg)

	_, _, _, _, err = parseMessageName("invalid")
	require.Error(t, err)
}

func TestParseArtifactName(t *testing.T) {
	uidSeg := testUserUUID.String()
	org, user, conv, art, err := parseArtifactName("organizations/acme/users/" + uidSeg + "/conversations/c1/artifacts/a1")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)

	_, _, _, _, err = parseArtifactName("invalid")
	require.Error(t, err)
}

func TestParseArtifactVersionName(t *testing.T) {
	uidSeg := testUserUUID.String()
	org, user, conv, art, ver, err := parseArtifactVersionName("organizations/acme/users/" + uidSeg + "/conversations/c1/artifacts/a1/versions/v1")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)
	assert.Equal(t, "v1", ver)

	_, _, _, _, _, err = parseArtifactVersionName("invalid")
	require.Error(t, err)
}

func TestBuildRoundTrip(t *testing.T) {
	convName := buildConversationName("acme", testUserUUID, "c1")
	org, user, conv, err := parseConversationName(convName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)

	msgName := buildMessageName("acme", testUserUUID, "c1", "m1")
	org, user, conv, msg, err := parseMessageName(msgName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "m1", msg)

	artName := buildArtifactName("acme", testUserUUID, "c1", "a1")
	org, user, conv, art, err := parseArtifactName(artName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)

	verName := buildArtifactVersionName("acme", testUserUUID, "c1", "a1", "v1")
	org, user, conv, art, ver, err := parseArtifactVersionName(verName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, testUserUUID, user)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)
	assert.Equal(t, "v1", ver)
}
