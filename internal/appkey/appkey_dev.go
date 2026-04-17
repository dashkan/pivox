//go:build dev

package appkey

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
)

// newFromEnvImpl (dev): if PIVOX_APP_KEY is unset, generate a random
// per-process key and log a warning. This keeps local dev frictionless —
// tokens issued by a given process work within it, and restart simply
// invalidates any in-flight tokens (fine during development).
func newFromEnvImpl(keyHex string) (*Codec, error) {
	if keyHex == "" {
		buf := make([]byte, keyLen)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("appkey: generate dev key: %w", err)
		}
		slog.Warn("PIVOX_APP_KEY not set — using a random per-process key. Tokens invalidate on restart.")
		return NewFromHex(hex.EncodeToString(buf))
	}
	return NewFromHex(keyHex)
}
