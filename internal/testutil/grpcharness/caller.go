//go:build dev

package grpcharness

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// Caller is the harness-side handle for an authenticated identity.
// SetCaller swaps which Caller subsequent gRPC requests authenticate
// as. The harness's test authn.Service trusts UID verbatim — the
// bearer token IS the UID — so a Caller is fully described by its
// (UID, IdentityID).
type Caller struct {
	UID                string
	IdentityID uuid.UUID
	Email              string
	DisplayName        string
}

// SeedIdentityOpts customizes a SeedIdentity call. Zero-value fields
// are populated with deterministic-but-unique defaults so callers
// can pass a bare struct.
type SeedIdentityOpts struct {
	UID         string // default: "test-uid-<random>"
	Email       string // default: "<UID>@example.com"
	DisplayName string // default: "Test <UID>"
}

// SeedIdentity inserts a identities row and returns a
// Caller pointing at it. Use SetCaller(caller) on the harness to
// authenticate subsequent RPCs as this identity. Identity-shape
// fields (email, display name) are stored exactly as passed; the
// rest of the system reads them via GetIdentityByFirebaseUID.
func (h *Harness) SeedIdentity(t *testing.T, opts SeedIdentityOpts) *Caller {
	t.Helper()

	if opts.UID == "" {
		opts.UID = "test-uid-" + uuid.New().String()
	}
	if opts.Email == "" {
		opts.Email = opts.UID + "@example.com"
	}
	if opts.DisplayName == "" {
		opts.DisplayName = "Test " + opts.UID
	}

	identity, err := h.Queries.UpsertIdentity(context.Background(),
		db.UpsertIdentityParams{
			FirebaseUid:   opts.UID,
			Email:         opts.Email,
			EmailVerified: true,
			DisplayName:   opts.DisplayName,
		})
	require.NoError(t, err)

	return &Caller{
		UID:                opts.UID,
		IdentityID: identity.ID,
		Email:              opts.Email,
		DisplayName:        opts.DisplayName,
	}
}
