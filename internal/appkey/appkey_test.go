package appkey

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey returns a deterministic 32-byte hex key suitable for NewFromHex.
func testKey() string {
	return strings.Repeat("ab", 32) // 64 hex chars = 32 bytes
}

func TestNewFromHex_Valid(t *testing.T) {
	c, err := NewFromHex(testKey())
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNewFromHex_WrongLength(t *testing.T) {
	_, err := NewFromHex("abcd") // 2 bytes, not 32
	require.Error(t, err)
	assert.Contains(t, err.Error(), "32 bytes")
}

func TestNewFromHex_InvalidHex(t *testing.T) {
	_, err := NewFromHex(strings.Repeat("zz", 32))
	require.Error(t, err)
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	c, err := NewFromHex(testKey())
	require.NoError(t, err)

	plaintext := []byte("hello world")
	tok, err := c.Encrypt(plaintext)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	got, err := c.Decrypt(tok)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(plaintext, got))
}

func TestEncryptDecrypt_EmptyPlaintext(t *testing.T) {
	c, err := NewFromHex(testKey())
	require.NoError(t, err)

	tok, err := c.Encrypt([]byte{})
	require.NoError(t, err)

	got, err := c.Decrypt(tok)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestEncrypt_NondeterministicAcrossCalls(t *testing.T) {
	// Same plaintext encrypted twice → different tokens, because a fresh
	// nonce is used each time. Essential for semantic security.
	c, err := NewFromHex(testKey())
	require.NoError(t, err)

	tok1, _ := c.Encrypt([]byte("same"))
	tok2, _ := c.Encrypt([]byte("same"))
	assert.NotEqual(t, tok1, tok2)
}

func TestDecrypt_TamperedCiphertext(t *testing.T) {
	c, err := NewFromHex(testKey())
	require.NoError(t, err)

	tok, _ := c.Encrypt([]byte("hello"))
	// Flip a byte in the middle of the token — GCM tag verification must fail.
	raw, _ := decodeToken(tok)
	raw[len(raw)/2] ^= 0xff
	bad := encodeToken(raw)

	_, err = c.Decrypt(bad)
	require.Error(t, err)
}

func TestDecrypt_WrongKey(t *testing.T) {
	a, _ := NewFromHex(testKey())
	b, _ := NewFromHex(strings.Repeat("cd", 32))

	tok, _ := a.Encrypt([]byte("secret"))
	_, err := b.Decrypt(tok)
	require.Error(t, err)
}

func TestDecrypt_MalformedToken(t *testing.T) {
	c, _ := NewFromHex(testKey())

	_, err := c.Decrypt("not-a-real-token")
	require.Error(t, err)

	_, err = c.Decrypt("")
	require.Error(t, err)
}

func TestDecrypt_UnknownVersion(t *testing.T) {
	// Token format: [version byte][nonce][ciphertext+tag]. Decrypt must
	// reject unknown versions so future rotation doesn't silently succeed.
	c, _ := NewFromHex(testKey())

	// Build a fake token with a bogus version byte.
	tok, _ := c.Encrypt([]byte("x"))
	raw, _ := decodeToken(tok)
	raw[0] = 0xff
	bad := encodeToken(raw)

	_, err := c.Decrypt(bad)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
}

func TestEncodeJSON_DecodeJSON_RoundTrip(t *testing.T) {
	c, _ := NewFromHex(testKey())

	in := map[string]any{"id": "abc-123", "ct": "2026-04-17T00:00:00Z"}
	tok, err := c.EncodeJSON(in)
	require.NoError(t, err)

	var out map[string]any
	err = c.DecodeJSON(tok, &out)
	require.NoError(t, err)
	assert.Equal(t, in["id"], out["id"])
	assert.Equal(t, in["ct"], out["ct"])
}

func TestDecodeJSON_TamperedToken(t *testing.T) {
	c, _ := NewFromHex(testKey())

	tok, _ := c.EncodeJSON(map[string]any{"x": "y"})
	raw, _ := decodeToken(tok)
	raw[len(raw)-1] ^= 0x01
	bad := encodeToken(raw)

	var out map[string]any
	err := c.DecodeJSON(bad, &out)
	require.Error(t, err)
}

func TestNewFromEnv_WithKey(t *testing.T) {
	t.Setenv("PIVOX_APP_KEY", testKey())
	c, err := NewFromEnv()
	require.NoError(t, err)
	require.NotNil(t, c)
}

// In dev builds we fall back to a random per-process key and expect a log
// warning. In prod builds we fatal. The build-tagged behaviors are tested
// in dedicated files (appkey_dev_test.go / appkey_prod_test.go).

func TestHex_RoundTripSanity(t *testing.T) {
	// Sanity: testKey() is 32 bytes when hex-decoded.
	b, err := hex.DecodeString(testKey())
	require.NoError(t, err)
	assert.Len(t, b, 32)
}
