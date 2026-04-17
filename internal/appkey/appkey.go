// Package appkey provides a process-lifetime symmetric encryption codec
// for ephemeral, high-frequency encrypt/decrypt workloads (page tokens,
// signed URLs, short-lived caches) where a KMS round-trip per call would
// be too costly.
//
// The codec uses AES-256-GCM with a key loaded once at startup from the
// PIVOX_APP_KEY env var. In production the key MUST be stable across all
// instances behind the load balancer and across restarts, otherwise tokens
// issued by one process will fail to decrypt on another.
//
// This is the RIGHT choice for:
//   - Opaque page tokens (AIP-158)
//   - Short-lived signed redirect URLs
//   - In-memory cache entry encryption
//
// This is the WRONG choice for:
//   - Data at rest (PII, credentials, long-lived rows) — use crypto.Encryptor
//     which envelope-encrypts via KMS.
//   - Signing (authenticity without confidentiality) — use HMAC.
package appkey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// currentVersion tags every encoded token so future key-rotation schemes
// can be added without silently accepting old formats.
const currentVersion byte = 0x01

// keyLen is the required byte length of the AES-256 key.
const keyLen = 32

// Codec encrypts and decrypts short-lived tokens using AES-256-GCM.
// Safe for concurrent use.
type Codec struct {
	aead cipher.AEAD
}

// NewFromHex builds a Codec from a 32-byte key expressed as 64 hex chars.
func NewFromHex(keyHex string) (*Codec, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return nil, fmt.Errorf("appkey: invalid hex key: %w", err)
	}
	if len(key) != keyLen {
		return nil, fmt.Errorf("appkey: key must be %d bytes (got %d)", keyLen, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("appkey: new cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("appkey: new GCM: %w", err)
	}
	return &Codec{aead: aead}, nil
}

// Encrypt returns a URL-safe base64 token containing version || nonce || ct||tag.
func (c *Codec) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("appkey: generate nonce: %w", err)
	}
	ct := c.aead.Seal(nil, nonce, plaintext, nil)

	buf := make([]byte, 0, 1+len(nonce)+len(ct))
	buf = append(buf, currentVersion)
	buf = append(buf, nonce...)
	buf = append(buf, ct...)
	return encodeToken(buf), nil
}

// Decrypt reverses Encrypt. Returns an error if the token is malformed,
// tampered, encrypted with a different key, or uses an unknown version.
func (c *Codec) Decrypt(token string) ([]byte, error) {
	raw, err := decodeToken(token)
	if err != nil {
		return nil, fmt.Errorf("appkey: decode token: %w", err)
	}
	if len(raw) < 1+c.aead.NonceSize() {
		return nil, fmt.Errorf("appkey: token too short")
	}
	if raw[0] != currentVersion {
		return nil, fmt.Errorf("appkey: unknown token version %#x", raw[0])
	}
	nonceSize := c.aead.NonceSize()
	nonce := raw[1 : 1+nonceSize]
	ct := raw[1+nonceSize:]
	pt, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("appkey: decrypt: %w", err)
	}
	return pt, nil
}

// EncodeJSON marshals v to JSON and encrypts the result. Convenience wrapper
// for the common "encrypt a struct/map" case (e.g., pagination cursors).
func (c *Codec) EncodeJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("appkey: marshal json: %w", err)
	}
	return c.Encrypt(raw)
}

// DecodeJSON decrypts the token and unmarshals the plaintext JSON into out.
func (c *Codec) DecodeJSON(token string, out any) error {
	raw, err := c.Decrypt(token)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("appkey: unmarshal json: %w", err)
	}
	return nil
}

// encodeToken and decodeToken use URL-safe base64 without padding so tokens
// are safe to embed in query params and JSON without escaping.
func encodeToken(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeToken(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// NewFromEnv loads PIVOX_APP_KEY and builds a Codec. Absence/behavior when
// unset is build-tag specific — see appkey_dev.go / appkey_prod.go.
func NewFromEnv() (*Codec, error) {
	return newFromEnvImpl(os.Getenv("PIVOX_APP_KEY"))
}
