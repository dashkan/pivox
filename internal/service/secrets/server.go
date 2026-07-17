// Package secrets implements the encrypted credential vault (the Secrets
// gRPC service). A Secret holds one opaque, write-only value encrypted at
// rest via the platform Encryptor and bound (via AAD) to the row's immutable
// id. Values are never returned; callers rotate by overwriting.
package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

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

// parseSecretName extracts the slug leaf from a full Secret resource name
// ("organizations/{org}[/spaces/{space}]/secrets/{slug}"). The slug is the
// user-assigned id, unique within the secret's parent scope; the handler
// resolves it to the internal uuid via a scoped (org + space + slug) lookup, so
// a name that names another org's secret simply finds no row in this scope
// (NotFound, no cross-scope leak). A name with no leaf is NotFound.
func parseSecretName(name string) (string, error) {
	idx := strings.LastIndex(name, "/")
	if idx < 0 || idx == len(name)-1 {
		return "", apierr.NotFound("Secret", name)
	}
	return name[idx+1:], nil
}

// marshalAnnotations renders the labels map as JSONB. Empty map → "{}" (the
// column is NOT NULL DEFAULT '{}').
func marshalAnnotations(m map[string]string) (json.RawMessage, error) {
	if len(m) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(m)
}

// ListSecrets is a dynamic AIP-160 filtered + AIP-132 sorted + compound-cursor
// keyset list, structurally identical to ListConnectors. The interceptor-
// resolved (org, space) is the NON-NEGOTIABLE base scope, ANDed as the base of
// the query; the request's filter/order_by layer ON TOP of it and can only
// narrow, never widen. Every value (scope ids, filter operands, cursor values,
// page size) is bound as a $N parameter by filter.BuildListQuery — nothing is
// string-interpolated — and column/direction come only from SecretFilter's
// whitelist. The value column is never selected here (SELECT * excludes nothing,
// but convert.SecretToProto never emits it). See
// docs/aip-list-transpiler-procedure.md.
func (s *SecretsServer) ListSecrets(ctx context.Context, req *secretsv1.ListSecretsRequest) (*secretsv1.ListSecretsResponse, error) {
	orgID, spaceID, prefix := s.scope(ctx)
	rf := filter.SecretFilter()
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	// Resolve order_by against the sortable whitelist (default: id). The plan
	// also tells the cursor codec whether the sort value is a timestamp.
	plan, err := filter.PlanOrderBy(rf, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}

	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	// Base scope: the interceptor-resolved org (always) + a space predicate that
	// depends on the parent's scope. The AIP filter is layered on top — it can
	// only narrow within the scope.
	//
	//   - Space-scoped parent (spaceID.Valid): narrow to that one space.
	//   - Org-level parent (!spaceID.Valid): the ROLLUP — org-direct rows
	//     (space_id NULL) PLUS every space's rows. No space_id predicate, so the
	//     org scope alone bounds the list. The permission interceptor already
	//     gated secrets.read at the org scope, which covers the whole org
	//     (org-direct + all spaces). Mirrors ListConnectors.
	base := []filter.Predicate{{SQL: "org_id = %s", Arg: orgID}}
	if spaceID.Valid {
		base = append(base, filter.Predicate{SQL: "space_id = %s", Arg: spaceID})
	}
	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     base,
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter, e.g.
		// an unknown field) — surface it as InvalidArgument on "filter".
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list secrets")
	}
	rows, err := filter.ScanSecrets(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list secrets")
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row (see the connectors comment for
	// the off-by-one this closes).
	rows, nextPageToken, err := filter.Paginate(rows, int(pageSize), func(last db.Secret) (string, error) {
		return filter.EncodeCursor(s.codec, plan, secretSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors := s.resolveActors(ctx, rows)
	// Per-row name prefix. A space-scoped list shares the single `prefix` (which
	// already carries /spaces/{space}) across every row. The org-level rollup
	// names each row by its actual location: an org-direct row (space_id NULL)
	// keeps the bare org prefix, while a space-scoped row gets its space segment,
	// with the page's distinct space slugs resolved in one batched lookup (no
	// N+1). Mirrors ListConnectors.
	var spaceSlugs map[uuid.UUID]string
	if !spaceID.Valid {
		// FAIL CLOSED: naming a space row without its /spaces/{slug} segment would
		// emit a well-formed org-direct name that mis-routes a later
		// Get/Update/Delete into the wrong scope, so surface the failure instead.
		spaceSlugs, err = s.resolveSpaceSlugs(ctx, orgID, rows)
		if err != nil {
			return nil, apierr.Internal(err, "resolve space slugs")
		}
	}
	out := make([]*secretsv1.Secret, 0, len(rows))
	for _, r := range rows {
		p := prefix
		if !spaceID.Valid && r.SpaceID.Valid {
			slug, ok := spaceSlugs[uuid.UUID(r.SpaceID.Bytes)]
			if !ok {
				// Cross-org anomaly (no same-org FK enforces a secret's space
				// belongs to its org). Unreachable via the API today. OMIT the row
				// rather than emit a mis-addressable org-direct name.
				slog.WarnContext(ctx, "secrets: space slug missing for rolled-up secret; omitting from response",
					"secret_id", r.ID, "space_id", uuid.UUID(r.SpaceID.Bytes))
				continue
			}
			p = prefix + "/spaces/" + slug
		}
		out = append(out, convert.SecretToProto(r, p, actors))
	}
	return &secretsv1.ListSecretsResponse{Secrets: out, NextPageToken: nextPageToken}, nil
}

// secretSortValue renders the primary order_by column's value for the given row
// as the string the compound page token carries. Timestamps use RFC3339Nano so
// filter.DecodeCursor can parse them back to an exact time.Time. For the default
// id ordering (plan.Field == "") the value is unused (EncodeCursor emits the
// id-only token), so "" is returned. Mirrors connectorSortValue.
func secretSortValue(plan filter.OrderByPlan, r db.Secret) string {
	switch plan.Field {
	case "displayName":
		return r.DisplayName
	case "createTime":
		return r.CreateTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
}

// resolveSpaceSlugs maps the distinct valid space ids across an org-level rollup
// page to their slug (spaces.name), scoped to orgID so a foreign org's space
// slug can never be resolved. One batched read (no N+1); a single autocommit
// statement needs no tx. The query error is PROPAGATED (fail closed): the caller
// must not render a page of space rows without their space segment, as the
// resulting org-direct names would mis-route later mutations. Mirrors the
// connectors helper of the same name.
func (s *SecretsServer) resolveSpaceSlugs(ctx context.Context, orgID uuid.UUID, rows []db.Secret) (map[uuid.UUID]string, error) {
	var ids []uuid.UUID
	seen := make(map[uuid.UUID]struct{})
	for _, r := range rows {
		if !r.SpaceID.Valid {
			continue
		}
		id := uuid.UUID(r.SpaceID.Bytes)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	slugByID := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return slugByID, nil
	}
	got, err := s.queries.GetSpaceSlugsByIDs(ctx, db.GetSpaceSlugsByIDsParams{
		Ids:   ids,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	for _, sr := range got {
		slugByID[sr.ID] = sr.Name
	}
	return slugByID, nil
}

func (s *SecretsServer) GetSecret(ctx context.Context, req *secretsv1.GetSecretRequest) (*secretsv1.Secret, error) {
	slug, err := parseSecretName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, prefix := s.scope(ctx)
	row, err := s.queries.GetSecretByParent(ctx, db.GetSecretByParentParams{
		OrgID:   orgID,
		SpaceID: spaceID,
		Slug:    slug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Secret", req.GetName())
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
		return nil, apierr.Internal(err, "generate secret id")
	}
	ciphertext, err := s.encryptor.Encrypt(value, secretAAD(id))
	if err != nil {
		slog.ErrorContext(ctx, "secrets: encrypt failed", "error", err)
		return nil, apierr.Internal(err, "encrypt secret value")
	}
	annotations, err := marshalAnnotations(in.GetAnnotations())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("secret.annotations", err.Error()))
	}

	params := db.CreateSecretParams{
		ID:              id,
		OrgID:           orgID,
		SpaceID:         spaceID,
		Slug:            secretID,
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
	slug, err := parseSecretName(in.GetName())
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

	// Encrypt the new value BEFORE the tx. The AAD binds ciphertext to the
	// secret's immutable uuid, so we first resolve that uuid via a NON-locking
	// read, then encrypt outside any lock — Tink's envelope Encrypt is an
	// uncancellable KMS round-trip, and holding a row's FOR UPDATE lock + tx +
	// pooled connection across an external call is exactly what we must avoid.
	// The in-tx re-lock below re-checks the uuid before writing.
	var (
		newCiphertext []byte
		encryptedID   uuid.UUID
	)
	if valueInMask {
		newValue := in.GetValue()
		if len(newValue) == 0 {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("secret.value",
				"must not be empty when in the update mask — delete the secret to remove it"))
		}
		existing, err := s.queries.GetSecretByParent(ctx, db.GetSecretByParentParams{
			OrgID:   orgID,
			SpaceID: spaceID,
			Slug:    slug,
		})
		if err != nil {
			return nil, apierr.HandleResourceError(err, "Secret", in.GetName())
		}
		ciphertext, err := s.encryptor.Encrypt(newValue, secretAAD(existing.ID))
		if err != nil {
			slog.ErrorContext(ctx, "secrets: encrypt failed", "error", err)
			return nil, apierr.Internal(err, "encrypt secret value")
		}
		newCiphertext = ciphertext
		encryptedID = existing.ID
	}

	var row db.Secret
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetSecretByParentForUpdate(ctx, db.GetSecretByParentForUpdateParams{
			OrgID:   orgID,
			SpaceID: spaceID,
			Slug:    slug,
		})
		if err != nil {
			return apierr.HandleResourceError(err, "Secret", in.GetName())
		}
		if etag := in.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Secret", in.GetName(), "etag mismatch")
		}
		params.ID = existing.ID
		if valueInMask {
			// The precomputed ciphertext is bound to the id read before the tx.
			// If the row was deleted + recreated with the same slug in the
			// meantime it now has a different uuid, so that ciphertext would be
			// mis-bound (its AAD wouldn't match). Bail so the client retries and
			// re-encrypts against the new row rather than persisting an
			// undecryptable value. (Retry-safe: nothing is committed here.)
			if existing.ID != encryptedID {
				return apierr.Aborted("Secret", in.GetName(), "secret was modified concurrently")
			}
			params.ValueCiphertext = newCiphertext
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
	slug, err := parseSecretName(req.GetName())
	if err != nil {
		return nil, err
	}
	orgID, spaceID, _ := s.scope(ctx)
	err = db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		existing, err := qtx.GetSecretByParentForUpdate(ctx, db.GetSecretByParentForUpdateParams{
			OrgID:   orgID,
			SpaceID: spaceID,
			Slug:    slug,
		})
		if err != nil {
			return apierr.HandleResourceError(err, "Secret", req.GetName())
		}
		if etag := req.GetEtag(); etag != "" && etag != existing.Etag {
			return apierr.Aborted("Secret", req.GetName(), "etag mismatch")
		}
		// Delete-guard: block if any connector's config still references this
		// secret via secret("…"). CreateConnector/UpdateConnector track those
		// refs in connector_secret_refs (in the same tx as the connector
		// write), so this in-tx lookup is authoritative. The FK CASCADE on
		// that table is only for org-purge tidiness — this app-level check is
		// the actual block. (A referencing connector is always in this
		// secret's own scope, since cross-scope refs are rejected at connector
		// write time, so naming its slug here leaks nothing.)
		refs, err := qtx.ConnectorsReferencingSecret(ctx, existing.ID)
		if err != nil {
			return apierr.Internal(err, "check secret references")
		}
		if len(refs) > 0 {
			slugs := make([]string, 0, len(refs))
			for _, r := range refs {
				slugs = append(slugs, r.Slug)
			}
			return apierr.FailedPrecondition(fmt.Sprintf(
				"secret is referenced by connector(s) %s; remove the reference before deleting",
				strings.Join(slugs, ", ")))
		}
		return qtx.DeleteSecret(ctx, existing.ID)
	})
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
