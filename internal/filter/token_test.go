package filter

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/appkey"
)

func newTestCodec(t *testing.T) *appkey.Codec {
	t.Helper()
	c, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	require.NoError(t, err)
	return c
}

func TestEncodeNextPageToken_RoundTripsThroughDecode(t *testing.T) {
	c := newTestCodec(t)
	id := uuid.New()

	tok, err := EncodeNextPageToken(c, id)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	got, err := decodeCursor(c, tok)
	require.NoError(t, err)
	assert.Equal(t, id.String(), got)
}

func TestEncodeNextPageToken_NilCodec(t *testing.T) {
	_, err := EncodeNextPageToken(nil, uuid.New())
	require.Error(t, err)
}

func TestDecodeCursor_Empty(t *testing.T) {
	c := newTestCodec(t)
	got, err := decodeCursor(c, "")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestDecodeCursor_NilCodec(t *testing.T) {
	_, err := decodeCursor(nil, "some-token")
	require.Error(t, err)
}

func TestDecodeCursor_Tampered(t *testing.T) {
	c := newTestCodec(t)
	tok, _ := EncodeNextPageToken(c, uuid.New())
	// Flip final byte.
	bad := tok[:len(tok)-1] + string([]byte{tok[len(tok)-1] ^ 0x01})
	_, err := decodeCursor(c, bad)
	require.Error(t, err)
}

func TestDecodeCursor_WrongKey(t *testing.T) {
	c1 := newTestCodec(t)
	c2, _ := appkey.NewFromHex(strings.Repeat("cd", 32))

	tok, _ := EncodeNextPageToken(c1, uuid.New())
	_, err := decodeCursor(c2, tok)
	require.Error(t, err)
}

func TestDecodeCursor_PlainUUIDRejected(t *testing.T) {
	// Client sending a plain UUID (pre-migration token or hand-crafted) must
	// fail — we never want to accept non-encrypted cursors once the codec is
	// wired.
	c := newTestCodec(t)
	_, err := decodeCursor(c, uuid.New().String())
	require.Error(t, err)
}
