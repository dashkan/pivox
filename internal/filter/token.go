package filter

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/appkey"
)

// EncodeNextPageToken returns an opaque page token encoding the given row id.
// The token is encrypted with the app key so clients can't reverse-engineer
// the sort strategy or forge cursor positions.
func EncodeNextPageToken(codec *appkey.Codec, id uuid.UUID) (string, error) {
	if codec == nil {
		return "", fmt.Errorf("filter: page token codec is required")
	}
	return codec.Encrypt(id[:])
}

// decodeCursor turns an encrypted page_token back into a UUID string for SQL.
// Empty token → empty string (caller treats as "no cursor"). Unparseable or
// tampered token → error.
func decodeCursor(codec *appkey.Codec, token string) (string, error) {
	if token == "" {
		return "", nil
	}
	if codec == nil {
		return "", fmt.Errorf("filter: page token codec is required")
	}
	raw, err := codec.Decrypt(token)
	if err != nil {
		return "", fmt.Errorf("invalid page_token: %w", err)
	}
	if len(raw) != 16 {
		return "", fmt.Errorf("invalid page_token: expected 16 bytes, got %d", len(raw))
	}
	var id uuid.UUID
	copy(id[:], raw)
	return id.String(), nil
}
