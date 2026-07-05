package identitysync_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/identitysync"
	"github.com/dashkan/pivox/internal/testutil"
)

func newTestLogger(*testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestHandler exercises the handler against a real Postgres so the
// assertions are on observable DB state, not mocked call shape.
func TestHandler(t *testing.T) {
	t.Parallel()

	pool, queries := testutil.SetupTestDB(t)
	defer pool.Close()

	h := identitysync.NewHandler(identitysync.HandlerConfig{
		Queries: queries,
		Realm:   "pivox",
		Logger:  newTestLogger(t),
	})
	ctx := context.Background()

	t.Run("REGISTER provisions the identity with the SPI-enriched name", func(t *testing.T) {
		sub := uuid.New()
		raw := []byte(`{"type":"REGISTER","realmName":"pivox","userId":"` + sub.String() +
			`","details":{"email":"emma@acme.com","identity_provider":"oidc.acme","name":"Emma Acme"}}`)

		require.NoError(t, h.Handle(ctx, raw))

		got, err := queries.GetIdentityByID(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, sub, got.ID)
		assert.Equal(t, "emma@acme.com", got.Email)
		assert.Equal(t, "Emma Acme", got.DisplayName)
		assert.False(t, got.IsDeleted)
	})

	t.Run("REGISTER without a name leaves display_name empty", func(t *testing.T) {
		sub := uuid.New()
		raw := []byte(`{"type":"REGISTER","realmName":"pivox","userId":"` + sub.String() +
			`","details":{"email":"noname@acme.com"}}`)

		require.NoError(t, h.Handle(ctx, raw))

		got, err := queries.GetIdentityByID(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, "noname@acme.com", got.Email)
		assert.Empty(t, got.DisplayName)
	})

	t.Run("UPDATE_PROFILE syncs the display name onto an existing identity", func(t *testing.T) {
		sub := uuid.New()
		_, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{ID: sub, Email: "ardy@acme.com"})
		require.NoError(t, err)

		raw := []byte(`{"type":"UPDATE_PROFILE","realmName":"pivox","userId":"` + sub.String() +
			`","details":{"email":"ardy@acme.com","name":"Ardy Daie"}}`)
		require.NoError(t, h.Handle(ctx, raw))

		got, err := queries.GetIdentityByID(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, "Ardy Daie", got.DisplayName)
		assert.Equal(t, "ardy@acme.com", got.Email)
	})

	t.Run("UPDATE_PROFILE without email preserves email + email_verified", func(t *testing.T) {
		sub := uuid.New()
		_, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
			ID: sub, Email: "keep@acme.com", EmailVerified: true, DisplayName: "Old",
		})
		require.NoError(t, err)

		// Event omits email (the blanking-risk case the upsert guard defends).
		raw := []byte(`{"type":"UPDATE_PROFILE","realmName":"pivox","userId":"` + sub.String() +
			`","details":{"name":"New Name"}}`)
		require.NoError(t, h.Handle(ctx, raw))

		got, err := queries.GetIdentityByID(ctx, sub)
		require.NoError(t, err)
		assert.Equal(t, "keep@acme.com", got.Email, "absent email must not blank the row")
		assert.True(t, got.EmailVerified, "email_verified must not be reset to false on update")
		assert.Equal(t, "New Name", got.DisplayName)
	})

	t.Run("REGISTER in another realm is ignored", func(t *testing.T) {
		sub := uuid.New()
		raw := []byte(`{"type":"REGISTER","realmName":"acme","userId":"` + sub.String() +
			`","details":{"email":"nobody@other.com"}}`)

		require.NoError(t, h.Handle(ctx, raw))

		_, err := queries.GetIdentityByID(ctx, sub)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("DELETE_ACCOUNT soft-deletes the identity", func(t *testing.T) {
		sub := uuid.New()
		_, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{ID: sub, Email: "del@acme.com"})
		require.NoError(t, err)

		raw := []byte(`{"type":"DELETE_ACCOUNT","realmName":"pivox","userId":"` + sub.String() + `"}`)
		require.NoError(t, h.Handle(ctx, raw))

		got, err := queries.GetIdentityByID(ctx, sub)
		require.NoError(t, err)
		assert.True(t, got.IsDeleted)
	})

	t.Run("DELETE_ACCOUNT for unknown identity is a no-op", func(t *testing.T) {
		sub := uuid.New()
		raw := []byte(`{"type":"DELETE_ACCOUNT","realmName":"pivox","userId":"` + sub.String() + `"}`)
		assert.NoError(t, h.Handle(ctx, raw))
	})

	t.Run("LOGIN is a no-op", func(t *testing.T) {
		sub := uuid.New()
		raw := []byte(`{"type":"LOGIN","realmName":"pivox","userId":"` + sub.String() +
			`","details":{"email":"login@acme.com"}}`)

		require.NoError(t, h.Handle(ctx, raw))

		_, err := queries.GetIdentityByID(ctx, sub)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("REGISTER with non-UUID userId errors-free no-op", func(t *testing.T) {
		raw := []byte(`{"type":"REGISTER","realmName":"pivox","userId":"not-a-uuid","details":{"email":"x@acme.com"}}`)
		require.NoError(t, h.Handle(ctx, raw))

		_, err := queries.GetIdentityByEmail(ctx, "x@acme.com")
		assert.ErrorIs(t, err, pgx.ErrNoRows)
	})

	t.Run("unparseable bytes are dropped without error", func(t *testing.T) {
		assert.NoError(t, h.Handle(ctx, []byte("{not json")))
	})

	t.Run("REGISTER for an email already owned by another sub skips without wedging", func(t *testing.T) {
		// The email is already provisioned under one sub...
		existing := uuid.New()
		_, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{ID: existing, Email: "dup@acme.com"})
		require.NoError(t, err)

		// ...and a *different* sub (a duplicate KC user for the same person —
		// e.g. a Firebase-era local user alongside a brokered login) registers
		// the same email. This is permanently undeliverable: the email unique
		// index will always reject it. Handle MUST skip it (return nil to
		// advance the offset), not wedge the partition by returning the error.
		other := uuid.New()
		raw := []byte(`{"type":"REGISTER","realmName":"pivox","userId":"` + other.String() +
			`","details":{"email":"dup@acme.com","name":"Dup User"}}`)
		require.NoError(t, h.Handle(ctx, raw))

		// The colliding sub is not provisioned; the original keeps the email.
		_, err = queries.GetIdentityByID(ctx, other)
		assert.ErrorIs(t, err, pgx.ErrNoRows)
		got, err := queries.GetIdentityByEmail(ctx, "dup@acme.com")
		require.NoError(t, err)
		assert.Equal(t, existing, got.ID, "original identity retains the email")
	})
}
