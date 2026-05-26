package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/testutil/authnmock"
)

// makeJWT builds an unsigned JWT with the given issuer + claims.
// The signature is irrelevant for these tests — the composite's
// routing reads `iss` via ParseUnverified, and the downstream
// verifier (stubbed in tests) is what would do real signature
// checking in production.
func makeJWT(t *testing.T, iss string, extraClaims map[string]any) string {
	t.Helper()
	claims := jwt.MapClaims{
		"iss": iss,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	for k, v := range extraClaims {
		claims[k] = v
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString([]byte("unused-test-key"))
	require.NoError(t, err)
	return signed
}

func TestNewCompositeAuthService_NilVerifyReturnsWrappedService(t *testing.T) {
	t.Parallel()
	// When ssrVerify is nil, the composite is pointless; callers
	// shouldn't pay for the wrapper. NewCompositeAuthService returns
	// the wrapped service as-is — a type assertion confirms.
	fb := authnmock.NewMockService(t)
	got := NewCompositeAuthService(fb, nil)

	// Same instance, not a wrapper.
	assert.Same(t, fb, got)
}

func TestCompositeAuthService_FirebaseTokenDelegatesToWrapped(t *testing.T) {
	t.Parallel()
	// A JWT with the Firebase iss prefix must reach the wrapped
	// service's VerifyToken. The ssrVerify closure would fail loudly
	// if invoked.
	fb := authnmock.NewMockService(t)
	want := &authn.Identity{
		UID:    "fb-user",
		Claims: map[string]any{"pivox_user_id": uuid.New().String()},
	}
	token := makeJWT(t, "https://securetoken.google.com/pivox-test", nil)
	fb.EXPECT().VerifyToken(mock.Anything, token).Return(want, nil)

	ssrVerify := func(ctx context.Context, _ string) (uuid.UUID, error) {
		t.Fatal("ssrVerify must not be called for Firebase-issued tokens")
		return uuid.Nil, nil
	}

	composite := NewCompositeAuthService(fb, ssrVerify)
	got, err := composite.VerifyToken(context.Background(), token)

	require.NoError(t, err)
	assert.Same(t, want, got)
}

func TestCompositeAuthService_SsrTokenInvokesVerifier(t *testing.T) {
	t.Parallel()
	// A JWT whose iss is a service-account email goes to ssrVerify.
	// fb.VerifyToken must NOT be called — the mock would fail at
	// teardown since no EXPECT is set.
	fb := authnmock.NewMockService(t)
	asserted := uuid.New()
	saEmail := "ssr@pivox.iam.gserviceaccount.com"
	token := makeJWT(t, saEmail, map[string]any{"actor_uid": asserted.String()})

	var seenToken string
	ssrVerify := func(_ context.Context, tok string) (uuid.UUID, error) {
		seenToken = tok
		return asserted, nil
	}

	composite := NewCompositeAuthService(fb, ssrVerify)
	got, err := composite.VerifyToken(context.Background(), token)

	require.NoError(t, err)
	assert.Equal(t, token, seenToken, "verifier must receive the raw token bytes")
	// Composite synthesizes an Identity carrying the asserted UUID
	// in both UID and pivox_user_id claim so AuthInterceptor's
	// downstream claim-extraction lands on the same UUID either
	// way the token came in.
	assert.Equal(t, asserted.String(), got.UID)
	assert.Equal(t, asserted.String(), got.Claims["pivox_user_id"])
}

func TestCompositeAuthService_SsrVerifyErrorPropagates(t *testing.T) {
	t.Parallel()
	// ssrVerify returns an error (bad signature, audience mismatch,
	// issuer not allowlisted, missing actor_uid, malformed UUID —
	// anything). The composite passes it through verbatim;
	// authenticateBearer collapses to a canonical Unauthenticated
	// upstream.
	fb := authnmock.NewMockService(t)
	token := makeJWT(t, "ssr@pivox.iam.gserviceaccount.com", nil)
	wantErr := errors.New("bad signature")

	ssrVerify := func(context.Context, string) (uuid.UUID, error) {
		return uuid.Nil, wantErr
	}

	composite := NewCompositeAuthService(fb, ssrVerify)
	got, err := composite.VerifyToken(context.Background(), token)

	require.Error(t, err)
	assert.Equal(t, wantErr, err)
	assert.Nil(t, got)
}

func TestCompositeAuthService_MalformedTokenFallsThroughToFirebase(t *testing.T) {
	t.Parallel()
	// A bearer that isn't parseable JWT shape can't be routed to
	// SSR. The composite hands it to the wrapped service, which
	// (in production) rejects via Firebase Admin SDK signature
	// validation.
	fb := authnmock.NewMockService(t)
	fb.EXPECT().VerifyToken(mock.Anything, "not.a.jwt").
		Return(nil, fmt.Errorf("malformed token"))

	ssrCalled := false
	ssrVerify := func(context.Context, string) (uuid.UUID, error) {
		ssrCalled = true
		return uuid.Nil, nil
	}

	composite := NewCompositeAuthService(fb, ssrVerify)
	_, err := composite.VerifyToken(context.Background(), "not.a.jwt")

	require.Error(t, err)
	assert.False(t, ssrCalled, "ssrVerify must not see a malformed token")
}

func TestCompositeAuthService_TokenWithEmptyIssFallsThroughToFirebase(t *testing.T) {
	t.Parallel()
	// A JWT-shaped token without an iss claim has no signal for
	// routing. Default to Firebase — same fail-safe principle.
	fb := authnmock.NewMockService(t)
	// Empty iss; library still produces a valid JWT shape.
	token := makeJWT(t, "", nil)
	fb.EXPECT().VerifyToken(mock.Anything, token).
		Return(nil, fmt.Errorf("missing iss"))

	ssrCalled := false
	ssrVerify := func(context.Context, string) (uuid.UUID, error) {
		ssrCalled = true
		return uuid.Nil, nil
	}

	composite := NewCompositeAuthService(fb, ssrVerify)
	_, err := composite.VerifyToken(context.Background(), token)

	require.Error(t, err)
	assert.False(t, ssrCalled, "ssrVerify must not be invoked for empty iss")
}
