package storage

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

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

// ---------------------------------------------------------------------------
// UpdateStorageGateway
// ---------------------------------------------------------------------------

func TestUnit_UpdateStorageGateway_WithFieldMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	updatedGW := testGateway
	updatedGW.DisplayName = "Updated Gateway"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("UpdateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageGatewayParams) bool {
		return p.ID == gwID &&
			p.DisplayName == pgtype.Text{String: "Updated Gateway", Valid: true} &&
			p.IpAddresses == nil && // not in mask, should be zero value
			p.TargetVersion == pgtype.Text{} // not in mask, should be zero value
	})).Return(updatedGW, nil)

	resp, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name:        "organizations/acme/storageGateways/gw-1",
			DisplayName: "Updated Gateway",
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"display_name"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(storagev1.StorageGateway)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "organizations/acme/storageGateways/gw-1", inner.GetName())
	assert.Equal(t, "Updated Gateway", inner.GetDisplayName())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateStorageGateway_NoMask(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	updatedGW := testGateway
	updatedGW.DisplayName = "Full Update"
	updatedGW.IpAddresses = []string{"10.0.0.2", "10.0.0.3"}
	updatedGW.TargetVersion = "2.0.0"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("UpdateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageGatewayParams) bool {
		return p.ID == gwID &&
			p.DisplayName == pgtype.Text{String: "Full Update", Valid: true} &&
			len(p.IpAddresses) == 2 &&
			p.TargetVersion == pgtype.Text{String: "2.0.0", Valid: true}
	})).Return(updatedGW, nil)

	resp, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name:          "organizations/acme/storageGateways/gw-1",
			DisplayName:   "Full Update",
			IpAddresses:   []string{"10.0.0.2", "10.0.0.3"},
			TargetVersion: "2.0.0",
		},
		// No UpdateMask — all mutable fields updated.
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	inner := new(storagev1.StorageGateway)
	require.NoError(t, anypb.UnmarshalTo(resp.GetResponse(), inner, proto.UnmarshalOptions{}))
	assert.Equal(t, "Full Update", inner.GetDisplayName())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetUninstallScript
// ---------------------------------------------------------------------------

func TestUnit_GetUninstallScript_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)

	resp, err := srv.GetUninstallScript(ctx, &storagev1.GetUninstallScriptRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	assert.Equal(t, "curl -sSL https://get.pivox.app/agent/uninstall | bash", resp.GetScript())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// ListStorageGateways
// ---------------------------------------------------------------------------

func TestUnit_ListStorageGateways_Unimplemented(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)

	_, err := srv.ListStorageGateways(context.Background(), &storagev1.ListStorageGatewaysRequest{})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "ListStorageGateways not yet implemented")
}

// ---------------------------------------------------------------------------
// UpgradeGateway
// ---------------------------------------------------------------------------

func TestUnit_UpgradeGateway_Unimplemented(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)

	_, err := srv.UpgradeGateway(context.Background(), &storagev1.UpgradeGatewayRequest{})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
	assert.Contains(t, st.Message(), "UpgradeGateway not yet implemented")
}

// ---------------------------------------------------------------------------
// parseStorageGatewayName — error path
// ---------------------------------------------------------------------------

func TestUnit_ParseStorageGatewayName_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"too_few_parts", "organizations/acme"},
		{"wrong_resource", "organizations/acme/gateways/gw-1"},
		{"too_many_parts", "organizations/acme/storageGateways/gw-1/extra/more"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseStorageGatewayName(tc.input)
			require.Error(t, err)
		})
	}
}

// ---------------------------------------------------------------------------
// GetStorageGateway — org not found
// ---------------------------------------------------------------------------

func TestUnit_GetStorageGateway_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "missing-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetStorageGateway(ctx, &storagev1.GetStorageGatewayRequest{
		Name: "organizations/missing-org/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetStorageGateway — invalid name
// ---------------------------------------------------------------------------

