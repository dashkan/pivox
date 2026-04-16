package aichat

import (
	"fmt"
	"strings"
)

// parseConversationName parses "organizations/{org}/conversations/{conversation}".
func parseConversationName(name string) (org, conv string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "conversations" {
		return "", "", fmt.Errorf("invalid conversation name %q", name)
	}
	return parts[1], parts[3], nil
}

// parseConversationParent parses "organizations/{organization}".
func parseConversationParent(parent string) (org string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 2 || parts[0] != "organizations" {
		return "", fmt.Errorf("invalid parent %q", parent)
	}
	return parts[1], nil
}

// parseMessageName parses "organizations/{org}/conversations/{conv}/messages/{msg}".
func parseMessageName(name string) (org, conv, msg string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "conversations" || parts[4] != "messages" {
		return "", "", "", fmt.Errorf("invalid message name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseMessageParent parses "organizations/{org}/conversations/{conv}".
func parseMessageParent(parent string) (org, conv string, err error) {
	return parseConversationName(parent)
}

// parseArtifactName parses "organizations/{org}/conversations/{conv}/artifacts/{art}".
func parseArtifactName(name string) (org, conv, art string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "conversations" || parts[4] != "artifacts" {
		return "", "", "", fmt.Errorf("invalid artifact name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseArtifactParent parses "organizations/{org}/conversations/{conv}".
func parseArtifactParent(parent string) (org, conv string, err error) {
	return parseConversationName(parent)
}

// parseArtifactVersionName parses "organizations/{org}/conversations/{conv}/artifacts/{art}/versions/{ver}".
func parseArtifactVersionName(name string) (org, conv, art, ver string, err error) {
	parts := strings.Split(name, "/")
	if len(parts) != 8 || parts[0] != "organizations" || parts[2] != "conversations" || parts[4] != "artifacts" || parts[6] != "versions" {
		return "", "", "", "", fmt.Errorf("invalid artifact version name %q", name)
	}
	return parts[1], parts[3], parts[5], parts[7], nil
}

// parseArtifactVersionParent parses "organizations/{org}/conversations/{conv}/artifacts/{art}".
func parseArtifactVersionParent(parent string) (org, conv, art string, err error) {
	return parseArtifactName(parent)
}

func buildConversationName(org, conv string) string {
	return fmt.Sprintf("organizations/%s/conversations/%s", org, conv)
}

func buildMessageName(org, conv, msg string) string {
	return fmt.Sprintf("organizations/%s/conversations/%s/messages/%s", org, conv, msg)
}

func buildArtifactName(org, conv, art string) string {
	return fmt.Sprintf("organizations/%s/conversations/%s/artifacts/%s", org, conv, art)
}

func buildArtifactVersionName(org, conv, art, ver string) string {
	return fmt.Sprintf("organizations/%s/conversations/%s/artifacts/%s/versions/%s", org, conv, art, ver)
}
