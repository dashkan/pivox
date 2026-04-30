package server

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// CallerIdentityResolver maps the request context to the caller's
// firebase_identity row id. Returns a non-nil pre-formed gRPC error
// if the caller can't be resolved — handlers surface that error
// directly without re-mapping. Two failure modes are distinguished:
//
//   - Unauthenticated: no UID on the context, or the UID has no
//     corresponding row in `identities` (orphan UID — likely
//     the sync-identity webhook hasn't fired yet).
//   - Internal: a DB fault during the lookup.
//
// Production wires this to `NewCallerIdentityResolver(queries)`.
// Tests inject a stub returning `(id, nil)` for happy path or a
// pre-formed status error for sad path.
type CallerIdentityResolver func(ctx context.Context) (uuid.UUID, error)

// NewCallerIdentityResolver returns a production resolver that pulls
// the Firebase UID off the auth-interceptor context (via
// AuthenticatedUID) and looks up the identities row.
func NewCallerIdentityResolver(queries db.Querier) CallerIdentityResolver {
	return func(ctx context.Context) (uuid.UUID, error) {
		uid, ok := AuthenticatedUID(ctx)
		if !ok {
			return uuid.Nil, status.Error(codes.Unauthenticated, "missing authenticated caller")
		}
		identity, err := queries.GetIdentityByFirebaseUID(ctx, uid)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return uuid.Nil, status.Error(codes.Unauthenticated, "caller has no identity record")
			}
			slog.ErrorContext(ctx, "caller identity lookup failed", "uid", uid, "error", err)
			return uuid.Nil, status.Error(codes.Internal, "lookup caller identity")
		}
		return identity.ID, nil
	}
}
