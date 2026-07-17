package apikeys

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/server"
)

type ApiKeysServer struct {
	apiv1.UnimplementedApiKeysServer
	pool    db.RWPool
	queries db.Querier
	filter  *filter.ResourceFilter
	codec   *appkey.Codec
	audit   *audit.Resolver
}

// Config is the constructor input for ApiKeysServer.
type Config struct {
	// Pool is the database pool — DBTX for filter reads + TxBeginner
	// for any future tx-wrapped paths. Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes resource names. Required.
	Codec *appkey.Codec
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewApiKeysServer constructs the server from cfg. Panics on a
// missing required field — a startup-time programmer error rather
// than a runtime failure, so unwound at the call site by reading the
// panic message during boot.
func NewApiKeysServer(cfg Config) *ApiKeysServer {
	if cfg.Pool == nil {
		panic("apikeys: Config.Pool is required")
	}
	if cfg.Queries == nil {
		panic("apikeys: Config.Queries is required")
	}
	if cfg.Codec == nil {
		panic("apikeys: Config.Codec is required")
	}
	return &ApiKeysServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		filter:  filter.ApiKeyFilter(),
		codec:   cfg.Codec,
		audit:   cfg.AuditResolver,
	}
}

// resolveApiKeyActors gathers created_by/updated_by/deleted_by UUIDs
// across the page and resolves them in a single batched call.
func (s *ApiKeysServer) resolveApiKeyActors(ctx context.Context, rows []db.ApiKey) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*3)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
		if r.DeletedBy.Valid {
			ids = append(ids, r.DeletedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve api key actors failed", "error", err)
		return nil, apierr.Internal(err, "resolve actors")
	}
	return actors, nil
}

func (s *ApiKeysServer) CreateKey(ctx context.Context, req *apiv1.CreateKeyRequest) (*apiv1.Key, error) {
	parent := req.GetParent()
	key := req.GetKey()

	orgName, err := parseOrgParent(parent)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", parent)
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", parent)
	}

	keyID := req.GetKeyId()
	if keyID == "" {
		keyID = uuid.New().String()
	}
	keyString := generateKeyString()

	var annotationsJSON json.RawMessage
	if annotations := key.GetAnnotations(); annotations != nil {
		annotationsJSON, _ = json.Marshal(annotations)
	} else {
		annotationsJSON = json.RawMessage("{}")
	}

	var restrictionsBytes []byte
	if restrictions := key.GetRestrictions(); restrictions != nil {
		restrictionsBytes, _ = protojson.Marshal(restrictions)
	}

	// validate_only runs the INSERT against real constraints and rolls it
	// back, so a would-fail request (e.g. duplicate key_id) returns the
	// same error a live one would while persisting nothing.
	created, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.ApiKey, error) {
		return qtx.CreateApiKey(ctx, db.CreateApiKeyParams{
			ID:           uuid.New(),
			OrgID:        org.ID,
			KeyID:        keyID,
			DisplayName:  key.GetDisplayName(),
			KeyString:    keyString,
			Annotations:  annotationsJSON,
			Restrictions: restrictionsBytes,
			CreatedBy:    convert.PgUUID(server.MustUserID(ctx)),
		})
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", "")
	}
	actors, resolveErr := s.resolveApiKeyActors(ctx, []db.ApiKey{created})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create api key: actor resolution failed; returning proto without audit actors",
			"key_id", created.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ApiKeyToProto(created, orgName, actors), nil
}

