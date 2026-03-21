package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/dashkan/pivox-server/internal/apierr"
	"github.com/dashkan/pivox-server/internal/convert"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/filter"
	apiv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/api/v1"
)

type ApiKeysServer struct {
	apiv1.UnimplementedApiKeysServer
	db      db.DBTX
	queries *db.Queries
	filter  *filter.ResourceFilter
}

func NewApiKeysServer(pool db.DBTX, queries *db.Queries) *ApiKeysServer {
	return &ApiKeysServer{
		db:      pool,
		queries: queries,
		filter:  filter.ApiKeyFilter(),
	}
}

func (s *ApiKeysServer) CreateKey(ctx context.Context, req *apiv1.CreateKeyRequest) (*apiv1.Key, error) {
	parent := req.GetParent()
	key := req.GetKey()

	orgName, err := parseOrgParent(parent)
	if err != nil {
		return nil, handleResourceError(err, "Organization", parent)
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", parent)
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

	created, err := s.queries.CreateApiKey(ctx, db.CreateApiKeyParams{
		ID:           uuid.New(),
		OrgID:        org.ID,
		KeyID:        keyID,
		DisplayName:  key.GetDisplayName(),
		KeyString:    keyString,
		Annotations:  annotationsJSON,
		Restrictions: restrictionsBytes,
		CreatedBy:    "",
	})
	if err != nil {
		return nil, handleResourceError(err, "Key", "")
	}
	return convert.ApiKeyToProto(created, orgName), nil
}

func (s *ApiKeysServer) ListKeys(ctx context.Context, req *apiv1.ListKeysRequest) (*apiv1.ListKeysResponse, error) {
	orgName, err := parseOrgParent(req.GetParent())
	if err != nil {
		return nil, handleResourceError(err, "Organization", req.GetParent())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", req.GetParent())
	}

	rows, err := filter.Query(ctx, s.db, s.filter, filter.QueryParams{
		Filter:      req.GetFilter(),
		ParentID:    org.ID.String(),
		OrderBy:     req.GetOrderBy(),
		PageSize:    req.GetPageSize(),
		Cursor:      req.GetPageToken(),
		ShowDeleted: req.GetShowDeleted(),
	})
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid filter: %v", err)
	}

	results, err := filter.ScanApiKeys(rows)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "database error")
	}

	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}

	var nextPageToken string
	if int32(len(results)) > pageSize {
		nextPageToken = results[pageSize].ID.String()
		results = results[:pageSize]
	}

	keys := make([]*apiv1.Key, 0, len(results))
	for _, k := range results {
		keys = append(keys, convert.ApiKeyToProto(k, orgName))
	}

	return &apiv1.ListKeysResponse{
		Keys:          keys,
		NextPageToken: nextPageToken,
	}, nil
}

func (s *ApiKeysServer) GetKey(ctx context.Context, req *apiv1.GetKeyRequest) (*apiv1.Key, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", orgName)
	}
	key, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	return convert.ApiKeyToProto(key, orgName), nil
}

func (s *ApiKeysServer) GetKeyString(ctx context.Context, req *apiv1.GetKeyStringRequest) (*apiv1.GetKeyStringResponse, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", orgName)
	}
	key, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	return &apiv1.GetKeyStringResponse{
		KeyString: key.KeyString,
	}, nil
}

func (s *ApiKeysServer) UpdateKey(ctx context.Context, req *apiv1.UpdateKeyRequest) (*apiv1.Key, error) {
	key := req.GetKey()
	orgName, keyID, err := parseApiKeyName(key.GetName())
	if err != nil {
		return nil, handleResourceError(err, "Key", key.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, handleResourceError(err, "Key", key.GetName())
	}

	updateParams := db.UpdateApiKeyParams{
		ID:        existing.ID,
		UpdatedBy: "",
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
					return nil, apierr.Internal("failed to marshal annotations")
				}
				updateParams.Annotations = annotationsJSON
			case "restrictions":
				if restrictions := key.GetRestrictions(); restrictions != nil {
					restrictionsBytes, err := protojson.Marshal(restrictions)
					if err != nil {
						return nil, apierr.Internal("failed to marshal restrictions")
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
		return nil, handleResourceError(err, "Key", key.GetName())
	}

	return convert.ApiKeyToProto(updated, orgName), nil
}

func (s *ApiKeysServer) DeleteKey(ctx context.Context, req *apiv1.DeleteKeyRequest) (*apiv1.Key, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", orgName)
	}
	existing, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	result, err := s.queries.SoftDeleteApiKey(ctx, db.SoftDeleteApiKeyParams{ID: existing.ID, DeletedBy: ""})
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	return convert.ApiKeyToProto(result, orgName), nil
}

func (s *ApiKeysServer) UndeleteKey(ctx context.Context, req *apiv1.UndeleteKeyRequest) (*apiv1.Key, error) {
	orgName, keyID, err := parseApiKeyName(req.GetName())
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, handleResourceError(err, "Organization", orgName)
	}
	// Use GetApiKeyIncludingDeleted via org+keyID — need to look up by ID first.
	existing, err := s.queries.GetApiKeyByOrgAndKeyID(ctx, db.GetApiKeyByOrgAndKeyIDParams{OrgID: org.ID, KeyID: keyID})
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	result, err := s.queries.UndeleteApiKey(ctx, db.UndeleteApiKeyParams{ID: existing.ID, UpdatedBy: ""})
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetName())
	}
	return convert.ApiKeyToProto(result, orgName), nil
}

func (s *ApiKeysServer) LookupKey(ctx context.Context, req *apiv1.LookupKeyRequest) (*apiv1.LookupKeyResponse, error) {
	row, err := s.queries.LookupApiKeyByKeyString(ctx, req.GetKeyString())
	if err != nil {
		return nil, handleResourceError(err, "Key", req.GetKeyString())
	}
	org, err := s.queries.GetOrganization(ctx, row.OrgID)
	if err != nil {
		return nil, handleResourceError(err, "Organization", "")
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
