// Package identitysync provisions Pivox identities from Keycloak
// events. Keycloak's keycloak-kafka SPI emits user lifecycle events
// (REGISTER, UPDATE_PROFILE, UPDATE_EMAIL, DELETE_ACCOUNT, LOGIN, ...)
// to the `keycloak-events` Kafka topic; this package consumes that
// topic and keeps the `identities` table in sync.
//
// Why this exists: nothing else provisions a Pivox identity at
// sign-up. A freshly-registered Keycloak user had no `identities` row, so they
// owned no orgs and the first CreateOrganization failed the
// `created_by` FK with a 23503. This consumer closes that gap by
// upserting the identity (id = the Keycloak `sub`) on REGISTER, and
// keeps email/name in sync on UPDATE_EMAIL / UPDATE_PROFILE.
//
// The keycloak-kafka SPI is patched to enrich every user event with the
// user's `name` (the OIDC display-name claim, composed from first + last)
// and `email`, looked up from the Keycloak user — so these events reliably
// carry the current name + email regardless of registration path (broker
// first-login omits them otherwise).
package identitysync

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// Event mirrors the keycloak-kafka SPI's event envelope as it lands on
// the `keycloak-events` topic. Only the fields we act on are modeled;
// unknown fields are ignored by encoding/json.
type event struct {
	Type      string            `json:"type"`
	RealmName string            `json:"realmName"`
	UserID    string            `json:"userId"`
	Details   map[string]string `json:"details"`
}

// Handler applies a single Keycloak event to the identities table. It
// is idempotent: REGISTER / UPDATE_PROFILE / UPDATE_EMAIL upsert,
// DELETE_ACCOUNT soft-deletes, and every other event type is a no-op.
// At-least-once redelivery is safe.
type Handler struct {
	queries db.Querier
	realm   string
	logger  *slog.Logger
}

// HandlerConfig configures Handler. All fields are required.
type HandlerConfig struct {
	// Queries is the identities sink (UpsertIdentity / SoftDeleteIdentity).
	Queries db.Querier
	// Realm is the Keycloak realm whose events this handler acts on.
	// Events from other realms are ignored.
	Realm string
	// Logger receives warnings for malformed events and skipped records.
	Logger *slog.Logger
}

// NewHandler builds a Handler. Panics if any required field is unset —
// a boot-time programmer error, not a per-event failure.
func NewHandler(cfg HandlerConfig) *Handler {
	if cfg.Queries == nil {
		panic("identitysync: HandlerConfig.Queries is required")
	}
	if cfg.Realm == "" {
		panic("identitysync: HandlerConfig.Realm is required")
	}
	if cfg.Logger == nil {
		panic("identitysync: HandlerConfig.Logger is required")
	}
	return &Handler{
		queries: cfg.Queries,
		realm:   cfg.Realm,
		logger:  cfg.Logger,
	}
}

// Handle applies one raw Keycloak event.
//
// Return-value contract drives offset commits in the consumer:
//   - A real DB error is RETURNED so the consumer does not advance the
//     offset past this record. It is retried on a later poll; the
//     upsert/soft-delete are idempotent, so replay is safe.
//   - Every "nothing to do" or "permanently undeliverable" case
//     (unparseable JSON, foreign realm, non-UUID user id, unhandled
//     event type, already-deleted identity) returns nil so the consumer
//     commits and moves on — redelivering noise helps no one.
func (h *Handler) Handle(ctx context.Context, raw []byte) error {
	var ev event
	if err := json.Unmarshal(raw, &ev); err != nil {
		// Malformed payload. Redelivery won't make it parse; drop it.
		h.logger.WarnContext(ctx, "identitysync: skipping unparseable event", "error", err)
		return nil
	}

	if ev.RealmName != h.realm {
		return nil
	}

	switch ev.Type {
	case "REGISTER", "UPDATE_PROFILE", "UPDATE_EMAIL":
		return h.upsert(ctx, ev)
	case "DELETE_ACCOUNT":
		return h.softDelete(ctx, ev)
	default:
		// LOGIN / LOGOUT and any future type: nothing to sync.
		return nil
	}
}

// upsert provisions or syncs the identity from a user event. The SPI enriches
// REGISTER / UPDATE_* with the current email + first/last name, so there's no
// risk of blanking an existing row's email. display_name is the trimmed
// "first last"; UpsertIdentity only overwrites display_name when the incoming
// value is non-empty, so an absent name never clears a previously-set one.
func (h *Handler) upsert(ctx context.Context, ev event) error {
	id, err := uuid.Parse(ev.UserID)
	if err != nil {
		h.logger.WarnContext(ctx, "identitysync: user event with non-UUID userId; skipping",
			"type", ev.Type, "user_id", ev.UserID, "error", err)
		return nil
	}
	if _, err := h.queries.UpsertIdentity(ctx, db.UpsertIdentityParams{
		ID:    id,
		Email: ev.Details["email"],
		// `name` is the OIDC display-name claim, populated by the keycloak-kafka
		// SPI (composed from the KC user's first + last). Absent => "" => the
		// upsert leaves any existing display_name untouched.
		DisplayName: ev.Details["name"],
	}); err != nil {
		if isEmailAlreadyOwned(err) {
			// Permanently undeliverable: the email already belongs to a
			// different sub — a duplicate Keycloak user for the same person
			// (e.g. a legacy local user alongside a brokered login).
			// Retrying can't resolve it (the unique index will always
			// reject), so skip-and-log rather than wedge the partition. The
			// duplicate is reconciled at the Keycloak level, not here.
			h.logger.WarnContext(ctx, "identitysync: email already owned by another sub; skipping",
				"type", ev.Type, "user_id", ev.UserID, "email", ev.Details["email"])
			return nil
		}
		return err
	}
	return nil
}

// isEmailAlreadyOwned reports whether err is the identities email-unique
// violation — the incoming sub's email already belongs to another identity.
// That's a permanent condition (a duplicate KC user), not a transient DB
// error, so the handler skips the record rather than retrying it forever.
func isEmailAlreadyOwned(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23505" && // unique_violation
		pgErr.ConstraintName == "idx_identities_email_unique"
}

func (h *Handler) softDelete(ctx context.Context, ev event) error {
	id, err := uuid.Parse(ev.UserID)
	if err != nil {
		h.logger.WarnContext(ctx, "identitysync: DELETE_ACCOUNT with non-UUID userId; skipping",
			"user_id", ev.UserID, "error", err)
		return nil
	}
	if _, err := h.queries.SoftDeleteIdentity(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Already gone (or never provisioned). Idempotent no-op.
			return nil
		}
		return err
	}
	return nil
}
