// Package secrets implements the encrypted credential vault (the Secrets
// gRPC service). A Secret holds one opaque, write-only value encrypted at
// rest via the platform Encryptor and bound (via AAD) to the row's immutable
// id. Values are never returned; callers rotate by overwriting.
package secrets

import (
	"context"
	"encoding/json"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	secretsv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/secrets/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/server"
	"google.golang.org/protobuf/types/known/emptypb"
)

// SecretsServer serves the Secrets vault RPCs.
type SecretsServer struct {
	secretsv1.UnimplementedSecretsServer
	pool      db.RWPool
	queries   db.Querier
	codec     *appkey.Codec
	audit     *audit.Resolver
	encryptor crypto.Encryptor
}

// Config is the constructor input for SecretsServer.
type Config struct {
	// Pool is the database pool (DBTX for reads, TxBeginner for tx writes).
	// Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes page tokens. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos. Optional.
	AuditResolver *audit.Resolver
	// Encryptor encrypts secret values at rest. Required.
	Encryptor crypto.Encryptor
}

// NewSecretsServer constructs the server from cfg. Panics on a missing
// required field.
func NewSecretsServer(cfg Config) *SecretsServer {
	if cfg.Pool == nil {
		panic("secrets: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("secrets: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("secrets: Config.Codec is required")
	}
	if cfg.Encryptor == nil {
		panic("secrets: Config.Encryptor is required")
	}
	return &SecretsServer{
		pool:      cfg.Pool,
		queries:   cfg.Queries,
		codec:     cfg.Codec,
		audit:     cfg.AuditResolver,
		encryptor: cfg.Encryptor,
	}
}

// secretAAD binds a secret's ciphertext to its immutable id, so a stored
// value cannot be replayed against a different Secret row (or a re-created
// slug). Encrypt and decrypt MUST derive the AAD identically.
func secretAAD(id uuid.UUID) []byte {
	return append([]byte("secret:"), id[:]...)
}

// scope reads the interceptor-resolved org (always set) and space (set when
// the resource is space-scoped) from ctx. Returns the org id, the nullable
// space id, and the parent resource-name prefix.
func (s *SecretsServer) scope(ctx context.Context) (orgID uuid.UUID, spaceID pgtype.UUID, namePrefix string) {
	org := server.MustResolvedOrgFromContext(ctx)
	orgID = org.ID
	namePrefix = "organizations/" + org.Slug
	if space, ok := server.ResolvedSpaceFromContext(ctx); ok {
		spaceID = convert.PgUUID(space.ID)
		namePrefix += "/spaces/" + space.Slug
	}
	return orgID, spaceID, namePrefix
}

// resolveActors batch-resolves created_by/updated_by across the page.
func (s *SecretsServer) resolveActors(ctx context.Context, rows []db.Secret) map[uuid.UUID]*typespb.Actor {
	if s.audit == nil {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.WarnContext(ctx, "secrets: actor resolution failed; returning without actors", "error", err)
		return nil
	}
	return actors
}

// parseSecretName extracts the leaf UUID from a full Secret resource name
// ("organizations/{org}[/spaces/{space}]/secrets/{uuid}"). A malformed name
// or non-UUID leaf is NotFound (the named secret doesn't exist).
func parseSecretName(name string) (uuid.UUID, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 {
		return uuid.Nil, apierr.NotFound("Secret", name)
	}
	id, err := uuid.Parse(name[idx+1:])
	if err != nil {
		return uuid.Nil, apierr.NotFound("Secret", name)
	}
	return id, nil
}

// checkScope enforces that a secret fetched by its (global) uuid actually
// belongs to the caller's interceptor-resolved scope. Without this, a caller
// with access to org A could read/mutate a secret in org B by crafting
// "organizations/A/secrets/{B-uuid}" — the interceptor gates on A but never
// verifies the uuid is A's. A mismatch is NotFound (don't leak existence).
func checkScope(row db.Secret, orgID uuid.UUID, spaceID pgtype.UUID, name string) error {
	if row.OrgID != orgID || row.SpaceID != spaceID {
		return apierr.NotFound("Secret", name)
	}
	return nil
}

// marshalAnnotations renders the labels map as JSONB. Empty map → "{}" (the
// column is NOT NULL DEFAULT '{}').
func marshalAnnotations(m map[string]string) (json.RawMessage, error) {
	if len(m) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}

func (s *SecretsServer) ListSecrets(ctx context.Context, req *secretsv1.ListSecretsRequest) (*secretsv1.ListSecretsResponse, error) {
	orgID, spaceID, prefix := s.scope(ctx)

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var cursor pgtype.UUID
	if tok := req.GetPageToken(); tok != "" {
		raw, err := s.codec.Decrypt(tok)
		if err != nil || len(raw) != 16 {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
		}
		var id uuid.UUID
		copy(id[:], raw)
		cursor = convert.PgUUID(id)
	}

	rows, err := s.queries.ListSecretsByParent(ctx, db.ListSecretsByParentParams{
		OrgID:     orgID,
		SpaceID:   spaceID,
		Cursor:    cursor,
		PageLimit: pageSize + 1,
	})
	if err != nil {
		return nil, apierr.Internal("list secrets")
	}

	var nextPageToken string
	if int32(len(rows)) > pageSize {
		nextPageToken, err = filter.EncodeNextPageToken(s.codec, rows[pageSize].ID)
		if err != nil {
			return nil, apierr.Internal("encode page token")
		}
		rows = rows[:pageSize]
	}

	actors := s.resolveActors(ctx, rows)
	out := make([]*secretsv1.Secret, 0, len(rows))
	for _, r := range rows {
		out = append(out, convert.SecretToProto(r, prefix, actors))
	}
	return &secretsv1.ListSecretsResponse{Secrets: out, NextPageToken: nextPageToken}, nil
}

func (s *SecretsServer) GetSecret(ctx context.Context, req *secretsv1.GetSecretRequest) (*secretsv1.Secret, error) {
	id, err := parseSecretName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := s.scope(ctx)
	row, err := s.queries.GetSecret(ctx, id)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Secret", req.GetName())
	}
	if err := checkScope(row, orgID, spaceID, req.GetName()); err != nil {
		return nil, err
	}
	return convert.SecretToProto(row, prefix, s.resolveActors(ctx, []db.Secret{row})), nil
}

