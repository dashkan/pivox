package fixtures

import (
	"github.com/google/uuid"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// DefaultStorageGatewayID is the canonical UUID for the default test
// storage gateway. Stable across calls.
var DefaultStorageGatewayID = uuid.MustParse("00000000-0000-7000-8000-000000000003")

// StorageGatewayOpt mutates a db.StorageGateway.
type StorageGatewayOpt func(*db.StorageGateway)

// StorageGateway returns a default-populated db.StorageGateway: an
// ACTIVE gateway named "gw-1" under the default org. Override with
// options.
func StorageGateway(opts ...StorageGatewayOpt) db.StorageGateway {
	g := db.StorageGateway{
		ID:                DefaultStorageGatewayID,
		OrgID:             DefaultOrgID,
		Name:              "gw-1",
		DisplayName:       "Gateway One",
		State:             db.StorageGatewayStateACTIVE,
		RegistrationToken: "reg-token-default",
		Hostname:          "gw-1.storage.pivox.app",
		Etag:              "etag-default",
		CreateTime:        DefaultTime,
		UpdateTime:        DefaultTime,
	}
	for _, opt := range opts {
		opt(&g)
	}
	return g
}

// GatewayID overrides the gateway's UUID.
func GatewayID(id uuid.UUID) StorageGatewayOpt {
	return func(g *db.StorageGateway) { g.ID = id }
}

// GatewayOrgID sets the parent org id.
func GatewayOrgID(id uuid.UUID) StorageGatewayOpt {
	return func(g *db.StorageGateway) { g.OrgID = id }
}

// GatewayName overrides the slug name.
func GatewayName(name string) StorageGatewayOpt {
	return func(g *db.StorageGateway) { g.Name = name }
}

// GatewayState sets the gateway's lifecycle state.
func GatewayState(s db.StorageGatewayState) StorageGatewayOpt {
	return func(g *db.StorageGateway) { g.State = s }
}
