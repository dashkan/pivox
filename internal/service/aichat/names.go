package aichat

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// Resource path shape (Phase 7 — AI chat re-parent under user):
//
//   organizations/{org}/users/{user}/conversations/{conv}[/messages/{msg}|/artifacts/{art}[/versions/{ver}]]
//
// `{user}` is the Pivox user UUID = `firebase_identities.id` — the
// same identifier the auth interceptor extracts from the
// `pivox_user_id` token claim and exposes as
// `server.MustPivoxUserID(ctx)`. Strict UUID parse — no `me`
// sentinel, matches every other user-rooted path.

// parseConversationName parses
// "organizations/{org}/users/{user-uuid}/conversations/{conversation}".
func parseConversationName(name string) (org string, user uuid.UUID, conv string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 ||
		parts[0] != "organizations" ||
		parts[2] != "users" ||
		parts[4] != "conversations" {
		return "", uuid.Nil, "", fmt.Errorf("invalid conversation name %q", name)
	}
	uid, parseErr := uuid.Parse(parts[3])
	if parseErr != nil {
		return "", uuid.Nil, "", fmt.Errorf("invalid user uuid in conversation name %q: %v", name, parseErr)
	}
	return parts[1], uid, parts[5], nil
}

// parseConversationParent parses
// "organizations/{org}/users/{user-uuid}".
func parseConversationParent(parent string) (org string, user uuid.UUID, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "users" {
		return "", uuid.Nil, fmt.Errorf("invalid parent %q: expected organizations/{org}/users/{user}", parent)
	}
	uid, parseErr := uuid.Parse(parts[3])
	if parseErr != nil {
		return "", uuid.Nil, fmt.Errorf("invalid user uuid in parent %q: %v", parent, parseErr)
	}
	return parts[1], uid, nil
}

// parseOrgScope extracts the org slug from any path that starts with
// "organizations/{org}/...". Used by GenerateContent /
// StreamGenerateContent — their `parent` field stays org-scoped
// (the `conversation` field, when supplied, carries the user
// segment that the conversation handler validates).
func parseOrgScope(parent string) (string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) < 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", fmt.Errorf("invalid parent %q: expected organizations/{org}", parent)
	}
	return parts[1], nil
}

// parseMessageName parses
// "organizations/{org}/users/{user-uuid}/conversations/{conv}/messages/{msg}".
func parseMessageName(name string) (org string, user uuid.UUID, conv, msg string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 8 ||
		parts[0] != "organizations" ||
		parts[2] != "users" ||
		parts[4] != "conversations" ||
		parts[6] != "messages" {
		return "", uuid.Nil, "", "", fmt.Errorf("invalid message name %q", name)
	}
	uid, parseErr := uuid.Parse(parts[3])
	if parseErr != nil {
		return "", uuid.Nil, "", "", fmt.Errorf("invalid user uuid in message name %q: %v", name, parseErr)
	}
	return parts[1], uid, parts[5], parts[7], nil
}

func parseMessageParent(parent string) (org string, user uuid.UUID, conv string, err error) {
	return parseConversationName(parent)
}

// parseArtifactName parses
// "organizations/{org}/users/{user-uuid}/conversations/{conv}/artifacts/{art}".
func parseArtifactName(name string) (org string, user uuid.UUID, conv, art string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 8 ||
		parts[0] != "organizations" ||
		parts[2] != "users" ||
		parts[4] != "conversations" ||
		parts[6] != "artifacts" {
		return "", uuid.Nil, "", "", fmt.Errorf("invalid artifact name %q", name)
	}
	uid, parseErr := uuid.Parse(parts[3])
	if parseErr != nil {
		return "", uuid.Nil, "", "", fmt.Errorf("invalid user uuid in artifact name %q: %v", name, parseErr)
	}
	return parts[1], uid, parts[5], parts[7], nil
}

func parseArtifactParent(parent string) (org string, user uuid.UUID, conv string, err error) {
	return parseConversationName(parent)
}

// parseArtifactVersionName parses
// "organizations/{org}/users/{user-uuid}/conversations/{conv}/artifacts/{art}/versions/{ver}".
func parseArtifactVersionName(name string) (org string, user uuid.UUID, conv, art, ver string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 10 ||
		parts[0] != "organizations" ||
		parts[2] != "users" ||
		parts[4] != "conversations" ||
		parts[6] != "artifacts" ||
		parts[8] != "versions" {
		return "", uuid.Nil, "", "", "", fmt.Errorf("invalid artifact version name %q", name)
	}
	uid, parseErr := uuid.Parse(parts[3])
	if parseErr != nil {
		return "", uuid.Nil, "", "", "", fmt.Errorf("invalid user uuid in artifact version name %q: %v", name, parseErr)
	}
	return parts[1], uid, parts[5], parts[7], parts[9], nil
}

func parseArtifactVersionParent(parent string) (org string, user uuid.UUID, conv, art string, err error) {
	return parseArtifactName(parent)
}

func buildConversationName(org string, user uuid.UUID, conv string) string {
	return fmt.Sprintf("organizations/%s/users/%s/conversations/%s", org, user, conv)
}

func buildMessageName(org string, user uuid.UUID, conv, msg string) string {
	return fmt.Sprintf("organizations/%s/users/%s/conversations/%s/messages/%s", org, user, conv, msg)
}

func buildArtifactName(org string, user uuid.UUID, conv, art string) string {
	return fmt.Sprintf("organizations/%s/users/%s/conversations/%s/artifacts/%s", org, user, conv, art)
}

func buildArtifactVersionName(org string, user uuid.UUID, conv, art, ver string) string {
	return fmt.Sprintf("organizations/%s/users/%s/conversations/%s/artifacts/%s/versions/%s", org, user, conv, art, ver)
}
