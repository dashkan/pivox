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
// bearer token IS the UID, which equals `identities.id` (mirroring a
// Keycloak token's `sub`) — so UID and IdentityID are the same value.
type Caller struct {
	UID         string
	IdentityID  uuid.UUID
	Email       string
	DisplayName string
}

// SeedIdentityOpts customizes a SeedIdentity call. Zero-value fields
// are populated with deterministic-but-unique defaults so callers
// can pass a bare struct.
//
// UID is retained for call-site readability (tests label callers by a
// stable string), but it does NOT become the bearer token — the
// identity's generated UUID does, since `identities.id` IS the
// Keycloak `sub` in production. Email/DisplayName default off UID.
type SeedIdentityOpts struct {
	UID         string // label only; default: "test-uid-<random>"
	Email       string // default: "<UID>@example.com"
	DisplayName string // default: "Test <UID>"
}

// SeedIdentity inserts an identities row and returns a Caller
// pointing at it. Use SetCaller(caller) on the harness to
// authenticate subsequent RPCs as this identity.
//
// Production auth flow: Keycloak issues an access token whose `sub`
// IS the `identities.id` UUID (the realm user id == the Pivox
// identity id). AuthInterceptor parses `sub` and puts the UUID on the
// gRPC context — handlers read it via `server.MustUserID(ctx)`.
//
// The harness mirrors this: the row's id is the generated UUID, and
// the Caller's UID (the bearer token) is that same UUID string, so
// `testAuthService.VerifyToken` returns it and the interceptor
// resolves the caller without any DB lookup.
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

	id := uuid.New()
	identity, err := h.Queries.UpsertIdentity(context.Background(),
		db.UpsertIdentityParams{
			ID:            id,
			Email:         opts.Email,
			EmailVerified: true,
			DisplayName:   opts.DisplayName,
		})
	require.NoError(t, err)

	return &Caller{
		UID:         identity.ID.String(),
		IdentityID:  identity.ID,
		Email:       opts.Email,
		DisplayName: opts.DisplayName,
	}
}