func TestUnit_GetStorageGateway_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	_, err := srv.GetStorageGateway(ctx, &storagev1.GetStorageGatewayRequest{
		Name: "bad-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

// ---------------------------------------------------------------------------
// UpdateStorageGateway — error paths
// ---------------------------------------------------------------------------

func TestUnit_UpdateStorageGateway_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	_, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name: "bad/name",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUnit_UpdateStorageGateway_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name: "organizations/no-org/storageGateways/gw-1",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateStorageGateway_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name: "organizations/acme/storageGateways/missing",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateStorageGateway_UpdateFails(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("UpdateStorageGateway", mock.Anything, mock.Anything).
		Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name:        "organizations/acme/storageGateways/gw-1",
			DisplayName: "Updated",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateStorageGateway_FieldMask_AllPaths(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	updatedGW := testGateway
	updatedGW.IpAddresses = []string{"10.0.0.5"}
	updatedGW.TargetVersion = "2.0.0"

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("UpdateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageGatewayParams) bool {
		return p.ID == gwID
	})).Return(updatedGW, nil)

	resp, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name:          "organizations/acme/storageGateways/gw-1",
			IpAddresses:   []string{"10.0.0.5"},
			TargetVersion: "2.0.0",
			Annotations:   map[string]string{"k": "v"},
		},
		UpdateMask: &fieldmaskpb.FieldMask{
			Paths: []string{"ip_addresses", "target_version", "annotations"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_UpdateStorageGateway_NoMask_WithAnnotations(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	updatedGW := testGateway

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("UpdateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.UpdateStorageGatewayParams) bool {
		return p.ID == gwID
	})).Return(updatedGW, nil)

	resp, err := srv.UpdateStorageGateway(ctx, &storagev1.UpdateStorageGatewayRequest{
		StorageGateway: &storagev1.StorageGateway{
			Name:        "organizations/acme/storageGateways/gw-1",
			DisplayName: "Updated",
			Annotations: map[string]string{"env": "staging"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// DeleteStorageGateway — error paths
// ---------------------------------------------------------------------------

func TestUnit_DeleteStorageGateway_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	_, err := srv.DeleteStorageGateway(ctx, &storagev1.DeleteStorageGatewayRequest{
		Name: "not/a/valid/name/at/all/extra",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUnit_DeleteStorageGateway_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.DeleteStorageGateway(ctx, &storagev1.DeleteStorageGatewayRequest{
		Name: "organizations/no-org/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteStorageGateway_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.DeleteStorageGateway(ctx, &storagev1.DeleteStorageGatewayRequest{
		Name: "organizations/acme/storageGateways/missing",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_DeleteStorageGateway_DeleteFails(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("DeleteStorageGateway", mock.Anything, gwID).
		Return(pgx.ErrNoRows)

	_, err := srv.DeleteStorageGateway(ctx, &storagev1.DeleteStorageGatewayRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// RotateRegistrationToken — error paths
// ---------------------------------------------------------------------------

func TestUnit_RotateRegistrationToken_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	_, err := srv.RotateRegistrationToken(ctx, &storagev1.RotateRegistrationTokenRequest{
		Name: "bad-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUnit_RotateRegistrationToken_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.RotateRegistrationToken(ctx, &storagev1.RotateRegistrationTokenRequest{
		Name: "organizations/no-org/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_RotateRegistrationToken_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.RotateRegistrationToken(ctx, &storagev1.RotateRegistrationTokenRequest{
		Name: "organizations/acme/storageGateways/missing",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_RotateRegistrationToken_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("RotateRegistrationToken", mock.Anything, mock.Anything).
		Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.RotateRegistrationToken(ctx, &storagev1.RotateRegistrationTokenRequest{
		Name: "organizations/acme/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetUninstallScript — error paths
// ---------------------------------------------------------------------------

func TestUnit_GetUninstallScript_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	_, err := srv.GetUninstallScript(ctx, &storagev1.GetUninstallScriptRequest{
		Name: "bad-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUnit_GetUninstallScript_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetUninstallScript(ctx, &storagev1.GetUninstallScriptRequest{
		Name: "organizations/no-org/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetUninstallScript_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.GetUninstallScript(ctx, &storagev1.GetUninstallScriptRequest{
		Name: "organizations/acme/storageGateways/missing",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// GetInstallScript — error paths
// ---------------------------------------------------------------------------

func TestUnit_GetInstallScript_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	_, err := srv.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
		Name: "bad-name",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
}

func TestUnit_GetInstallScript_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
		Name: "organizations/no-org/storageGateways/gw-1",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetInstallScript_GatewayNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "missing",
	}).Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
		Name: "organizations/acme/storageGateways/missing",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetInstallScript_WithProxyFlags(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)

	resp, err := srv.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
		Name:       "organizations/acme/storageGateways/gw-1",
		HttpProxy:  "http://proxy:3128",
		HttpsProxy: "https://proxy:3128",
		NoProxy:    "localhost,127.0.0.1",
	})

	require.NoError(t, err)
	script := resp.GetScript()
	assert.Contains(t, script, "--http-proxy http://proxy:3128")
	assert.Contains(t, script, "--https-proxy https://proxy:3128")
	assert.Contains(t, script, "--no-proxy localhost,127.0.0.1")
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// CreateStorageGateway — error paths
// ---------------------------------------------------------------------------

func TestUnit_CreateStorageGateway_OrgNotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "no-org").
		Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent: "organizations/no-org",
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: "GW",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateStorageGateway_DBError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("CreateStorageGateway", mock.Anything, mock.Anything).
		Return(db.StorageGateway{}, pgx.ErrNoRows)

	_, err := srv.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent: "organizations/acme",
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: "GW",
		},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateStorageGateway_AutoGeneratedID(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("CreateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.CreateStorageGatewayParams) bool {
		// No StorageGatewayId provided — auto-generated UUID prefix, 8 chars long
		return p.OrgID == gwOrgID && len(p.Name) == 8
	})).Return(testGateway, nil)

	resp, err := srv.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent: "organizations/acme",
		// No StorageGatewayId — auto-generated
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: "Auto GW",
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateStorageGateway_WithAnnotations(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newGatewayServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("CreateStorageGateway", mock.Anything, mock.MatchedBy(func(p db.CreateStorageGatewayParams) bool {
		return p.OrgID == gwOrgID && p.Name == "gw-anno"
	})).Return(testGateway, nil)

	resp, err := srv.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent:           "organizations/acme",
		StorageGatewayId: "gw-anno",
		StorageGateway: &storagev1.StorageGateway{
			DisplayName: "Annotated GW",
			Annotations: map[string]string{"env": "prod"},
		},
	})

	require.NoError(t, err)
	assert.True(t, resp.GetDone())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// CreateStorageSession
// ---------------------------------------------------------------------------

func TestUnit_CreateStorageSession_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := &StorageGatewaysServer{
		queries:           mockQ,
		encryptor:         nil,
		conns:             conns,
		sessionSigningKey: []byte("test-key"),
	}
	ctx := context.Background()

	resp, err := srv.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	// Verify expiry is set and roughly 1 hour from now (default TTL).
	require.NotNil(t, resp.GetExpiry())
	expiry := resp.GetExpiry().AsTime()
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiry, 5*time.Second)
}

func TestUnit_CreateStorageSession_WithCustomTTL(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	conns := agentstream.NewConnectionManager()
	srv := &StorageGatewaysServer{
		queries:           mockQ,
		encryptor:         nil,
		conns:             conns,
		sessionSigningKey: []byte("test-key"),
	}
	ctx := context.Background()

	customTTL := 30 * time.Minute
	resp, err := srv.CreateStorageSession(ctx, &storagev1.CreateStorageSessionRequest{
		Ttl: durationpb.New(customTTL),
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.GetExpiry())
	expiry := resp.GetExpiry().AsTime()
	assert.WithinDuration(t, time.Now().Add(customTTL), expiry, 5*time.Second)
}
