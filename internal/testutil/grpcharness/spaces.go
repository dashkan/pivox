package grpcharness

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/service/spaces"
)

// WithSpacesServer registers the SpacesServer on the harness's gRPC
// server using sane defaults. Pair with WithOrganizationsServer when
// the test seeds an org first; SeedOwnedSpace then composes the two
// for the common "owned org + owned space" setup.
func WithSpacesServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerSpacesServer)
	}
}

func registerSpacesServer(h *Harness, s *grpc.Server) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	apiv1.RegisterSpacesServer(s, spaces.NewSpacesServer(spaces.Config{
		Pool:       h.Pool,
		Queries:    h.Queries,
		Codec:      codec,
		Resolver:   permission.NewResolver(h.Queries),
		LROManager: h.LROManager,
	}))
}

// OwnedSpace bundles the artifacts produced by SeedOwnedSpace.
type OwnedSpace struct {
	OrgSlug string
	Slug    string
	Row     db.Space
}

// SeedOwnedSpace creates a space inside an existing org through the
// real CreateSpace handler. The current harness caller (whoever was
// last passed to SetCaller, typically set up by SeedOwnedOrg) must
// have permission to create spaces in the org — which the founding
// owner does by default.
//
// Requires WithSpacesServer to have been passed to grpcharness.New.
func (h *Harness) SeedOwnedSpace(t *testing.T, orgSlug, spaceSlug, displayName string) OwnedSpace {
	t.Helper()
	parent := "organizations/" + orgSlug

	client := apiv1.NewSpacesClient(h.Conn())
	op, err := client.CreateSpace(context.Background(), &apiv1.CreateSpaceRequest{
		Parent:  parent,
		SpaceId: spaceSlug,
		Space:   &apiv1.Space{DisplayName: displayName},
	})
	require.NoError(t, err, "CreateSpace(%s/spaces/%s) failed — did you pass WithSpacesServer()?", parent, spaceSlug)
	require.True(t, op.GetDone(), "CreateSpace is sync; expected done=true")

	row, err := h.Queries.GetSpaceByName(context.Background(), db.GetSpaceByNameParams{
		OrgID: h.LookupOrgID(t, orgSlug),
		Name:  spaceSlug,
	})
	require.NoError(t, err)
	return OwnedSpace{OrgSlug: orgSlug, Slug: spaceSlug, Row: row}
}
