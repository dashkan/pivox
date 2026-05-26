package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/authn"
	"github.com/dashkan/pivox/internal/config"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/identity"
)

// emailUniqueIndexName is the partial-unique index on identities.email
// (see 000001_init.up.sql). Detected by name on a 23505 pgError so the
// syncIdentity defensive path can distinguish "email-orphan collision"
// from any other unique-constraint violation.
const emailUniqueIndexName = "idx_identities_email_unique"

// InternalHooksConfig is the constructor input for NewInternalHooks.
type InternalHooksConfig struct {
	// Pool is the database pool. Required by syncIdentity's
	// orphan-collision recovery path, which wraps a tombstone +
	// member-cascade in a transaction. Single-statement endpoints
	// use `Queries` directly.
	Pool db.TxBeginner
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// SyncAuth carries OIDC validation settings for the
	// auth:syncIdentity endpoint.
	SyncAuth config.ServiceAccountAuthConfig
	// DelegatedAuth governs the delegated-auth endpoints.
	DelegatedAuth config.DelegatedAuthConfig
	// Logger is the slog.Logger used for warning/error events.
	// Required.
	Logger *slog.Logger
	// Auth is the authn service. Required.
	//
	// MUST be the bare Firebase service (firebase.NewAuthService),
	// NOT the SSR composite (server.NewCompositeAuthService). The
	// delegated-auth flow (completeDelegatedAuthSession) reads
	// `identity.UID` and passes it to `CreateCustomToken` — that's
	// valid only for real Firebase UIDs. The SSR composite
	// synthesizes Identity with the Pivox UUID in `UID`, which would
	// cause CreateCustomToken to mint tokens for non-existent
	// Firebase users.
	//
	// There is no architectural path for SSR-acting-as tokens to
	// reach these endpoints (they're for native-app interactive
	// sign-in, not server-to-server), but the type system can't
	// enforce the wiring — code review must.
	Auth authn.Service
	// AuditResolver receives Invalidate() calls when the
	// syncIdentity webhook upserts an existing identity row.
	// Optional; nil disables cache-invalidation (audit cache will
	// catch up via TTL expiry).
	AuditResolver *audit.Resolver
}

// InternalHooks handles internal webhook endpoints that are not part of the
// public gRPC/REST API. These are called by Firebase Functions and other
// internal services.
type InternalHooks struct {
	pool          db.TxBeginner
	queries       db.Querier
	logger        *slog.Logger
	auth          authn.Service
	delegatedAuth config.DelegatedAuthConfig
	// audit receives Invalidate() calls when syncIdentity upserts
	// an existing identity row (display_name / photo_url / email
	// changes from Firebase). Optional — if nil, the audit cache
	// catches up via TTL.
	audit *audit.Resolver

	// syncAuth protects the auth:syncIdentity endpoint via Google Cloud
	// OIDC identity token verification. Set during NewInternalHooks
	// from cfg.SyncAuth.AllowedServiceAccounts and cfg.SyncAuth.Audience.
	syncAuth func(http.HandlerFunc) http.HandlerFunc
}

// Register mounts the internal endpoints on the given mux.
//
// No app-level rate limiting is applied: pivox-cloud runs behind an edge
// proxy / load balancer in production, which owns volumetric IP-class
// abuse defense. App-level abuse defense lives in single-use codes,
// short TTLs, the auth chain, and response-shape uniformity.
func (h *InternalHooks) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /internal/v1/auth:syncIdentity", h.syncAuth(h.syncIdentity))
	mux.HandleFunc("POST /internal/v1/auth:createDelegatedAuthSession", h.createDelegatedAuthSession)
	mux.HandleFunc("POST /internal/v1/auth:completeDelegatedAuthSession", h.completeDelegatedAuthSession)
	mux.HandleFunc("POST /internal/v1/auth:pollDelegatedAuthSession", h.pollDelegatedAuthSession)
	mux.HandleFunc("POST /internal/v1/auth:resolveProvider", h.resolveProvider)
}

