//go:build dev

package storage_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/service/storage"
	"github.com/dashkan/pivox/internal/testutil"
)

func createStorageTestOrg(t *testing.T, queries *db.Queries, name string) db.Organization {
	t.Helper()
	org, err := queries.CreateOrganization(context.Background(), db.CreateOrganizationParams{
		ID:          uuid.New(),
		Name:        name,
		DisplayName: "Test Org " + name,
		CreatedBy:   pgtype.UUID{},
	})
	require.NoError(t, err)
	return org
}

func TestIntegration_Storage_GatewayLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	_, queries, cleanup := testutil.SetupTestDB(t)
	defer cleanup()

	enc, err := crypto.NewEncryptor()
	require.NoError(t, err)
	conns := agentstream.NewConnectionManager()

	conn := testutil.SetupGRPCServer(t, func(s *grpc.Server) {
		storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
			Queries: queries, Encryptor: enc, Conns: conns,
		}))
		storagev1.RegisterEndpointsServer(s, storage.NewEndpointsServer(storage.EndpointsConfig{
			Queries: queries, Encryptor: enc,
		}))
	})

	gwClient := storagev1.NewStorageGatewaysClient(conn)
	epClient := storagev1.NewEndpointsClient(conn)
	ctx := context.Background()

	// Prerequisite: create org.
	createStorageTestOrg(t, queries, "acme")

	var gatewayName string
	var endpointName string

	t.Run("CreateStorageGateway", func(t *testing.T) {
		op, err := gwClient.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
			Parent:           "organizations/acme",
			StorageGatewayId: "gw1",
			StorageGateway: &storagev1.StorageGateway{
				DisplayName: "Gateway One",
				IpAddresses: []string{"10.0.0.1"},
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var gw storagev1.StorageGateway
		require.NoError(t, op.GetResponse().UnmarshalTo(&gw))
		assert.Equal(t, "organizations/acme/storageGateways/gw1", gw.GetName())
		assert.Equal(t, "Gateway One", gw.GetDisplayName())
		assert.NotEmpty(t, gw.GetRegistrationToken())
		assert.Equal(t, "gw1.storage.pivox.app", gw.GetHostname())
		gatewayName = gw.GetName()
	})

	t.Run("GetStorageGateway", func(t *testing.T) {
		resp, err := gwClient.GetStorageGateway(ctx, &storagev1.GetStorageGatewayRequest{
			Name: gatewayName,
		})
		require.NoError(t, err)
		assert.Equal(t, gatewayName, resp.GetName())
		assert.Equal(t, "Gateway One", resp.GetDisplayName())
	})

	t.Run("GetInstallScript", func(t *testing.T) {
		resp, err := gwClient.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
			Name:        gatewayName,
			CacheSizeGb: 100,
		})
		require.NoError(t, err)
		assert.Contains(t, resp.GetScript(), "curl -sSL https://get.pivox.app/agent")
		assert.Contains(t, resp.GetScript(), "--token")
		assert.Contains(t, resp.GetScript(), "--cache-size-gb 100")
	})

	t.Run("CreateEndpoint_Filesystem", func(t *testing.T) {
		op, err := epClient.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
			Parent:     gatewayName,
			EndpointId: "ep-fs",
			Endpoint: &storagev1.Endpoint{
				DisplayName: "Local Filesystem",
				Configuration: &storagev1.Endpoint_Filesystem{
					Filesystem: &storagev1.FileSystemConfiguration{
						Path: "/mnt/storage",
					},
				},
			},
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())

		var ep storagev1.Endpoint
		require.NoError(t, op.GetResponse().UnmarshalTo(&ep))
		assert.Contains(t, ep.GetName(), "endpoints/ep-fs")
		assert.Equal(t, "Local Filesystem", ep.GetDisplayName())
		endpointName = ep.GetName()
	})

	t.Run("GetEndpoint", func(t *testing.T) {
		resp, err := epClient.GetEndpoint(ctx, &storagev1.GetEndpointRequest{
			Name: endpointName,
		})
		require.NoError(t, err)
		assert.Equal(t, endpointName, resp.GetName())
	})

	t.Run("ListEndpoints", func(t *testing.T) {
		resp, err := epClient.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{
			Parent: gatewayName,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(resp.GetEndpoints()), 1)
	})

	t.Run("DeleteEndpoint", func(t *testing.T) {
		op, err := epClient.DeleteEndpoint(ctx, &storagev1.DeleteEndpointRequest{
			Name: endpointName,
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())
	})

	t.Run("DeleteStorageGateway", func(t *testing.T) {
		op, err := gwClient.DeleteStorageGateway(ctx, &storagev1.DeleteStorageGatewayRequest{
			Name: gatewayName,
		})
		require.NoError(t, err)
		assert.True(t, op.GetDone())
	})
}
