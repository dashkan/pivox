package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
)

type EndpointsServer struct {
	storagev1.UnimplementedEndpointsServer
	pool      *pgxpool.Pool
	queries   *db.Queries
	encryptor crypto.Encryptor
}

func NewEndpointsServer(pool *pgxpool.Pool, queries *db.Queries, enc crypto.Encryptor) *EndpointsServer {
	return &EndpointsServer{
		pool:      pool,
		queries:   queries,
		encryptor: enc,
	}
}

func parseEndpointName(name string) (orgName, gwName, endpointName string, err error) {
	// organizations/{org}/storageGateways/{gw}/endpoints/{endpoint}
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "storageGateways" || parts[4] != "endpoints" {
		return "", "", "", fmt.Errorf("invalid endpoint name %q", name)
	}
	return parts[1], parts[3], parts[5], nil
}

func (s *EndpointsServer) resolveEndpointGateway(ctx context.Context, orgName, gwName string) (db.StorageGateway, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return db.StorageGateway{}, apierr.HandleResourceError(err, "Organization", orgName)
	}
	gw, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return db.StorageGateway{}, apierr.HandleResourceError(err, "StorageGateway", fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName))
	}
	return gw, nil
}

// configToJSON serializes the endpoint configuration proto to the JSONB shape
// stored in the database.
func configToJSON(endpoint *storagev1.Endpoint) (json.RawMessage, error) {
	switch cfg := endpoint.GetConfiguration().(type) {
	case *storagev1.Endpoint_S3:
		if cfg.S3 == nil {
			return nil, fmt.Errorf("S3 configuration is required")
		}
		m := map[string]interface{}{
			"type":         "s3",
			"endpoint_uri": cfg.S3.GetEndpointUri(),
			"bucket":       cfg.S3.GetBucket(),
			"region":       cfg.S3.GetRegion(),
		}
		if ak := cfg.S3.GetAccessKey(); ak != nil {
			m["access_key"] = map[string]string{
				"access_key_id":     ak.GetAccessKeyId(),
				"secret_access_key": ak.GetSecretAccessKey(),
			}
		}
		return json.Marshal(m)
	case *storagev1.Endpoint_Filesystem:
		if cfg.Filesystem == nil {
			return nil, fmt.Errorf("filesystem configuration is required")
		}
		return json.Marshal(map[string]string{
			"type": "filesystem",
			"path": cfg.Filesystem.GetPath(),
		})
	default:
		return nil, fmt.Errorf("configuration is required")
	}
}

func (s *EndpointsServer) CreateEndpoint(ctx context.Context, req *storagev1.CreateEndpointRequest) (*longrunningpb.Operation, error) {
	orgName, gwName, err := parseGatewayParent(req.GetParent())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	endpoint := req.GetEndpoint()
	endpointID := req.GetEndpointId()
	if endpointID == "" {
		endpointID = uuid.New().String()[:8]
	}

	// Serialize configuration to JSONB.
	configJSON, err := configToJSON(endpoint)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("configuration", err.Error()))
	}

	var annotationsJSON json.RawMessage
	if annotations := endpoint.GetAnnotations(); annotations != nil {
		annotationsJSON, _ = json.Marshal(annotations)
	} else {
		annotationsJSON = json.RawMessage("{}")
	}

	// Map cache config from proto to DB fields.
	var cacheEnabled bool
	var cacheMaxSizeGb int32
	var cacheEviction db.EvictionPolicy = db.EvictionPolicyLRU
	var cacheTtlHours int32
	if cc := endpoint.GetCacheConfig(); cc != nil {
		cacheEnabled = cc.GetEnabled()
		cacheMaxSizeGb = cc.GetMaxSizeGb()
		cacheTtlHours = cc.GetTtlHours()
		switch cc.GetEvictionPolicy() {
		case storagev1.CacheConfig_LFU:
			cacheEviction = db.EvictionPolicyLFU
		default:
			cacheEviction = db.EvictionPolicyLRU
		}
	}

	result, err := s.queries.CreateStorageEndpoint(ctx, db.CreateStorageEndpointParams{
		ID:             uuid.New(),
		GatewayID:      gw.ID,
		Name:           endpointID,
		DisplayName:    endpoint.GetDisplayName(),
		Configuration:  configJSON,
		CacheEnabled:   cacheEnabled,
		CacheMaxSizeGb: cacheMaxSizeGb,
		CacheEviction:  cacheEviction,
		CacheTtlHours:  cacheTtlHours,
		Annotations:    annotationsJSON,
		CreatedBy:      "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Endpoint", "")
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return lro.DoneOperation(convert.EndpointToProto(result, gatewayName))
}

func (s *EndpointsServer) GetEndpoint(ctx context.Context, req *storagev1.GetEndpointRequest) (*storagev1.Endpoint, error) {
	orgName, gwName, endpointName, err := parseEndpointName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	endpoint, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Endpoint", req.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return convert.EndpointToProto(endpoint, gatewayName), nil
}