func (s *SecretsServer) CreateSecret(ctx context.Context, req *secretsv1.CreateSecretRequest) (*secretsv1.Secret, error) {
	in := req.GetSecret()
	value := in.GetValue()
	if len(value) == 0 {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("secret.value", "must not be empty"))
	}
	secretID := req.GetSecretId()
	if secretID == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("secret_id", "is required"))
	}

	orgID, spaceID, prefix := s.scope(ctx)

	// App-generate the id up front (v7 for time-ordered listing, matching
	// the column's uuidv7 intent) so the AAD is bound to it before encrypt.
	id, err := uuid.NewV7()
	if err != nil {
		return nil, apierr.Internal("generate secret id")
	}
	ciphertext, err := s.encryptor.Encrypt(value, secretAAD(id))
	if err != nil {
		slog.ErrorContext(ctx, "secrets: encrypt failed", "error", err)
		return nil, apierr.Internal("encrypt secret value")
	}
	annotations, err := marshalAnnotations(in.GetAnnotations())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("secret.annotations", err.Error()))
	}

	params := db.CreateSecretParams{
		ID:              id,
		OrgID:           orgID,
		SpaceID:         spaceID,
		SecretID:        secretID,
		DisplayName:     in.GetDisplayName(),
		ValueCiphertext: ciphertext,
		Annotations:     annotations,
		CreatedBy:       convert.PgUUID(server.MustUserID(ctx)),
	}
	// validate_only rolls back the insert but still runs it, so a would-fail
	// request (e.g. duplicate secret_id) returns the same error a live one
	// would. There are no non-DB side effects to guard here.
	row, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.Secret, error) {
		return qtx.CreateSecret(ctx, params)
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Secret", secretID)
	}
	return convert.SecretToProto(row, prefix, s.resolveActors(ctx, []db.Secret{row})), nil
}

func (s *SecretsServer) UpdateSecret(ctx context.Context, req *secretsv1.UpdateSecretRequest) (*secretsv1.Secret, error) {
	in := req.GetSecret()
	id, err := parseSecretName(in.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := s.scope(ctx)

	mask := req.GetUpdateMask()
	// inScope: an empty mask means all metadata fields are significant (AIP
	// full-replace). `value` is deliberately EXCLUDED from that default — it
	// is write-only and unreadable, so a maskless update must not demand a
	// value the client can't know. value rotates only when named explicitly
	// in the mask (valueInMask).
	inScope := func(field string) bool {
		if mask == nil || len(mask.GetPaths()) == 0 {
			return true
		}
		return slices.Contains(mask.GetPaths(), field)
	}
	valueInMask := mask != nil && slices.Contains(mask.GetPaths(), "value")

	params := db.UpdateSecretParams{
		ID:        id,
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
	}
	if inScope("display_name") {
		params.DisplayName = pgtype.Text{String: in.GetDisplayName(), Valid: true}
	}
	if inScope("annotations") {
		annotations, err := marshalAnnotations(in.GetAnnotations())
		if err != nil {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("secret.annotations", err.Error()))
		}
		params.Annotations = annotations
	}
	if valueInMask {
		value := in.GetValue()
		if len(value) == 0 {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("secret.value",
				"must not be empty when in the update mask — delete the secret to remove it"))
		}
		ciphertext, err := s.encryptor.Encrypt(value, secretAAD(id))
		if err != nil {
			slog.ErrorContext(ctx, "secrets: encrypt failed", "error", err)
			return nil, apierr.Internal("encrypt secret value")
		}
		params.ValueCiphertext = ciphertext
	}

	var row db.Secret
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetSecretForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "Secret", in.GetName())
		}
		if err := checkScope(existing, orgID, spaceID, in.GetName()); err != nil {
			return err
		}
		if etag := in.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Secret", in.GetName(), "etag mismatch")
		}
		row, err = qtx.UpdateSecret(ctx, params)
		if err != nil {
			return apierr.HandleResourceError(err, "Secret", in.GetName())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return convert.SecretToProto(row, prefix, s.resolveActors(ctx, []db.Secret{row})), nil
}

func (s *SecretsServer) DeleteSecret(ctx context.Context, req *secretsv1.DeleteSecretRequest) (*emptypb.Empty, error) {
	id, err := parseSecretName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := s.scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetSecretForUpdate(ctx, id)
		if err != nil {
			return apierr.HandleResourceError(err, "Secret", req.GetName())
		}
		if err := checkScope(existing, orgID, spaceID, req.GetName()); err != nil {
			return err
		}
		if etag := req.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Secret", req.GetName(), "etag mismatch")
		}
		// TODO(connectors): return FailedPrecondition if any connector still
		// references this secret. No connector resource exists yet, so there
		// is nothing to check — the guard lands with the connector resource.
		return qtx.DeleteSecret(ctx, id)
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