// syncIdentityRequest is the payload sent by the Firebase
// onUserCreated / onUserSignedIn blocking functions.
type syncIdentityRequest struct {
	FirebaseUID   string `json:"firebase_uid"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	DisplayName   string `json:"display_name"`
	PhotoURL      string `json:"photo_url"`
	Disabled      bool   `json:"disabled"`
}

// syncIdentity upserts a Firebase Auth user into the
// identities table. Called by the Firebase Function blocking
// triggers on user create / sign-in so the cloud has a Pivox-side
// identity row before any org-scoped RPC runs.
func (h *InternalHooks) syncIdentity(w http.ResponseWriter, r *http.Request) {
	// AUTHN-05: Limit request body to 8 KB (sync payloads are small JSON).
	r.Body = http.MaxBytesReader(w, r.Body, 8<<10)

	var req syncIdentityRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("invalid sync identity request", "error", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.FirebaseUID == "" {
		http.Error(w, "firebase_uid is required", http.StatusBadRequest)
		return
	}

	params := db.UpsertIdentityParams{
		FirebaseUid:   req.FirebaseUID,
		Email:         req.Email,
		EmailVerified: req.EmailVerified,
		DisplayName:   req.DisplayName,
		PhotoUrl:      req.PhotoURL,
		Disabled:      req.Disabled,
		LastLoginTime: pgtype.Timestamptz{}, // not set on creation
	}
	identityRow, err := h.queries.UpsertIdentity(r.Context(), params)
	if err != nil {
		// Detect the orphan-email collision (out-of-band Firebase
		// delete left the Pivox identity alive, user re-registers
		// the same email → Firebase mints a new uid → ON CONFLICT
		// (firebase_uid) doesn't fire → INSERT path → 23505 on the
		// partial unique email index).
		if isOrphanEmailCollision(err) {
			identityRow, err = h.recoverOrphanEmailCollision(r.Context(), params)
		}
		if err != nil {
			// Already-active email collision is reported as 409 so
			// the blocking function can surface a clean
			// "email already in use" path to the client; everything
			// else is an opaque 500.
			if errors.Is(err, errEmailStillActive) {
				h.logger.Warn("syncIdentity: email already in use",
					"firebase_uid", req.FirebaseUID, "email", req.Email)
				http.Error(w, "email already in use", http.StatusConflict)
				return
			}
			h.logger.Error("failed to upsert identity",
				"firebase_uid", req.FirebaseUID, "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	// Drop any cached Actor — the upsert may have changed
	// display_name / photo_url / email (Firebase profile-edit path)
	// or email_verified. New identity rows aren't cached yet so the
	// call is a no-op for the create path; the cost is one map
	// delete per webhook call.
	if h.audit != nil {
		h.audit.Invalidate(identityRow.ID)
	}

	h.logger.Info("identity synced", "firebase_uid", req.FirebaseUID, "identity_id", identityRow.ID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"identity_id": identityRow.ID.String(),
	}); err != nil {
		h.logger.Warn("write sync-identity response failed", "error", err)
	}
}

// errEmailStillActive signals the email-collision case where the
// pre-existing identity's firebase_uid is still active in the auth
// provider — i.e., NOT an out-of-band-deleted orphan. The handler
// turns this into a 409 so the blocking function can surface a
// "email already in use" path to the client. Distinct from "internal
// error" because the retry shape is different (user-error vs ours).
var errEmailStillActive = errors.New("syncIdentity: email already bound to an active firebase user")

// isOrphanEmailCollision reports whether the error is a 23505 unique-
// violation on the partial unique email index — the orphan-email
// collision signature. Any other unique violation (firebase_uid
// recycled, etc.) returns false so the caller's outer error path
// runs unchanged.
func isOrphanEmailCollision(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == emailUniqueIndexName
}

// recoverOrphanEmailCollision handles the orphan-email collision
// detected by isOrphanEmailCollision. Sequence:
//
//  1. Lookup the live identity row by email.
//  2. Ask Firebase Admin SDK whether the existing firebase_uid is
//     still active.
//     3a. If active → return errEmailStillActive (the handler maps to 409).
//     3b. If gone → tombstone the orphan + drop memberships in a tx,
//     then re-call UpsertIdentity with the new uid (the partial
//     unique index excludes the tombstoned row, so the second
//     attempt INSERTs cleanly).
//     3c. If the Admin SDK lookup itself errors → propagate as internal
//     error. Never tombstone on uncertainty.
//
// SECURITY: the Firebase-side existence check is load-bearing. Without
// it, an attacker who knew a victim's email could re-register at
// Firebase (which is open by default) → trigger syncIdentity → and if
// we blindly tombstoned the colliding row, the victim would lose
// their orgs + PII. The Admin SDK call confirms the original UID is
// actually gone before we touch anything.
func (h *InternalHooks) recoverOrphanEmailCollision(
	ctx context.Context, params db.UpsertIdentityParams,
) (db.Identity, error) {
	existing, err := h.queries.GetIdentityByEmail(ctx, params.Email)
	if err != nil {
		// Shouldn't happen if we hit 23505 on the email index, but
		// guard against the race where the row was deleted between
		// our INSERT attempt and the lookup.
		return db.Identity{}, err
	}
	if existing.FirebaseUid == params.FirebaseUid {
		// Same uid — the upsert should have hit the firebase_uid
		// ON CONFLICT path. If we're here, something is weird;
		// return the existing row and let the caller decide.
		return existing, nil
	}
	stillActive, err := h.auth.UserExists(ctx, existing.FirebaseUid)
	if err != nil {
		h.logger.Error("syncIdentity: UserExists lookup failed during orphan recovery",
			"existing_firebase_uid", existing.FirebaseUid, "error", err)
		return db.Identity{}, err
	}
	if stillActive {
		return db.Identity{}, errEmailStillActive
	}
	// Confirmed orphan. Tombstone + drop memberships, then retry
	// the upsert. The partial unique email index excludes tombstoned
	// rows so the second INSERT succeeds.
	h.logger.Warn("syncIdentity: tombstoning orphaned identity for email re-registration",
		"email", params.Email,
		"orphan_identity_id", existing.ID,
		"orphan_firebase_uid", existing.FirebaseUid,
		"new_firebase_uid", params.FirebaseUid)
	if err := identity.TombstoneOrphaned(ctx, h.pool, existing.ID, h.logger); err != nil {
		return db.Identity{}, err
	}
	if h.audit != nil {
		h.audit.Invalidate(existing.ID)
	}
	return h.queries.UpsertIdentity(ctx, params)
}

// ---------------------------------------------------------------------------
// Delegated auth sessions (AUTHN-07)
//
// Plugins running inside third-party host processes (NRCS ActiveX, Adobe UXP)
// cannot safely authenticate in-process. They instead delegate auth to the
// Pivox app: create a session here, deep-link into the app, poll until the
// app completes the session, then sign in with the resulting custom token.
// ---------------------------------------------------------------------------

// createDelegatedAuthSessionResponse is the body returned to clients that
// create a new delegated auth session.
type createDelegatedAuthSessionResponse struct {
	Code         string `json:"code"`
	PollInterval int    `json:"pollInterval"`
}

// createDelegatedAuthSession mints a new session code and stores it in the
// pending state. The client then launches the Pivox app via deep link and
// polls until the session becomes ready.
func (h *InternalHooks) createDelegatedAuthSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	// Drain body to keep connection reusable; request shape is empty.
	_, _ = io.Copy(io.Discard, r.Body)

	// Codes come from crypto/rand via uuid.New() (v4) rather than the table
	// default — we never want a predictable time-ordered UUID for auth secrets.
	code := uuid.New()
	expiresAt := time.Now().Add(h.delegatedAuth.SessionTTL)

	if _, err := h.queries.CreateDelegatedAuthSession(r.Context(), db.CreateDelegatedAuthSessionParams{
		Code:       code,
		ExpireTime: expiresAt,
	}); err != nil {
		h.logger.Error("failed to create delegated auth session", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	h.logger.Info("delegated auth session created", "code_prefix", code.String()[:8])

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(createDelegatedAuthSessionResponse{
		Code:         code.String(),
		PollInterval: int(h.delegatedAuth.PollInterval / time.Second),
	})
}

// completeDelegatedAuthSessionRequest is the payload for the complete endpoint.
type completeDelegatedAuthSessionRequest struct {
	Code string `json:"code"`
}

// completeDelegatedAuthSession is called by the Pivox app after the user
// signs in. It verifies the user's Firebase ID token, mints a custom token
// bound to the same UID, and transitions the session to ready so the
// polling plugin can pick it up.
func (h *InternalHooks) completeDelegatedAuthSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)

	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "missing or invalid authorization header", http.StatusUnauthorized)
		return
	}
	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	identity, err := h.auth.VerifyToken(r.Context(), idToken)
	if err != nil {
		h.logger.Warn("complete delegated session: invalid ID token", "error", err)
		http.Error(w, "invalid ID token", http.StatusUnauthorized)
		return
	}

	var req completeDelegatedAuthSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	code, err := uuid.Parse(req.Code)
	if err != nil {
		http.Error(w, "invalid code format", http.StatusBadRequest)
		return
	}
	// Defense-in-depth: verify the parsed canonical form matches the input
	// using a constant-time comparison. Parameterized DB lookups already
	// protect the secret path; this keeps the handler itself free of any
	// variable-time byte comparisons on user-supplied code material.
	if subtle.ConstantTimeCompare([]byte(code.String()), []byte(strings.ToLower(req.Code))) != 1 {
		http.Error(w, "invalid code format", http.StatusBadRequest)
		return
	}

	customToken, err := h.auth.CreateCustomToken(r.Context(), identity.UID)
	if err != nil {
		h.logger.Error("failed to create custom token", "uid", identity.UID, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if _, err := h.queries.CompleteDelegatedAuthSession(r.Context(), db.CompleteDelegatedAuthSessionParams{
		Code:        code,
		CustomToken: pgtype.Text{String: customToken, Valid: true},
	}); err != nil {
		// No rows = code unknown, already completed, or expired. Return 404
		// without leaking which of the three it was.
		h.logger.Warn("complete delegated session: no matching pending session", "error", err)
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	h.logger.Info("delegated auth session completed", "uid", identity.UID)
	w.WriteHeader(http.StatusNoContent)
}

// pollDelegatedAuthSessionRequest is the payload for the poll endpoint.
type pollDelegatedAuthSessionRequest struct {
	Code string `json:"code"`
}

// pollDelegatedAuthSession is called by the plugin after it launches the app.
// If the session is ready it returns the custom token and atomically deletes
// the record (single-use). If still pending it returns {"status":"pending"}.
// If unknown/expired it returns 404.
func (h *InternalHooks) pollDelegatedAuthSession(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)

	var req pollDelegatedAuthSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	code, err := uuid.Parse(req.Code)
	if err != nil {
		http.Error(w, "invalid code format", http.StatusBadRequest)
		return
	}

	// Try to consume first — the common ready-path is a single SQL statement.
	customToken, err := h.queries.ConsumeDelegatedAuthSession(r.Context(), code)
	if err == nil && customToken.Valid {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"customToken": customToken.String,
		})
		return
	}

	// Consume missed — fall back to a state lookup so we can distinguish
	// "still pending" from "unknown/expired".
	state, err := h.queries.GetDelegatedAuthSessionState(r.Context(), code)
	if err != nil {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}
	if state == db.DelegatedAuthSessionStatePENDING {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "pending",
		})
		return
	}
	// Any other state is treated as gone (should not normally happen — an
	// APPROVED row would have been consumed above).
	http.Error(w, "session not found", http.StatusNotFound)
}

// resolveProviderRequest is the payload for
// POST /internal/v1/auth:resolveProvider. The Firebase pre-sign-in
// hook calls this with the user's email to look up the right
// SAML/OIDC provider id.
type resolveProviderRequest struct {
	Email string `json:"email"`
}

type resolveProviderResponse struct {
	// ProviderID is the firebase_provider_id of the SsoConfig that
	// matches the email's domain. Empty when no provider applies
	// (response is 404 in that case).
	ProviderID string `json:"provider_id"`
}

// resolveProvider maps an email's domain → verified Domain row →
// enabled SsoConfig → firebase_provider_id. The Firebase
// pre-sign-in blocking function calls this synchronously before
// completing a federated sign-in; a NOT_FOUND tells Firebase to
// fall back to password (or whatever else the project allows).
//
// Returns:
//
//	200 + JSON `{provider_id: "oidc.<slug>"}` when a match exists.
//	404 when the email's domain isn't claimed, isn't verified, or
//	    its SsoConfig is disabled. The error body is intentionally
//	    generic — we don't disclose whether the domain is unknown
//	    vs. unconfigured to avoid enumeration attacks.
//	400 on malformed input (missing/invalid email).
//
// Authentication: this endpoint is called by Firebase blocking
// functions over an internal channel. Same auth posture as the
// other internal hooks (rate-limited; in production guarded by the
// reverse proxy / VPC).
func (h *InternalHooks) resolveProvider(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var req resolveProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.logger.Warn("resolveProvider: invalid request body", "error", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	domain := emailDomain(req.Email)
	if domain == "" {
		http.Error(w, "email is required and must contain a domain", http.StatusBadRequest)
		return
	}
	row, err := h.queries.ResolveProviderByDomain(r.Context(), domain)
	if err != nil {
		// pgx.ErrNoRows is the common case (no provider applies):
		// domain not claimed, not VERIFIED, or SsoConfig disabled.
		// Generic 404 to avoid enumeration leaks.
		if errors.Is(err, pgx.ErrNoRows) {
			http.Error(w, "no provider configured for this domain", http.StatusNotFound)
			return
		}
		h.logger.Error("resolveProvider: lookup failed", "domain", domain, "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resolveProviderResponse{ProviderID: row.FirebaseProviderID}); err != nil {
		h.logger.Warn("resolveProvider: write response failed", "error", err)
	}
}

// emailDomain returns the lowercase domain part of an email or
// empty if the address is malformed. We accept exactly one '@'
// and require both sides to be non-empty.
func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at <= 0 || at == len(email)-1 {
		return ""
	}
	return strings.ToLower(email[at+1:])
}