func (s *EndpointsServer) ListEndpoints(ctx context.Context, req *storagev1.ListEndpointsRequest) (*storagev1.ListEndpointsResponse, error) {
	orgName, gwName, err := parseGatewayParent(req.GetParent())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	endpoints, err := s.queries.ListStorageEndpointsByGateway(ctx, gw.ID)
	if err != nil {
		return nil, apierr.Internal("failed to list endpoints")
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	pbEndpoints := make([]*storagev1.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		pbEndpoints = append(pbEndpoints, convert.EndpointToProto(ep, gatewayName))
	}

	return &storagev1.ListEndpointsResponse{
		Endpoints: pbEndpoints,
	}, nil
}

func (s *EndpointsServer) UpdateEndpoint(ctx context.Context, req *storagev1.UpdateEndpointRequest) (*longrunningpb.Operation, error) {
	endpoint := req.GetEndpoint()
	orgName, gwName, endpointName, err := parseEndpointName(endpoint.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("endpoint.name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Endpoint", endpoint.GetName())
	}

	updateParams := db.UpdateStorageEndpointParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName.String = endpoint.GetDisplayName()
				updateParams.DisplayName.Valid = true
			case "s3.credentials", "s3", "filesystem", "configuration":
				configJSON, cfgErr := configToJSON(endpoint)
				if cfgErr != nil {
					return nil, apierr.InvalidArgument(apierr.FieldViolation("configuration", cfgErr.Error()))
				}
				updateParams.Configuration = configJSON
			case "cache_config", "cache_config.enabled", "cache_config.max_size_gb", "cache_config.eviction_policy", "cache_config.ttl_hours":
				if cc := endpoint.GetCacheConfig(); cc != nil {
					updateParams.CacheEnabled = pgtype.Bool{Bool: cc.GetEnabled(), Valid: true}
					updateParams.CacheMaxSizeGb = pgtype.Int4{Int32: cc.GetMaxSizeGb(), Valid: true}
					updateParams.CacheTtlHours = pgtype.Int4{Int32: cc.GetTtlHours(), Valid: true}
					switch cc.GetEvictionPolicy() {
					case storagev1.CacheConfig_LFU:
						updateParams.CacheEviction = db.NullEvictionPolicy{EvictionPolicy: db.EvictionPolicyLFU, Valid: true}
					default:
						updateParams.CacheEviction = db.NullEvictionPolicy{EvictionPolicy: db.EvictionPolicyLRU, Valid: true}
					}
				}
			case "annotations":
				annotationsJSON, marshalErr := json.Marshal(endpoint.GetAnnotations())
				if marshalErr != nil {
					return nil, apierr.Internal("failed to marshal annotations")
				}
				updateParams.Annotations = annotationsJSON
			}
		}
	} else {
		updateParams.DisplayName.String = endpoint.GetDisplayName()
		updateParams.DisplayName.Valid = true
		if endpoint.GetConfiguration() != nil {
			configJSON, _ := configToJSON(endpoint)
			updateParams.Configuration = configJSON
		}
		if cc := endpoint.GetCacheConfig(); cc != nil {
			updateParams.CacheEnabled = pgtype.Bool{Bool: cc.GetEnabled(), Valid: true}
			updateParams.CacheMaxSizeGb = pgtype.Int4{Int32: cc.GetMaxSizeGb(), Valid: true}
			updateParams.CacheTtlHours = pgtype.Int4{Int32: cc.GetTtlHours(), Valid: true}
			switch cc.GetEvictionPolicy() {
			case storagev1.CacheConfig_LFU:
				updateParams.CacheEviction = db.NullEvictionPolicy{EvictionPolicy: db.EvictionPolicyLFU, Valid: true}
			default:
				updateParams.CacheEviction = db.NullEvictionPolicy{EvictionPolicy: db.EvictionPolicyLRU, Valid: true}
			}
		}
		if annotations := endpoint.GetAnnotations(); annotations != nil {
			annotationsJSON, _ := json.Marshal(annotations)
			updateParams.Annotations = annotationsJSON
		}
	}

	result, err := s.queries.UpdateStorageEndpoint(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Endpoint", endpoint.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return lro.DoneOperation(convert.EndpointToProto(result, gatewayName))
}

func (s *EndpointsServer) DeleteEndpoint(ctx context.Context, req *storagev1.DeleteEndpointRequest) (*longrunningpb.Operation, error) {
	orgName, gwName, endpointName, err := parseEndpointName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Endpoint", req.GetName())
	}

	err = s.queries.DeleteStorageEndpoint(ctx, existing.ID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Endpoint", req.GetName())
	}

	return lro.DoneOperation(&storagev1.Endpoint{
		Name: req.GetName(),
	})
}
