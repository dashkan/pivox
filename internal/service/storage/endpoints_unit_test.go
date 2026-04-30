package storage

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	db "github.com/dashkan/pivox/internal/db/generated"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

var (
	endpointID   = uuid.MustParse("0192a000-0030-7000-8000-000000000001")
	testEndpoint = db.StorageEndpoint{
		ID:             endpointID,
		GatewayID:      gwID,
		Name:           "ep-s3",
		DisplayName:    "S3 Endpoint",
		Configuration:  json.RawMessage(`{"type":"s3","endpoint_uri":"https://s3.amazonaws.com","bucket":"my-bucket","region":"us-east-1"}`),
		CacheEnabled:   true,
		CacheMaxSizeGb: 50,
		CacheEviction:  db.EvictionPolicyLRU,
		CacheTtlHours:  24,
		Annotations:    json.RawMessage(`{"tier":"hot"}`),
		State:          db.EndpointStateACTIVE,
		Etag:           "etag-ep-1",
		Revision:       1,
		CreateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testFSEndpoint = db.StorageEndpoint{
		ID:             uuid.MustParse("0192a000-0031-7000-8000-000000000001"),
		GatewayID:      gwID,
		Name:           "ep-fs",
		DisplayName:    "FS Endpoint",
		Configuration:  json.RawMessage(`{"type":"filesystem","path":"/mnt/data"}`),
		CacheEnabled:   false,
		CacheMaxSizeGb: 0,
		CacheEviction:  db.EvictionPolicyLRU,
		CacheTtlHours:  0,
		Annotations:    json.RawMessage(`{}`),
		State:          db.EndpointStateACTIVE,
		Etag:           "etag-ep-2",
		Revision:       1,
		CreateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

func newEndpointsServer(q *mocks.MockQuerier) *EndpointsServer {
	return &EndpointsServer{
		queries:   q,
		encryptor: nil,
	}
}

// ---------------------------------------------------------------------------
// CreateEndpoint
// ---------------------------------------------------------------------------

func TestUnit_CreateEndpoint_S3(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("CreateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.CreateStorageEndpointParams) bool {
		return p.GatewayID == gwID && p.Name == "ep-s3" && p.DisplayName == "S3 Endpoint"
	})).Return(testEndpoint, nil)

	resp, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:     "organizations/acme/storageGateways/gw-1",
		EndpointId: "ep-s3",
		Endpoint: &storagev1.Endpoint{
			DisplayName: "S3 Endpoint",
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{
					EndpointUri: "https://s3.amazonaws.com",
					Bucket:      "my-bucket",
					Region:      "us-east-1",
				},
			},
			CacheConfig: &storagev1.CacheConfig{
				Enabled:   true,
				MaxSizeGb: 50,
				TtlHours:  24,
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(storagev1.Endpoint)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/endpoints/ep-s3", inner.GetName())
	assert.Equal(t, "S3 Endpoint", inner.GetDisplayName())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateEndpoint_Filesystem(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("CreateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.CreateStorageEndpointParams) bool {
		return p.GatewayID == gwID && p.Name == "ep-fs"
	})).Return(testFSEndpoint, nil)

	resp, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:     "organizations/acme/storageGateways/gw-1",
		EndpointId: "ep-fs",
		Endpoint: &storagev1.Endpoint{
			DisplayName: "FS Endpoint",
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfiguration{
					Path: "/mnt/data",
				},
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(storagev1.Endpoint)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/endpoints/ep-fs", inner.GetName())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetEndpoint
// ---------------------------------------------------------------------------

func TestUnit_GetEndpoint_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)

	resp, err := srv.GetEndpoint(ctx, &storagev1.GetEndpointRequest{
		Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/endpoints/ep-s3", resp.GetName())
	assert.Equal(t, "S3 Endpoint", resp.GetDisplayName())
	// Verify the S3 config is returned (credentials stripped by EndpointToProto).
	s3Cfg := resp.GetS3()
	require.NotNil(t, s3Cfg)
	assert.Equal(t, "https://s3.amazonaws.com", s3Cfg.GetEndpointUri())
	assert.Equal(t, "my-bucket", s3Cfg.GetBucket())
	assert.Equal(t, "us-east-1", s3Cfg.GetRegion())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetEndpoint_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "missing",
	}).Return(db.StorageEndpoint{}, pgx.ErrNoRows)

	_, err := srv.GetEndpoint(ctx, &storagev1.GetEndpointRequest{
		Name: "organizations/acme/storageGateways/gw-1/endpoints/missing",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// DeleteEndpoint
// ---------------------------------------------------------------------------

func TestUnit_DeleteEndpoint_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("DeleteStorageEndpoint", mock.Anything, endpointID).Return(nil)

	resp, err := srv.DeleteEndpoint(ctx, &storagev1.DeleteEndpointRequest{
		Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// CreateEndpoint — error paths
// ---------------------------------------------------------------------------

func TestUnit_CreateEndpoint_InvalidParent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	_, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent: "bad-parent",
		Endpoint: &storagev1.Endpoint{
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{Bucket: "b"},
			},
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_CreateEndpoint_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent: "organizations/no-org/storageGateways/gw-1",
		Endpoint: &storagev1.Endpoint{
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{Bucket: "b"},
			},
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateEndpoint_InvalidConfig(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)

	// No configuration set — should fail.
	_, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:   "organizations/acme/storageGateways/gw-1",
		Endpoint: &storagev1.Endpoint{},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateEndpoint_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("CreateStorageEndpoint", mock.Anything, mock.Anything).
		Return(db.StorageEndpoint{}, pgx.ErrNoRows)

	_, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
		Endpoint: &storagev1.Endpoint{
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{Bucket: "b"},
			},
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateEndpoint_WithLFUCachePolicy(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("CreateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.CreateStorageEndpointParams) bool {
		return p.CacheEviction == db.EvictionPolicyLFU
	})).Return(testEndpoint, nil)

	resp, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
		Endpoint: &storagev1.Endpoint{
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{Bucket: "b"},
			},
			CacheConfig: &storagev1.CacheConfig{
				Enabled:        true,
				MaxSizeGb:      100,
				EvictionPolicy: storagev1.CacheConfig_LFU,
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateEndpoint_WithAnnotations(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("CreateStorageEndpoint", mock.Anything, mock.Anything).Return(testEndpoint, nil)

	resp, err := srv.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
		Endpoint: &storagev1.Endpoint{
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfiguration{Path: "/tmp"},
			},
			Annotations: map[string]string{"tier": "warm"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetEndpoint — error paths
// ---------------------------------------------------------------------------

func TestUnit_GetEndpoint_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	_, err := srv.GetEndpoint(ctx, &storagev1.GetEndpointRequest{
		Name: "bad-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_GetEndpoint_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing-gw",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.GetEndpoint(ctx, &storagev1.GetEndpointRequest{
		Name: "organizations/acme/storageGateways/missing-gw/endpoints/ep-s3",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// ListEndpoints — error paths
// ---------------------------------------------------------------------------

func TestUnit_ListEndpoints_InvalidParent(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	_, err := srv.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
		Parent: "bad-parent",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_ListEndpoints_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing-gw",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
		Parent: "organizations/acme/storageGateways/missing-gw",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListEndpoints_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gwID).
		Return([]db.StorageEndpoint(nil), fmt.Errorf("db error"))

	_, err := srv.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// configToJSON
// ---------------------------------------------------------------------------

func TestUnit_ConfigToJSON_S3(t *testing.T) {
	ep := &storagev1.Endpoint{
		Configuration: &storagev1.Endpoint_S3{
			S3: &storagev1.S3Configuration{
				EndpointUri: "https://s3.amazonaws.com",
				Bucket:      "test-bucket",
				Region:      "eu-west-1",
				Credentials: &storagev1.S3Configuration_AccessKey{
					AccessKey: &storagev1.S3AccessKeyCredentials{
						AccessKeyId:     "AKID",
						SecretAccessKey: "SECRET",
					},
				},
			},
		},
	}

	raw, err := configToJSON(ep)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "s3", m["type"])
	assert.Equal(t, "https://s3.amazonaws.com", m["endpoint_uri"])
	assert.Equal(t, "test-bucket", m["bucket"])
	assert.Equal(t, "eu-west-1", m["region"])
	// Verify access_key is included in the JSON.
	ak, ok := m["access_key"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "AKID", ak["access_key_id"])
	assert.Equal(t, "SECRET", ak["secret_access_key"])
}

func TestUnit_ConfigToJSON_Filesystem(t *testing.T) {
	ep := &storagev1.Endpoint{
		Configuration: &storagev1.Endpoint_Filesystem{
			Filesystem: &storagev1.FileSystemConfiguration{
				Path: "/mnt/storage",
			},
		},
	}

	raw, err := configToJSON(ep)
	require.NoError(t, err)

	var m map[string]string
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "filesystem", m["type"])
	assert.Equal(t, "/mnt/storage", m["path"])
}

func TestUnit_ConfigToJSON_NilConfig(t *testing.T) {
	ep := &storagev1.Endpoint{}

	_, err := configToJSON(ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration is required")
}

func TestUnit_ConfigToJSON_S3NilInner(t *testing.T) {
	ep := &storagev1.Endpoint{
		Configuration: &storagev1.Endpoint_S3{S3: nil},
	}

	_, err := configToJSON(ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "S3 configuration is required")
}

func TestUnit_ConfigToJSON_FilesystemNilInner(t *testing.T) {
	ep := &storagev1.Endpoint{
		Configuration: &storagev1.Endpoint_Filesystem{Filesystem: nil},
	}

	_, err := configToJSON(ep)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filesystem configuration is required")
}

func TestUnit_ConfigToJSON_S3WithoutAccessKey(t *testing.T) {
	ep := &storagev1.Endpoint{
		Configuration: &storagev1.Endpoint_S3{
			S3: &storagev1.S3Configuration{
				EndpointUri: "https://s3.amazonaws.com",
				Bucket:      "my-bucket",
				Region:      "us-west-2",
			},
		},
	}

	raw, err := configToJSON(ep)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &m))
	assert.Equal(t, "s3", m["type"])
	_, hasKey := m["access_key"]
	assert.False(t, hasKey)
}

// ---------------------------------------------------------------------------
// DeleteEndpoint — error paths
// ---------------------------------------------------------------------------

func TestUnit_DeleteEndpoint_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	_, err := srv.DeleteEndpoint(ctx, &storagev1.DeleteEndpointRequest{
		Name: "bad-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_DeleteEndpoint_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing-gw",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.DeleteEndpoint(ctx, &storagev1.DeleteEndpointRequest{
		Name: "organizations/acme/storageGateways/missing-gw/endpoints/ep-s3",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteEndpoint_EndpointNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "missing-ep",
	}).Return(db.StorageEndpoint{}, pgx.ErrNoRows)

	_, err := srv.DeleteEndpoint(ctx, &storagev1.DeleteEndpointRequest{
		Name: "organizations/acme/storageGateways/gw-1/endpoints/missing-ep",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteEndpoint_DeleteFails(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("DeleteStorageEndpoint", mock.Anything, endpointID).
		Return(pgx.ErrNoRows)

	_, err := srv.DeleteEndpoint(ctx, &storagev1.DeleteEndpointRequest{
		Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// UpdateEndpoint
// ---------------------------------------------------------------------------

func TestUnit_UpdateEndpoint_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	updatedEndpoint := testEndpoint
	updatedEndpoint.DisplayName = "Renamed S3 Endpoint"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID &&
			p.DisplayName.Valid && p.DisplayName.String == "Renamed S3 Endpoint" &&
			p.Configuration == nil && // not in mask
			// caller's pivox_user_id flows through to UpdatedBy (audit
			// regression guard: pre-B-5 the field was the zero
			// pgtype.UUID{} on every Update endpoint call).
			p.UpdatedBy.Valid && p.UpdatedBy.Bytes == storageCallerPivoxUUID
	})).Return(updatedEndpoint, nil)

	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name:        "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			DisplayName: "Renamed S3 Endpoint",
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(storagev1.Endpoint)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/endpoints/ep-s3", inner.GetName())
	assert.Equal(t, "Renamed S3 Endpoint", inner.GetDisplayName())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// ListEndpoints
// ---------------------------------------------------------------------------

func TestUnit_ListEndpoints_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("ListStorageEndpointsByGateway", mock.Anything, gwID).
		Return([]db.StorageEndpoint{testEndpoint, testFSEndpoint}, nil)

	resp, err := srv.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	require.Len(t, resp.GetEndpoints(), 2)
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/endpoints/ep-s3", resp.GetEndpoints()[0].GetName())
	assert.Equal(t, "S3 Endpoint", resp.GetEndpoints()[0].GetDisplayName())
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/endpoints/ep-fs", resp.GetEndpoints()[1].GetName())
	assert.Equal(t, "FS Endpoint", resp.GetEndpoints()[1].GetDisplayName())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// UpdateEndpoint — error paths and additional field mask paths
// ---------------------------------------------------------------------------

func TestUnit_UpdateEndpoint_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	_, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "bad-name",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_UpdateEndpoint_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing-gw",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "organizations/acme/storageGateways/missing-gw/endpoints/ep-s3",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_EndpointNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "missing-ep",
	}).Return(db.StorageEndpoint{}, pgx.ErrNoRows)

	_, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "organizations/acme/storageGateways/gw-1/endpoints/missing-ep",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_UpdateFails(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.Anything).
		Return(db.StorageEndpoint{}, pgx.ErrNoRows)

	_, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name:        "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			DisplayName: "New Name",
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name"},
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_FieldMask_Configuration(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID && p.Configuration != nil
	})).Return(testEndpoint, nil)

	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{
					Bucket: "new-bucket",
					Region: "eu-west-1",
				},
			},
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"s3"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_FieldMask_InvalidConfig(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)

	// No configuration set but config path is in mask — should fail.
	_, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"configuration"},
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_FieldMask_CacheConfig_LFU(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID && p.CacheEnabled.Valid && p.CacheEviction.EvictionPolicy == db.EvictionPolicyLFU
	})).Return(testEndpoint, nil)

	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			CacheConfig: &storagev1.CacheConfig{
				Enabled:        true,
				MaxSizeGb:      200,
				EvictionPolicy: storagev1.CacheConfig_LFU,
				TtlHours:       48,
			},
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"cache_config"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_FieldMask_Annotations(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID && p.Annotations != nil
	})).Return(testEndpoint, nil)

	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name:        "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			Annotations: map[string]string{"tier": "cold"},
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"annotations"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_NoMask_AllFields(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID && p.DisplayName.Valid && p.Configuration != nil && p.CacheEnabled.Valid
	})).Return(testEndpoint, nil)

	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name:        "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			DisplayName: "Full Update",
			Configuration: &storagev1.Endpoint_S3{
				S3: &storagev1.S3Configuration{Bucket: "b"},
			},
			CacheConfig: &storagev1.CacheConfig{
				Enabled:   true,
				MaxSizeGb: 100,
			},
			Annotations: map[string]string{"env": "prod"},
		},
		// No mask — update all fields.
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_NoMask_NoConfig(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID && p.Configuration == nil
	})).Return(testEndpoint, nil)

	// No configuration in request and no mask — config should not be updated.
	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name:        "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			DisplayName: "Name Only",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateEndpoint_NoMask_LFUCachePolicy(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newEndpointsServer(mockQ)
	ctx := callerCtx()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("GetStorageEndpointByName", mock.Anything, db.GetStorageEndpointByNameParams{
		GatewayID: gwID,
		Name:      "ep-s3",
	}).Return(testEndpoint, nil)
	mockQ.On("UpdateStorageEndpoint", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageEndpointParams) bool {
		return p.ID == endpointID && p.CacheEviction.EvictionPolicy == db.EvictionPolicyLFU
	})).Return(testEndpoint, nil)

	resp, err := srv.UpdateEndpoint(ctx, &storagev1.UpdateEndpointRequest{
		Endpoint: &storagev1.Endpoint{
			Name: "organizations/acme/storageGateways/gw-1/endpoints/ep-s3",
			CacheConfig: &storagev1.CacheConfig{
				Enabled:        true,
				MaxSizeGb:      100,
				EvictionPolicy: storagev1.CacheConfig_LFU,
			},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}
