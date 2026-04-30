package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// OAuth broker state token: HMAC-SHA256-signed payload that round-
// trips through the IdP's authorize/redirect dance and comes back
// to us at /api/oauth/{provider}/callback. Format:
//
//	<base64url(payload)>.<base64url(HMAC-SHA256(key, payload))>
//
// Stateless by design — rotating PIVOX_APP_KEY invalidates all
// in-flight flows but we don't track per-flow state server-side.
// 10-minute TTL is enough for slow sign-ins; replay outside the
// window fails the freshness check.
//
// Mirrors the previous TanStack `state.ts` exactly (same JSON keys,
// same HMAC label) so we can run side-by-side during cutover.

type oauthStatePayload struct {
	N string `json:"n"` // nonce: per-request random, binds authorize → callback
	R string `json:"r"` // return URL: pivox://… for native, same-origin for web
	P string `json:"p"` // provider id (e.g. "github" or "oidc.acme")
	T int64  `json:"t"` // issued-at, seconds-since-epoch
}

const (
	oauthStateAlgo       = "sha256"
	oauthStateLabel      = "oauth-state"
	oauthStateMaxAgeSecs = 10 * 60
)

// oauthSigningKey derives the HMAC key from the app secret using a
// label so the same PIVOX_APP_KEY can be used for multiple distinct
// signing purposes (HKDF-style key separation).
func oauthSigningKey(appKey string) ([]byte, error) {
	if len(appKey) < 32 {
		return nil, fmt.Errorf("PIVOX_APP_KEY missing or < 32 chars")
	}
	mac := hmac.New(sha256.New, []byte(appKey))
	mac.Write([]byte(oauthStateLabel))
	return mac.Sum(nil), nil
}

// signOAuthState produces a signed state token for the broker's
// /start handler to send to the IdP. Returns both the encoded token
// and the payload it embedded so the caller can read fields (e.g.
// the nonce for the OIDC authorize request) without re-parsing.
//
// The nonce is generated here (16 random bytes, base64url-encoded)
// and included in the token so the callback can later verify replay.
func signOAuthState(appKey, returnURL, providerID string) (string, *oauthStatePayload, error) {
	return signOAuthStateAt(appKey, returnURL, providerID, time.Now())
}

// signOAuthStateAt is signOAuthState with a custom issued-at, used
// only by tests to forge expired tokens. Production callers go
// through signOAuthState which always uses the current time.
func signOAuthStateAt(appKey, returnURL, providerID string, issuedAt time.Time) (string, *oauthStatePayload, error) {
	key, err := oauthSigningKey(appKey)
	if err != nil {
		return "", nil, err
	}
	var nonceBytes [16]byte
	if _, err := rand.Read(nonceBytes[:]); err != nil {
		return "", nil, fmt.Errorf("read random nonce: %w", err)
	}
	payload := oauthStatePayload{
		N: base64.RawURLEncoding.EncodeToString(nonceBytes[:]),
		R: returnURL,
		P: providerID,
		T: issuedAt.Unix(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", nil, fmt.Errorf("marshal state: %w", err)
	}
	encoded := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encoded + "." + sig, &payload, nil
}

// verifyOAuthState rejects tokens that fail HMAC verification, are
// older than the max age, or are malformed. Returns the decoded
// payload on success.
func verifyOAuthState(appKey, token string) (*oauthStatePayload, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil, fmt.Errorf("malformed state")
	}
	body, sig := parts[0], parts[1]

	key, err := oauthSigningKey(appKey)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(body))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	// Constant-time compare to avoid timing oracles. Length check
	// guards `hmac.Equal` (which itself is constant-time within
	// equal-length pairs but panics on mismatched lengths).
	if len(want) != len(sig) || !hmac.Equal([]byte(want), []byte(sig)) {
		return nil, fmt.Errorf("invalid state signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return nil, fmt.Errorf("decode state body: %w", err)
	}
	var payload oauthStatePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	age := time.Now().Unix() - payload.T
	if age < 0 || age > oauthStateMaxAgeSecs {
		return nil, fmt.Errorf("state expired or future-dated")
	}
	return &payload, nil
}
