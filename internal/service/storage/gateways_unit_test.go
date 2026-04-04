package storage

import (
	"context"
	"encoding/json"
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

	"github.com/dashkan/pivox/internal/agentstream"
	db "github.com/dashkan/pivox/internal/db/generated"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// Shared test fixtures for the storage package tests.
var (
	gwOrgID = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	gwID    = uuid.MustParse("0192a000-0010-7000-8000-000000000001")
	gwOrg   = db.Organization{
		ID:          gwOrgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
	testGateway = db.StorageGateway{
		ID:                gwID,
		OrgID:             gwOrgID,
		Name:              "gw-1",
		DisplayName:       "Gateway One",
		IpAddresses:       []string{"10.0.0.1"},
		RegistrationToken: "reg-token-abc",
		TargetVersion:     "1.2.0",
		CurrentVersion:    "1.1.0",
		Hostname:          "gw-1.storage.pivox.app",
		Annotations:       json.RawMessage(`{"env":"prod"}`),
		State:             db.StorageGatewayStateACTIVE,
		CertState:         db.CertStatePENDING,
		Etag:              "etag-gw-1",
		Revision:          1,
		CreateTime:        time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:        time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

func newGatewayServer(q *mocks.MockQuerier) *StorageGatewaysServer {
	conns := agentstream.NewConnectionManager()
	return &StorageGatewaysServer{
		queries:           q,
		encryptor:         nil,
		conns:             conns,
		sessionSigningKey: []byte("test-key"),
	}
}

// ---------------------------------------------------------------------------
// CreateStorageGateway
// ---------------------------------------------------------------------------

func TestUnit_CreateStorageGateway_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("CreateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.CreateStorageGatewayParams) bool {
		return p.OrgID == gwOrgID && p.Name == "gw-1" && p.DisplayName == "Gateway One"
	})).Return(testGateway, nil)

	resp, err := srv.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent:           "organizations/acme",
		StorageGatewayId: "gw-1",
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: "Gateway One",
			IpAddresses: []string{"10.0.0.1"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(storagev1.StorageGateway)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "organizations/acme/storageGateways/gw-1", inner.GetName())
	assert.Equal(t, "Gateway One", inner.GetDisplayName())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetStorageGateway
// ---------------------------------------------------------------------------

func TestUnit_GetStorageGateway_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)

	resp, err := srv.GetStorageGateway(ctx, &storagev1.GetStorageGatewayRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/storageGateways/gw-1", resp.GetName())
	assert.Equal(t, "Gateway One", resp.GetDisplayName())
	assert.Equal(t, "reg-token-abc", resp.GetRegistrationToken())
	assert.Equal(t, "gw-1.storage.pivox.app", resp.GetHostname())
	assert.Equal(t, map[string]string{"env": "prod"}, resp.GetAnnotations())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetStorageGateway_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.GetStorageGateway(ctx, &storagev1.GetStorageGatewayRequest{
		Name: "organizations/acme/storageGateways/missing",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// DeleteStorageGateway
// ---------------------------------------------------------------------------

func TestUnit_DeleteStorageGateway_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("DeleteStorageGateway", mock.Anything, gwID).Return(nil)

	resp, err := srv.DeleteStorageGateway(ctx, &storagev1.DeleteStorageGatewayRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// RotateRegistrationToken
// ---------------------------------------------------------------------------

func TestUnit_RotateRegistrationToken_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	rotatedGW := testGateway
	rotatedGW.RegistrationToken = "new-token-xyz"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("RotateRegistrationToken", mock.Anything, mock.MatchedBy(func(p db.RotateRegistrationTokenParams) bool {
		return p.ID == gwID && p.RegistrationToken != ""
	})).Return(rotatedGW, nil)

	resp, err := srv.RotateRegistrationToken(ctx, &storagev1.RotateRegistrationTokenRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/storageGateways/gw-1", resp.GetName())
	assert.Equal(t, "new-token-xyz", resp.GetRegistrationToken())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetInstallScript
// ---------------------------------------------------------------------------

func TestUnit_GetInstallScript_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)

	resp, err := srv.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	assert.Contains(t, resp.GetScript(), "curl -sSL https://get.pivox.app/agent | bash -s --")
	assert.Contains(t, resp.GetScript(), "--token reg-token-abc")
	mockQ.AssertExpectations(t)
}

func TestUnit_GetInstallScript_WithFlags(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)

	resp, err := srv.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
		Name:        "organizations/acme/storageGateways/gw-1",
		CacheDir:    "/mnt/cache",
		CacheSizeGb: 100,
		Port:        9090,
		BindAddress: "0.0.0.0",
		Telemetry:   true,
	})

	require.NoError(t, err)
	script := resp.GetScript()
	assert.Contains(t, script, "--cache-dir /mnt/cache")
	assert.Contains(t, script, "--cache-size-gb 100")
	assert.Contains(t, script, "--port 9090")
	assert.Contains(t, script, "--bind-address 0.0.0.0")
	assert.Contains(t, script, "--telemetry")
	mockQ.AssertExpectations(t)
}
