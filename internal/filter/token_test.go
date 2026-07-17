package filter

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.einride.tech/aip/filtering"

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

	got, err := DecodePageToken(c, tok)
	require.NoError(t, err)
	assert.Equal(t, id, got)
}

func TestEncodeNextPageToken_NilCodec(t *testing.T) {
	_, err := EncodeNextPageToken(nil, uuid.New())
	require.Error(t, err)
}

func TestDecodePageToken_Empty(t *testing.T) {
	c := newTestCodec(t)
	got, err := DecodePageToken(c, "")
	require.NoError(t, err)
	assert.Equal(t, uuid.Nil, got)
}

func TestDecodePageToken_NilCodec(t *testing.T) {
	_, err := DecodePageToken(nil, "some-token")
	require.Error(t, err)
}

func TestDecodePageToken_Tampered(t *testing.T) {
	c := newTestCodec(t)
	tok, _ := EncodeNextPageToken(c, uuid.New())
	// Flip final byte.
	bad := tok[:len(tok)-1] + string([]byte{tok[len(tok)-1] ^ 0x01})
	_, err := DecodePageToken(c, bad)
	require.Error(t, err)
}

func TestDecodePageToken_WrongKey(t *testing.T) {
	c1 := newTestCodec(t)
	c2, _ := appkey.NewFromHex(strings.Repeat("cd", 32))

	tok, _ := EncodeNextPageToken(c1, uuid.New())
	_, err := DecodePageToken(c2, tok)
	require.Error(t, err)
}

func TestDecodePageToken_PlainUUIDRejected(t *testing.T) {
	// Client sending a plain UUID (pre-migration token or hand-crafted) must
	// fail — we never want to accept non-encrypted cursors once the codec is
	// wired.
	c := newTestCodec(t)
	_, err := DecodePageToken(c, uuid.New().String())
	require.Error(t, err)
}

func TestEncodeDecodeCursor_IDOnlyRoundTrip(t *testing.T) {
	c := newTestCodec(t)
	id := uuid.New()
	plan := OrderByPlan{} // default id ordering

	tok, err := EncodeCursor(c, plan, "", id)
	require.NoError(t, err)
	require.NotEmpty(t, tok)

	got, err := DecodeCursor(c, plan, tok)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, id, got.ID)
	assert.Nil(t, got.SortValue, "id-only cursor carries no sort value")
}

func TestEncodeDecodeCursor_CompoundStringRoundTrip(t *testing.T) {
	c := newTestCodec(t)
	id := uuid.New()
	plan := OrderByPlan{Field: "displayName", Column: "display_name", Type: filtering.TypeString}

	tok, err := EncodeCursor(c, plan, "Acme Hub", id)
	require.NoError(t, err)

	got, err := DecodeCursor(c, plan, tok)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, "Acme Hub", got.SortValue)
}

func TestEncodeDecodeCursor_CompoundTimestampRoundTrip(t *testing.T) {
	c := newTestCodec(t)
	id := uuid.New()
	plan := OrderByPlan{Field: "createTime", Column: "create_time", Type: filtering.TypeTimestamp}

	// Microsecond precision — the Postgres timestamptz resolution the boundary
	// must preserve exactly for a correct keyset.
	ts := time.Date(2026, 7, 15, 12, 34, 56, 789000000, time.UTC)
	tok, err := EncodeCursor(c, plan, ts.Format(time.RFC3339Nano), id)
	require.NoError(t, err)

	got, err := DecodeCursor(c, plan, tok)
	require.NoError(t, err)
	require.NotNil(t, got)
	gotTS, ok := got.SortValue.(time.Time)
	require.True(t, ok, "timestamp cursor decodes to time.Time, got %T", got.SortValue)
	assert.True(t, ts.Equal(gotTS), "timestamp round-trips exactly: want %s got %s", ts, gotTS)
}

func TestEncodeDecodeCursor_CompoundIntRoundTrip(t *testing.T) {
	c := newTestCodec(t)
	id := uuid.New()
	plan := OrderByPlan{Field: "sizeBytes", Column: "size_bytes", Type: filtering.TypeInt}

	// A BIGINT sort column must resume as an int64, not a text literal — pgx's
	// int8 codec rejects a Go string bound against the bigint column.
	const size int64 = 9_223_372_036_854_775_806 // near math.MaxInt64
	tok, err := EncodeCursor(c, plan, strconv.FormatInt(size, 10), id)
	require.NoError(t, err)

	got, err := DecodeCursor(c, plan, tok)
	require.NoError(t, err)
	require.NotNil(t, got)
	gotN, ok := got.SortValue.(int64)
	require.True(t, ok, "int cursor decodes to int64, got %T", got.SortValue)
	assert.Equal(t, size, gotN, "int64 round-trips exactly")
}

func TestDecodeCursor_BadIntCursorRejected(t *testing.T) {
	c := newTestCodec(t)
	plan := OrderByPlan{Field: "sizeBytes", Column: "size_bytes", Type: filtering.TypeInt}
	// A non-numeric sort value in the token (tampered/forged) must be rejected,
	// not silently coerced.
	tok, err := EncodeCursor(c, plan, "not-a-number", uuid.New())
	require.NoError(t, err)
	_, err = DecodeCursor(c, plan, tok)
	require.Error(t, err)
}

func TestDecodeCursor_EmptyTokenIsNilCursor(t *testing.T) {
	c := newTestCodec(t)
	got, err := DecodeCursor(c, OrderByPlan{Field: "displayName"}, "")
	require.NoError(t, err)
	assert.Nil(t, got, "empty token means first page")
}

func TestDecodeCursor_TamperedCompoundRejected(t *testing.T) {
	c := newTestCodec(t)
	plan := OrderByPlan{Field: "displayName", Type: filtering.TypeString}
	tok, err := EncodeCursor(c, plan, "x", uuid.New())
	require.NoError(t, err)
	bad := tok[:len(tok)-1] + string([]byte{tok[len(tok)-1] ^ 0x01})
	_, err = DecodeCursor(c, plan, bad)
	require.Error(t, err)
}
