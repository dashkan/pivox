//go:build dev

package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/agentstream"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/service/storage"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

func TestIntegration_Storage_GatewayLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	conns := agentstream.NewConnectionManager()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
				Queries: h.Queries, Encryptor: h.Encryptor, Conns: conns,
			}))
			storagev1.RegisterEndpointsServer(s, storage.NewEndpointsServer(storage.EndpointsConfig{
				Queries: h.Queries, Encryptor: h.Encryptor,
			}))
		}))
	h.SeedOwnedOrg(t, "acme", "Acme", "storage")

	gwClient := storagev1.NewStorageGatewaysClient(h.Conn())
	epClient := storagev1.NewEndpointsClient(h.Conn())
	ctx := context.Background()

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
		// port + bind_address are marked OPTIONAL in the proto but
		// have validation rules (gte:1, string.ip) that reject the
		// proto3 default-zero values — see storage_gateway.proto.
		// Pass valid values explicitly so the request clears
		// protovalidate. (Underlying proto inconsistency tracked
		// separately; test migration shouldn't fix unrelated proto
		// rules.)
		resp, err := gwClient.GetInstallScript(ctx, &storagev1.GetInstallScriptRequest{
			Name:        gatewayName,
			CacheSizeGb: 100,
			Port:        8080,
			BindAddress: "0.0.0.0",
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
