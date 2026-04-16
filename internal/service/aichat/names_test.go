package aichat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseConversationName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		org     string
		conv    string
		wantErr bool
	}{
		{"valid", "organizations/acme/conversations/abc123", "acme", "abc123", false},
		{"too short", "organizations/acme", "", "", true},
		{"wrong prefix", "orgs/acme/conversations/abc", "", "", true},
		{"wrong middle", "organizations/acme/convos/abc", "", "", true},
		{"too many parts", "organizations/acme/conversations/abc/extra", "", "", true},
		{"empty", "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, conv, err := parseConversationName(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.org, org)
			assert.Equal(t, tt.conv, conv)
		})
	}
}

func TestParseConversationParent(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		org     string
		wantErr bool
	}{
		{"valid", "organizations/acme", "acme", false},
		{"wrong prefix", "orgs/acme", "", true},
		{"too many parts", "organizations/acme/extra", "", true},
		{"empty", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org, err := parseConversationParent(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.org, org)
		})
	}
}

func TestParseMessageName(t *testing.T) {
	org, conv, msg, err := parseMessageName("organizations/acme/conversations/c1/messages/m1")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "m1", msg)

	_, _, _, err = parseMessageName("invalid")
	require.Error(t, err)
}

func TestParseArtifactName(t *testing.T) {
	org, conv, art, err := parseArtifactName("organizations/acme/conversations/c1/artifacts/a1")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)

	_, _, _, err = parseArtifactName("invalid")
	require.Error(t, err)
}

func TestParseArtifactVersionName(t *testing.T) {
	org, conv, art, ver, err := parseArtifactVersionName("organizations/acme/conversations/c1/artifacts/a1/versions/v1")
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)
	assert.Equal(t, "v1", ver)

	_, _, _, _, err = parseArtifactVersionName("invalid")
	require.Error(t, err)

	_, _, _, _, err = parseArtifactVersionName("organizations/acme/conversations/c1/artifacts/a1/vers/v1")
	require.Error(t, err)
}

func TestBuildRoundTrip(t *testing.T) {
	// Conversation
	convName := buildConversationName("acme", "c1")
	org, conv, err := parseConversationName(convName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)

	// Message
	msgName := buildMessageName("acme", "c1", "m1")
	org, conv, msg, err := parseMessageName(msgName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "m1", msg)

	// Artifact
	artName := buildArtifactName("acme", "c1", "a1")
	org, conv, art, err := parseArtifactName(artName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)

	// Artifact version
	verName := buildArtifactVersionName("acme", "c1", "a1", "v1")
	org, conv, art, ver, err := parseArtifactVersionName(verName)
	require.NoError(t, err)
	assert.Equal(t, "acme", org)
	assert.Equal(t, "c1", conv)
	assert.Equal(t, "a1", art)
	assert.Equal(t, "v1", ver)
}
