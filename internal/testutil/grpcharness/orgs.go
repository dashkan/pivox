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
	"github.com/dashkan/pivox/internal/service/organizations"
)

// WithOrganizationsServer registers the OrganizationsServer on the
// harness's gRPC server using sane defaults pulled from the
// harness's Pool/Queries/Auth/Codec/Caller. Composes with
// WithServices — a test that needs to seed an org and exercise its
// own system-under-test passes both:
//
//	h := grpcharness.New(t,
//	    grpcharness.WithOrganizationsServer(),
//	    grpcharness.WithServices(func(h, s) { /* register SUT */ }))
//	owner, _ := h.SeedOwnedOrg(t, "acme", "Acme")
//
// This is the canonical way to set up an org-scoped integration
// test. Going through CreateOrganization (vs. raw SQL) keeps test
// setup honest about the real handler's invariants — founder
// member binding, system roles, etag/revision seeding all happen
// the same way they do in production.
func WithOrganizationsServer() Option {
	return func(c *config) {
		c.registerServices = append(c.registerServices, registerOrganizationsServer)
	}
}

func registerOrganizationsServer(h *Harness, s *grpc.Server) {
	codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
	if err != nil {
		// AppKey parse only fails on malformed hex; the constant
		// above is hand-checked, so this never fires at runtime.
		// Panic so test failures surface here, not in a deeper
		// constructor down the chain.
		panic("grpcharness: hard-coded test app key is malformed: " + err.Error())
	}
	apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
		Pool:       h.Pool,
		Queries:    h.Queries,
		Codec:      codec,
		Resolver:   permission.NewResolver(h.Queries),
		LROManager: h.LROManager,
	}))
}

// OwnedOrg bundles the artifacts produced by SeedOwnedOrg: the
// founding owner identity (already wired as the harness's current
// caller), the org's stable slug, and the row from the DB. Tests
// pull whichever piece they need.
type OwnedOrg struct {
	Owner *Caller
	Slug  string
	Row   db.Organization
}

// SeedOwnedOrg seeds an identity, sets it as the harness caller,
// and creates an organization owned by that identity through the
// real CreateOrganization handler. Returns the artifacts the
// caller most often needs (the owner Caller, the slug, the row).
//
// Requires WithOrganizationsServer to have been passed to
// grpcharness.New — otherwise CreateOrganization isn't registered
// and the call fails with Unimplemented.
//
// uidPrefix lets parallel tests in the same package avoid identity
// collisions — pass something deterministic per test (the package
// name, or t.Name()). Empty string falls back to "owner".
func (h *Harness) SeedOwnedOrg(t *testing.T, slug, displayName, uidPrefix string) OwnedOrg {
	t.Helper()
	if uidPrefix == "" {
		uidPrefix = "owner"
	}
	owner := h.SeedIdentity(t, SeedIdentityOpts{UID: uidPrefix + "-" + slug})
	h.SetCaller(owner)

	client := apiv1.NewOrganizationsClient(h.Conn())
	op, err := client.CreateOrganization(context.Background(), &apiv1.CreateOrganizationRequest{
		OrganizationId: slug,
		Organization:   &apiv1.Organization{DisplayName: displayName},
	})
	require.NoError(t, err, "CreateOrganization(%s) failed — did you pass WithOrganizationsServer()?", slug)
	require.True(t, op.GetDone(), "CreateOrganization is sync; expected done=true")

	row, err := h.Queries.GetOrganizationByName(context.Background(), slug)
	require.NoError(t, err)
	return OwnedOrg{Owner: owner, Slug: slug, Row: row}
}
