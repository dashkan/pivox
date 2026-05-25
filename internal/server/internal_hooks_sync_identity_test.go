package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil"
	"github.com/dashkan/pivox/internal/testutil/authnmock"
)

// TestSyncIdentity_OrphanEmailCollision_TombstonesAndInserts pins the
// recovery path: when out-of-band Firebase delete leaves a Pivox
// identity alive and the same email re-registers with a new uid, the
// handler verifies via Firebase Admin SDK that the old uid is gone,
// then tombstones the orphan + drops its memberships + inserts the
// new identity. End-to-end against a real test DB; the Firebase
// SDK is mocked because we don't want a real Firebase round-trip in
// unit tests.
func TestSyncIdentity_OrphanEmailCollision_TombstonesAndInserts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Pre-seed: an existing live identity with the email — this is
	// the orphan whose Firebase counterpart was deleted out-of-band.
	const orphanUID = "orphan-uid"
	const email = "alice@example.com"
	orphan, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
		FirebaseUid:   orphanUID,
		Email:         email,
		EmailVerified: true,
		DisplayName:   "Alice (orphan)",
	})
	require.NoError(t, err)

	authMock := authnmock.NewMockService(t)
	// Firebase reports the old uid as gone — confirmed orphan.
	authMock.EXPECT().UserExists(mock.Anything, orphanUID).Return(false, nil).Once()

	h := newHooksForTest(t, pool, queries, authMock)

	// Re-registration: same email, new uid.
	const newUID = "new-uid"
	resp := callSyncIdentity(t, h, syncIdentityRequest{
		FirebaseUID:   newUID,
		Email:         email,
		EmailVerified: false,
		DisplayName:   "Alice (new)",
	})
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())

	var body map[string]string
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	newIdentityID, err := uuid.Parse(body["identity_id"])
	require.NoError(t, err)

	// The new identity row is alive with the new uid and email.
	live, err := queries.GetIdentityByEmail(ctx, email)
	require.NoError(t, err)
	assert.Equal(t, newUID, live.FirebaseUid)
	assert.Equal(t, newIdentityID, live.ID)

	// The orphan was tombstoned: is_deleted, blanked PII, id
	// preserved (so audit refs still resolve).
	stale, err := queries.GetIdentityByID(ctx, orphan.ID)
	require.NoError(t, err)
	assert.True(t, stale.IsDeleted, "orphan must be is_deleted=true")
	assert.Equal(t, "", stale.Email, "orphan PII must be blanked")
}

// TestSyncIdentity_EmailCollisionStillActive_Returns409 pins the
// safety guard: when the colliding identity's firebase_uid IS still
// active in Firebase, the handler refuses to tombstone (which would
// clobber an active user) and returns 409 so the blocking function
// can surface "email already in use" without ambiguity.
func TestSyncIdentity_EmailCollisionStillActive_Returns409(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	ctx := context.Background()

	const activeUID = "still-active-uid"
	const email = "bob@example.com"
	original, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
		FirebaseUid:   activeUID,
		Email:         email,
		EmailVerified: true,
		DisplayName:   "Bob",
	})
	require.NoError(t, err)

	authMock := authnmock.NewMockService(t)
	// Firebase says the existing uid is STILL ACTIVE — do not
	// tombstone.
	authMock.EXPECT().UserExists(mock.Anything, activeUID).Return(true, nil).Once()

	h := newHooksForTest(t, pool, queries, authMock)

	resp := callSyncIdentity(t, h, syncIdentityRequest{
		FirebaseUID:   "attacker-uid",
		Email:         email,
		DisplayName:   "Eve",
		EmailVerified: false,
	})
	assert.Equal(t, http.StatusConflict, resp.Code, "body=%s", resp.Body.String())

	// Original row must be untouched.
	got, err := queries.GetIdentityByID(ctx, original.ID)
	require.NoError(t, err)
	assert.False(t, got.IsDeleted, "active user must not be tombstoned")
	assert.Equal(t, activeUID, got.FirebaseUid)
	assert.Equal(t, email, got.Email)
}

// TestSyncIdentity_OrphanLookupErrors_Returns500 pins the safety
// guard for transient Firebase outages: if the Admin SDK lookup
// fails, the handler propagates 500 rather than tombstoning on
// uncertainty.
func TestSyncIdentity_OrphanLookupErrors_Returns500(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)
	ctx := context.Background()

	const orphanUID = "maybe-orphan-uid"
	const email = "carol@example.com"
	original, err := queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
		FirebaseUid:   orphanUID,
		Email:         email,
		EmailVerified: true,
		DisplayName:   "Carol",
	})
	require.NoError(t, err)

	authMock := authnmock.NewMockService(t)
	authMock.EXPECT().
		UserExists(mock.Anything, orphanUID).
		Return(false, assert.AnError).
		Once()

	h := newHooksForTest(t, pool, queries, authMock)

	resp := callSyncIdentity(t, h, syncIdentityRequest{
		FirebaseUID: "new-uid",
		Email:       email,
		DisplayName: "Carol (new)",
	})
	assert.Equal(t, http.StatusInternalServerError, resp.Code)

	// Original must NOT be tombstoned on uncertainty.
	got, err := queries.GetIdentityByID(ctx, original.ID)
	require.NoError(t, err)
	assert.False(t, got.IsDeleted)
	assert.Equal(t, email, got.Email)
}

// (Membership-drop coverage lives in the identity package's
// TombstoneOrphaned tests — this file pins the syncIdentity handler's
// branching logic, not the cascade internals.)

// TestSyncIdentity_HappyPath_NoCollision pins the common case: a
// brand-new email goes straight through UpsertIdentity without the
// recovery path firing. Firebase Admin SDK should not be called.
func TestSyncIdentity_HappyPath_NoCollision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	pool, queries := testutil.SetupTestDB(t)

	// No prior identity row. Mock asserts no UserExists call (via
	// strict expectations — mockery panics on unexpected calls when
	// no .EXPECT() is set).
	authMock := authnmock.NewMockService(t)
	h := newHooksForTest(t, pool, queries, authMock)

	resp := callSyncIdentity(t, h, syncIdentityRequest{
		FirebaseUID:   "fresh-uid",
		Email:         "fresh@example.com",
		EmailVerified: false,
		DisplayName:   "Fresh",
	})
	require.Equal(t, http.StatusOK, resp.Code, "body=%s", resp.Body.String())
}

// newHooksForTest builds an InternalHooks instance suitable for
// direct handler invocation (no syncAuth gate — we're exercising the
// handler logic, not the OIDC auth chain). Constructs the struct
// literal directly so we don't have to spin up a real
// google.golang.org/api/idtoken.Validator.
func newHooksForTest(t *testing.T, pool db.TxBeginner, queries db.Querier, auth *authnmock.MockService) *InternalHooks {
	t.Helper()
	return &InternalHooks{
		pool:    pool,
		queries: queries,
		logger:  silentLogger(),
		auth:    auth,
	}
}

func callSyncIdentity(t *testing.T, h *InternalHooks, req syncIdentityRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	httpReq := httptest.NewRequest(http.MethodPost, "/internal/v1/auth:syncIdentity", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.syncIdentity(rec, httpReq)
	return rec
}
