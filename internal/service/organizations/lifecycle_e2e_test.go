//go:build dev

package organizations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/permission"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
	"github.com/dashkan/pivox/internal/workers"
)

// TestE2E_OrgSoftDeleteRevive exercises the full DeleteOrganization
// (soft path) → UndeleteOrganization round-trip end-to-end through
// the production interceptor chain.
//
// Cycle:
//
//  1. Owner creates an org. State=ACTIVE.
//  2. Owner soft-deletes it. State=DELETE_REQUESTED, delete_time +
//     purge_time set, etag bumped.
//  3. Reads still work during the grace window.
//  4. A second DeleteOrganization on the DELETE_REQUESTED org
//     surfaces FailedPrecondition (the handler's state guard,
//     distinct from the soft-delete-gate's permission rejection
//     that's covered in A4). Confirms the row's state pin is honored.
//  5. Owner undeletes. State=ACTIVE, delete_time/purge_time cleared,
//     etag bumped again.
//  6. A second UndeleteOrganization on the now-ACTIVE org surfaces
//     FailedPrecondition — confirms the inverse state guard.
//
// The fine-grained "soft-delete gate allow/deny matrix" check (e.g.
// members.create rejected on DELETE_REQUESTED) lives in A4's
// permission-interceptor E2E test, not here.
func TestE2E_OrgSoftDeleteRevive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Step 1: create the org as the founding owner.
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	createOp, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "revive-me",
		Organization:   &apiv1.Organization{DisplayName: "Revive Me"},
	})
	require.NoError(t, err)
	require.True(t, createOp.GetDone())

	var created apiv1.Organization
	require.NoError(t, createOp.GetResponse().UnmarshalTo(&created))
	require.Equal(t, apiv1.Organization_ACTIVE, created.GetState())
	originalEtag := created.GetEtag()

	// Step 2: soft-delete (force=false, no etag — non-force is
	// allowed without etag pinning per handler contract).
	deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization")

	// Step 3: reads still work during grace.
	got, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.NoError(t, err, "reads remain allowed during grace window")
	assert.Equal(t, apiv1.Organization_DELETE_REQUESTED, got.GetState())
	assert.NotEmpty(t, got.GetDeleteTime())
	assert.NotEmpty(t, got.GetPurgeTime())
	require.NotEqual(t, originalEtag, got.GetEtag(),
		"soft-delete must bump etag")

	// Step 4: redundant DeleteOrganization on DELETE_REQUESTED row
	// rejected by the handler's state guard.
	_, err = client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.Error(t, err, "second soft-delete on DELETE_REQUESTED row must fail")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))

	// Step 5: undelete with the correct etag.
	undeleteOp, err := client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/revive-me",
		Etag: got.GetEtag(),
	})
	require.NoError(t, err)
	waitOp(t, h, undeleteOp, "UndeleteOrganization")

	revived, err := client.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.NoError(t, err)
	assert.Equal(t, apiv1.Organization_ACTIVE, revived.GetState())
	assert.Empty(t, revived.GetDeleteTime(), "revived org must clear delete_time")
	assert.Empty(t, revived.GetPurgeTime(), "revived org must clear purge_time")
	assert.NotEqual(t, got.GetEtag(), revived.GetEtag(),
		"undelete must bump etag")

	// Step 6: redundant UndeleteOrganization on now-ACTIVE row
	// rejected by the inverse state guard.
	_, err = client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/revive-me",
	})
	require.Error(t, err, "undelete on ACTIVE row must fail")
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// newLifecycleHarness wires up a harness with the Organizations
// service + LRO manager + an in-process River client running every
// org-lifecycle worker that's been ported off the legacy goroutine
// path (#69 Phase 5+). Lifecycle tests reuse this across all e2e
// cases in this file. Wiring matches cmd/pivox-cloud/main.go's
// production OrganizationsServer construction — anything passed as
// nil here means the test doesn't exercise that path (e.g.,
// resolver/codec are unused without space-scoped or appkey paths).
func newLifecycleHarness(t *testing.T) *grpcharness.Harness {
	h := grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		permResolver := permission.NewResolver(h.Queries)
		codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
		require.NoError(t, err)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Auth:       h.Auth,
			Codec:      codec,
			ReadUID:    server.AuthenticatedUID,
			Resolver:   permResolver,
			Caller:     callerIdentity,
			LROManager: h.LROManager,
			Encryptor:  h.Encryptor,
		}))
	}))
	startOrgLifecycleWorkers(t, h)
	return h
}

// startOrgLifecycleWorkers spins up an in-process River client with
// the post-Phase-5 org-lifecycle workers registered. Mirrors
// cmd/pivox-worker/main.go's wiring (same Schema, same driver),
// scoped to the test. Add new workers here as more LROs port off
// the legacy goroutine path (#69 Phase 6).
func startOrgLifecycleWorkers(t *testing.T, h *grpcharness.Harness) {
	t.Helper()
	rw := river.NewWorkers()
	river.AddWorker(rw, &workers.UndeleteOrgWorker{
		Pool:   h.Pool,
		Logger: silentDomainLogger(),
	})
	c, err := river.NewClient(riverpgxv5.New(h.Pool), &river.Config{
		Logger:  silentDomainLogger(),
		Queues:  map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}},
		Schema:  "river",
		Workers: rw,
	})
	require.NoError(t, err)
	require.NoError(t, c.Start(context.Background()))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = c.Stop(stopCtx)
	})
}

// waitOp blocks until the LRO is done. Polls the operations row
// because LROs split between the legacy in-process goroutine path
// (DeleteOrganization, etc.) and the River-driven path
// (UndeleteOrganization, post-Phase-5). Manager.WaitOperation's
// listener channel only fires for the legacy path; polling covers
// both uniformly.
func waitOp(t *testing.T, h *grpcharness.Harness, op interface{ GetName() string }, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := h.LROManager.GetOperation(context.Background(), op.GetName())
		require.NoError(t, err, "%s: GetOperation failed", label)
		if got.GetDone() {
			if got.GetError() != nil {
				t.Fatalf("%s LRO failed: code=%d msg=%s",
					label, got.GetError().GetCode(), got.GetError().GetMessage())
			}
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s LRO not done within deadline", label)
}
