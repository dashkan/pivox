package grpcharness

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/service/storage"
)

// TestAppCodec returns the shared hard-coded test app-key codec used to
// opaque-encode page tokens in harness-backed tests. The key is a fixed
// 32-byte hex constant — deterministic across runs so a token minted in one
// call decodes in the next. Panics only if the constant is malformed, which
// is hand-checked and never fires at runtime.
func TestAppCodec() *appkey.Codec {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	return codec
}

// WithStorageGatewaysServer registers the StorageGateways service with default
// wiring (harness Pool/Queries/Encryptor, the shared test codec, and a
// dedicated ConnectionManager). For tests exercising session/TTL/cookie config
// that need a customized StorageGatewaysConfig, register inline via WithServices
// instead.
func WithStorageGatewaysServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, func(h *Harness, s *grpc.Server) {
			storagev1.RegisterStorageGatewaysServer(s, storage.NewStorageGatewaysServer(storage.StorageGatewaysConfig{
				Pool:      h.Pool,
				Queries:   h.Queries,
				Codec:     TestAppCodec(),
				Encryptor: h.Encryptor,
				Conns:     agentstream.NewConnectionManager(),
			}))
		})
	}
}

// WithAgentsServer registers the Agents service on the harness's gRPC server.
func WithAgentsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, func(h *Harness, s *grpc.Server) {
			storagev1.RegisterAgentsServer(s, storage.NewAgentsServer(storage.AgentsConfig{Queries: h.Queries}))
		})
	}
}

// SeedStorageGateway inserts a storage gateway directly (bypassing the
// CreateStorageGateway LRO) and returns the row. The slug `name` is unique
// within `orgID`. created_by is left unset (the column is nullable), so no
// identity row is required. Use this to stage gateways for list/pagination and
// cross-org isolation tests.
func (h *Harness) SeedStorageGateway(t *testing.T, orgID uuid.UUID, name string) db.StorageGateway {
	t.Helper()
	gw, err := h.Queries.CreateStorageGateway(context.Background(), db.CreateStorageGatewayParams{
		ID:                uuid.New(),
		OrgID:             orgID,
		Name:              name,
		DisplayName:       name,
		IpAddresses:       []string{"10.0.0.1"},
		RegistrationToken: uuid.New().String(),
		Hostname:          name + ".storage.pivox.app",
		Annotations:       json.RawMessage("{}"),
	})
	require.NoError(t, err)
	return gw
}

// SeedStorageAgent inserts an agent under `gatewayID` directly. Agents cannot
// be created through the API (they self-register via bidi streaming), so tests
// stage them via this helper. `ipAddress` must be unique within the gateway
// (a unique index covers (gateway_id, ip_address)).
func (h *Harness) SeedStorageAgent(t *testing.T, gatewayID uuid.UUID, hostname, ipAddress string) db.StorageAgent {
	t.Helper()
	agent, err := h.Queries.CreateStorageAgent(context.Background(), db.CreateStorageAgentParams{
		ID:        uuid.New(),
		GatewayID: gatewayID,
		IpAddress: ipAddress,
		Hostname:  hostname,
		Version:   "1.0.0",
	})
	require.NoError(t, err)
	return agent
}
