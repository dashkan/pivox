package filter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"

	"go.einride.tech/aip/filtering"

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

// DecodePageToken decodes an id-only page token into its row id. Empty token →
// uuid.Nil (caller treats as "no cursor"). Tampered/short token → error. This
// is the exported, uuid-typed sibling of decodeCursor, used by the
// compound-cursor keyset path (BuildListQuery / DecodeCursor).
func DecodePageToken(codec *appkey.Codec, token string) (uuid.UUID, error) {
	if token == "" {
		return uuid.Nil, nil
	}
	if codec == nil {
		return uuid.Nil, fmt.Errorf("filter: page token codec is required")
	}
	raw, err := codec.Decrypt(token)
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid page_token: %w", err)
	}
	if len(raw) != 16 {
		return uuid.Nil, fmt.Errorf("invalid page_token: expected 16 bytes, got %d", len(raw))
	}
	var id uuid.UUID
	copy(id[:], raw)
	return id, nil
}

// compoundCursor is the JSON payload of a compound page token: the primary
// sort column's value (as a string — timestamps use RFC3339Nano) plus the row
// id tiebreaker. It is encrypted with the app key, so clients can neither read
// the sort strategy nor forge a cursor. Both fields are re-bound as $N
// parameters on the next page (never string-interpolated), so an arbitrary
// decoded sort value can only ever be a literal operand.
type compoundCursor struct {
	Sort string    `json:"s"`
	ID   uuid.UUID `json:"i"`
}

// EncodeCursor builds the next-page token for a keyset list. For the default
// id-only ordering (plan.Field == "") it emits the simple 16-byte id token
// (sortValue is ignored); for a custom order_by it emits a compound token
// carrying both the sort value and the id. sortValue is the last returned
// row's value for plan.Field, formatted by the caller (timestamps as
// RFC3339Nano — see DecodeCursor for the inverse).
func EncodeCursor(codec *appkey.Codec, plan OrderByPlan, sortValue string, id uuid.UUID) (string, error) {
	if codec == nil {
		return "", fmt.Errorf("filter: page token codec is required")
	}
	if plan.Field == "" {
		return EncodeNextPageToken(codec, id)
	}
	b, err := json.Marshal(compoundCursor{Sort: sortValue, ID: id})
	if err != nil {
		return "", fmt.Errorf("filter: encode compound cursor: %w", err)
	}
	return codec.Encrypt(b)
}

// DecodeCursor decodes a page token into a KeysetCursor for the given order
// plan. For id-only ordering it decodes the 16-byte id token; for a custom
// order_by it decodes the compound token and converts the sort value to the
// plan's column type (TypeTimestamp → time.Time via RFC3339Nano; otherwise a
// string). Empty token → nil cursor (first page). A malformed or tampered
// token → error (caller maps to InvalidArgument on page_token).
func DecodeCursor(codec *appkey.Codec, plan OrderByPlan, token string) (*KeysetCursor, error) {
	if token == "" {
		return nil, nil
	}
	if plan.Field == "" {
		id, err := DecodePageToken(codec, token)
		if err != nil {
			return nil, err
		}
		return &KeysetCursor{ID: id}, nil
	}
	if codec == nil {
		return nil, fmt.Errorf("filter: page token codec is required")
	}
	raw, err := codec.Decrypt(token)
	if err != nil {
		return nil, fmt.Errorf("invalid page_token: %w", err)
	}
	var c compoundCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("invalid page_token: %w", err)
	}
	var sortValue any = c.Sort
	switch plan.Type {
	case filtering.TypeTimestamp:
		ts, err := time.Parse(time.RFC3339Nano, c.Sort)
		if err != nil {
			return nil, fmt.Errorf("invalid page_token: bad timestamp cursor: %w", err)
		}
		sortValue = ts
	case filtering.TypeInt:
		// Integer sort columns (e.g. a BIGINT size_bytes) must resume as an int64,
		// not a text literal: pgx has a registered int8 codec that rejects a Go
		// string bound against the bigint column, so the row comparison would fail
		// to encode. Reparse the RFC-decimal cursor value the handler encoded.
		n, err := strconv.ParseInt(c.Sort, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid page_token: bad integer cursor: %w", err)
		}
		sortValue = n
	}
	return &KeysetCursor{SortValue: sortValue, ID: c.ID}, nil
}
