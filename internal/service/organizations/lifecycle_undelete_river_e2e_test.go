//go:build dev

package organizations_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
	"github.com/dashkan/pivox/internal/workers"
)

// TestE2E_UndeleteOrganization_EnqueuesAndCompletesViaRiver pins the
// #69 Phase 5 contract: UndeleteOrganization is the first LRO ported
// off the legacy CreateAndRun + runWork goroutine path onto River.
// pivox-cloud's RPC handler enqueues a job; pivox-worker's
// UndeleteOrgWorker processes it and completes the operations row.
//
// newLifecycleHarness wires an in-process River client with
// UndeleteOrgWorker registered (same shape as cmd/pivox-worker/main.go),
// so this test exercises the real path end-to-end:
// NewLro → river queue → worker → CompleteOperation + JobCompleteTx
// in one pgx tx.
//
// Successor LRO ports (#69 Phase 6) follow the same shape:
//
//  1. Register the new worker in startOrgLifecycleWorkers.
//  2. Bring the row to the precondition state via existing handlers.
//  3. Call the migrated handler — should return done=false.
//  4. Assert operations row + river_job row both exist post-handler.
//  5. Wait for the operations row to flip done; assert terminal state.
func TestE2E_UndeleteOrganization_EnqueuesAndCompletesViaRiver(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	h := newLifecycleHarness(t)
	client := apiv1.NewOrganizationsClient(h.Conn())
	ctx := context.Background()

	// Setup: own → soft-delete the org so it's eligible for undelete.
	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "founder"})
	h.SetCaller(owner)

	createOp, err := client.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "revive-via-river",
		Organization:   &apiv1.Organization{DisplayName: "Revive Via River"},
	})
	require.NoError(t, err)
	require.True(t, createOp.GetDone(), "CreateOrganization is sync")

	deleteOp, err := client.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/revive-via-river",
	})
	require.NoError(t, err)
	waitOp(t, h, deleteOp, "DeleteOrganization") // legacy in-process goroutine path

	// === Phase 5 surface ===

	undeleteOp, err := client.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/revive-via-river",
	})
	require.NoError(t, err)

	// Handler returns immediately; work is in the worker process,
	// not the cloud handler. Distinct from the legacy LRO shape
	// where the goroutine often completed before the response
	// returned. (Strictly, a fast worker tick can race in and flip
	// done=true before this assertion fires; in practice the
	// assertion holds because the handler returns synchronously and
	// we sample the snapshot it returned, not a fresh DB read.)
	assert.False(t, undeleteOp.GetDone(),
		"River-driven LROs return done=false until the worker runs")
	assert.Contains(t, undeleteOp.GetName(), "organizations/revive-via-river/operations/")

	// Atomic enqueue: river_job row visible after handler return.
	// Direct SQL — rivertest.RequireInsertedTx has a schema-resolution
	// gap when used outside an explicitly schema-aware client;
	// querying `river.river_job` by fully-qualified name sidesteps it.
	// The job may already be running/completed by the time we read,
	// so just assert presence regardless of state.
	var jobCount int
	err = h.Pool.QueryRow(ctx,
		`SELECT count(*) FROM river.river_job WHERE kind = $1`,
		workers.UndeleteOrgArgs{}.Kind(),
	).Scan(&jobCount)
	require.NoError(t, err)
	assert.Equal(t, 1, jobCount, "exactly one UndeleteOrg river_job should have been inserted by NewLro")

	// === Wait for the worker to process the job ===

	completed := waitOpUntilDone(t, h, undeleteOp, 10*time.Second, "UndeleteOrganization")
	require.Nil(t, completed.GetError(),
		"LRO must complete cleanly: %v", completed.GetError())
	require.NotNil(t, completed.GetResponse(),
		"completed op should carry the org as response")

	// Org is back to ACTIVE.
	revived, err := h.Queries.GetOrganizationByNameForGate(ctx, "revive-via-river")
	require.NoError(t, err)
	assert.Equal(t, "ACTIVE", string(revived.State),
		"UndeleteOrganization SQL must have committed in the worker's tx")
}