func (s *ApiKeysServer) ListKeys(ctx context.Context, req *apiv1.ListKeysRequest) (*apiv1.ListKeysResponse, error) {
	orgName, err := parseOrgParent(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}

	rows, err := filter.Query(ctx, s.pool, s.filter, filter.QueryParams{
		Filter:      req.GetFilter(),
		ParentID:    org.ID.String(),
		OrderBy:     req.GetOrderBy(),
		PageSize:    req.GetPageSize(),
		Cursor:      req.GetPageToken(),
		ShowDeleted: req.GetShowDeleted(),
		Codec:       s.codec,
	})
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	results, err := filter.ScanApiKeys(rows)
	if err != nil {
		return nil, apierr.Internal(err, "database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	// filter.Paginate trims the over-fetched result to pageSize and derives the
	// next-page token from the LAST RETURNED row — never the first un-returned
	// row (the resume predicate is a strict `>`, so results[pageSize] would be
	// silently dropped next page). Owning both the trim and the token here makes
	// that off-by-one unrepresentable at the call site.
	results, nextPageToken, err := filter.Paginate(results, int(pageSize), func(last db.ApiKey) (string, error) {
		return filter.EncodeNextPageToken(s.codec, last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	actors, err := s.resolveApiKeyActors(ctx, results)
	if err != nil {
		return nil, err
	}
	keys := make([]*apiv1.Key, 0, len(results))
	for _, k := range results {
		keys = append(keys, convert.ApiKeyToProto(k, orgName, actors))
	}

	return &apiv1.ListKeysResponse{
		Keys:          keys,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *ApiKeysServer) GetKey(ctx context.Context, req *apiv1.GetKeyRequest) (*apiv1.Key, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	key, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	actors, err := s.resolveApiKeyActors(ctx, []db.ApiKey{key})
	if err != nil {
		return nil, err
	}
	return convert.ApiKeyToProto(key, orgName, actors), nil
}

func (s *ApiKeysServer) GetKeyString(ctx context.Context, req *apiv1.GetKeyStringRequest) (*apiv1.GetKeyStringResponse, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	key, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	return &apiv1.GetKeyStringResponse{
		KeyString: key.KeyString,
	}, nil
}

func (s *ApiKeysServer) UpdateKey(ctx context.Context, req *apiv1.UpdateKeyRequest) (*apiv1.Key, error) {
	key := req.GetKey()
	orgName, keyID, err := parseApiKeyName(key.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", key.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", key.GetName())
	}

	updateParams := db.UpdateApiKeyParams{
		ID:        existing.ID,
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: key.GetDisplayName(), Valid: true}
			case "annotations":
				annotationsJSON, err := json.Marshal(key.GetAnnotations())
				if err != nil {
					return nil, apierr.Internal(err, "failed to marshal annotations")
				}
				updateParams.Annotations = annotationsJSON
			case "restrictions":
				if restrictions := key.GetRestrictions(); restrictions != nil {
					restrictionsBytes, err := protojson.Marshal(restrictions)
					if err != nil {
						return nil, apierr.Internal(err, "failed to marshal restrictions")
					}
					updateParams.Restrictions = restrictionsBytes
				}
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: key.GetDisplayName(), Valid: true}
		if annotations := key.GetAnnotations(); annotations != nil {
			annotationsJSON, _ := json.Marshal(annotations)
			updateParams.Annotations = annotationsJSON
		}
		if restrictions := key.GetRestrictions(); restrictions != nil {
			restrictionsBytes, _ := protojson.Marshal(restrictions)
			updateParams.Restrictions = restrictionsBytes
		}
	}

	updated, err := s.queries.UpdateApiKey(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", key.GetName())
	}

	actors, resolveErr := s.resolveApiKeyActors(ctx, []db.ApiKey{updated})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update api key: actor resolution failed; returning proto without audit actors",
			"key_id", updated.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ApiKeyToProto(updated, orgName, actors), nil
}

func (s *ApiKeysServer) DeleteKey(ctx context.Context, req *apiv1.DeleteKeyRequest) (*apiv1.Key, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	existing, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	result, err := s.queries.SoftDeleteApiKey(ctx, db.SoftDeleteApiKeyParams{ID: existing.ID, DeletedBy: convert.PgUUID(server.MustUserID(ctx))})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	actors, resolveErr := s.resolveApiKeyActors(ctx, []db.ApiKey{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "delete api key: actor resolution failed; returning proto without audit actors",
			"key_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ApiKeyToProto(result, orgName, actors), nil
}

func (s *ApiKeysServer) UndeleteKey(ctx context.Context, req *apiv1.UndeleteKeyRequest) (*apiv1.Key, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}
	// Use GetApiKeyIncludingDeleted via org+keyID -- need to look up by ID first.
	existing, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	result, err := s.queries.UndeleteApiKey(ctx, db.UndeleteApiKeyParams{ID: existing.ID, UpdatedBy: convert.PgUUID(server.MustUserID(ctx))})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetName())
	}
	actors, resolveErr := s.resolveApiKeyActors(ctx, []db.ApiKey{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "undelete api key: actor resolution failed; returning proto without audit actors",
			"key_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.ApiKeyToProto(result, orgName, actors), nil
}

func (s *ApiKeysServer) LookupKey(ctx context.Context, req *apiv1.LookupKeyRequest) (*apiv1.LookupKeyResponse, error) {
	row, err := s.queries.LookupApiKeyByKeyString(ctx, req.GetKeyString())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Key", req.GetKeyString())
	}
	org, err := s.queries.GetOrganization(ctx, row.OrgID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", "")
	}
	return &apiv1.LookupKeyResponse{
		Parent: "organizations/" + org.Name,
		Name:   fmt.Sprintf("organizations/%s/keys/%s", org.Name, row.KeyID),
	}, nil
}

// parseOrgParent parses "organizations/{name}" and returns the org name.
func parseOrgParent(parent string) (string, error) {
	parts := strings.SplitN(parent, "/", 2)
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", fmt.Errorf("invalid organization parent %q", parent)
	}
	return parts[1], nil
}

func parseApiKeyName(name string) (string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "keys" {
		return "", "", fmt.Errorf("invalid API key name %q", name)
	}
	return parts[1], parts[3], nil
}

func generateKeyString() string {
	const charset = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 39)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}
