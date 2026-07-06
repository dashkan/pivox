package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/agentstream"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/service/storage"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
)

// TestIntegration_Storage_ValidateOnly pins the AIP validate_only contract
// for both the StorageGateways and Endpoints create paths: a dry-run runs
// the same validation a live request would but persists nothing, so the
// would-be resource is not gettable and its id is reusable.
func TestIntegration_Storage_ValidateOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	conns := agentstream.NewConnectionManager()
	h := grpcharness.New(t,
		grpcharness.WithOrganizationsServer(),
		grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
			storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
				Pool: h.Pool, Queries: h.Queries, Encryptor: h.Encryptor, Conns: conns,
			}))
			storagev1.RegisterEndpointsServer(s, storage.NewEndpointsServer(storage.EndpointsConfig{
				Pool: h.Pool, Queries: h.Queries, Encryptor: h.Encryptor,
			}))
		}))
	h.SeedOwnedOrg(t, "acme-vo", "Acme VO", "storage")

	gwClient := storagev1.NewStorageGatewaysClient(h.Conn())
	epClient := storagev1.NewEndpointsClient(h.Conn())
	ctx := context.Background()

	// Dry-run gateway create: returns the would-be resource, persists nothing.
	dryOp, err := gwClient.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent:           "organizations/acme-vo",
		StorageGatewayId: "gw-vo",
		StorageGateway:   &storagev1.StorageGateway{DisplayName: "Dry", IpAddresses: []string{"10.0.0.1"}},
		ValidateOnly:     true,
	})
	require.NoError(t, err)
	require.True(t, dryOp.GetDone())

	// Not persisted → the would-be gateway is not gettable.
	_, err = gwClient.GetStorageGateway(ctx, &storagev1.GetStorageGatewayRequest{
		Name: "organizations/acme-vo/storageGateways/gw-vo",
	})
	require.Error(t, err, "validate_only must not have persisted the gateway")
	assert.Equal(t, codes.NotFound, status.Code(err))

	// A real create can reuse the same id.
	realOp, err := gwClient.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent:           "organizations/acme-vo",
		StorageGatewayId: "gw-vo",
		StorageGateway:   &storagev1.StorageGateway{DisplayName: "Real", IpAddresses: []string{"10.0.0.1"}},
	})
	require.NoError(t, err)
	var gw storagev1.StorageGateway
	require.NoError(t, realOp.GetResponse().UnmarshalTo(&gw))
	gatewayName := gw.GetName()

	// A dry-run that WOULD fail live (duplicate gateway id now exists) fails.
	_, err = gwClient.CreateStorageGateway(ctx, &storagev1.CreateStorageGatewayRequest{
		Parent:           "organizations/acme-vo",
		StorageGatewayId: "gw-vo",
		StorageGateway:   &storagev1.StorageGateway{DisplayName: "Dup", IpAddresses: []string{"10.0.0.1"}},
		ValidateOnly:     true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err),
		"validate_only must fail if the live gateway create would")

	// Dry-run endpoint create under the real gateway: persists nothing.
	dryEp, err := epClient.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:     gatewayName,
		EndpointId: "ep-vo",
		Endpoint: &storagev1.Endpoint{
			DisplayName: "Dry EP",
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfig{Path: "/mnt/storage"},
			},
		},
		ValidateOnly: true,
	})
	require.NoError(t, err)
	require.True(t, dryEp.GetDone())

	list, err := epClient.ListEndpoints(ctx, &storagev1.ListEndpointsRequest{Parent: gatewayName})
	require.NoError(t, err)
	assert.Empty(t, list.GetEndpoints(), "validate_only endpoint create must persist nothing")

	// A real endpoint create can reuse the same id.
	_, err = epClient.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:     gatewayName,
		EndpointId: "ep-vo",
		Endpoint: &storagev1.Endpoint{
			DisplayName: "Real EP",
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfig{Path: "/mnt/storage"},
			},
		},
	})
	require.NoError(t, err, "validate_only must not have persisted the endpoint")

	// A dry-run that WOULD fail live (duplicate endpoint id now exists) fails.
	_, err = epClient.CreateEndpoint(ctx, &storagev1.CreateEndpointRequest{
		Parent:     gatewayName,
		EndpointId: "ep-vo",
		Endpoint: &storagev1.Endpoint{
			DisplayName: "Dup EP",
			Configuration: &storagev1.Endpoint_Filesystem{
				Filesystem: &storagev1.FileSystemConfig{Path: "/mnt/storage"},
			},
		},
		ValidateOnly: true,
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err),
		"validate_only must fail if the live endpoint create would")
}
