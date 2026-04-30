package organizations

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fakeProgress captures phase updates from runDeleteOrganization
// so tests can assert the state machine progresses through the
// correct phases in order.
type fakeProgress struct {
	phases []apiv1.DeleteOrganizationMetadata_Phase
}

func (f *fakeProgress) Update(_ context.Context, m proto.Message) {
	if md, ok := m.(*apiv1.DeleteOrganizationMetadata); ok {
		f.phases = append(f.phases, md.GetPhase())
	}
}

// slogTestLogger silences LRO Manager logs in lifecycle tests.
func slogTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubCaller returns a CallerIdentityResolver that always succeeds
// with a deterministic identity. Lifecycle handler tests don't need
// real caller resolution; they're exercising state machines.
func stubCaller(_ *testing.T) server.CallerIdentityResolver {
	id := uuid.MustParse("0192a000-cafe-7000-8000-000000000001")
	return func(_ context.Context) (uuid.UUID, error) { return id, nil }
}

// --- enforceSoftDeleteGate (unit) ---

func TestEnforceSoftDeleteGate_ActiveOrgPassesAnyPerm(t *testing.T) {
	// ACTIVE org accepts every permission — the gate is a no-op.
	cases := []string{
		"organizations.read", "organizations.update", "organizations.delete",
		"members.create", "assets.assets.create",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := server.EnforceSoftDeleteGateForTest(db.ResourceStateACTIVE, p, "acme")
			require.NoError(t, err)
		})
	}
}

func TestEnforceSoftDeleteGate_DeletedOrgAllowsReadsAndUndelete(t *testing.T) {
	cases := []string{
		"organizations.read", "users.read", "members.read",
		"organizations.delete", // gates UndeleteOrganization
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := server.EnforceSoftDeleteGateForTest(db.ResourceStateDELETEREQUESTED, p, "acme")
			require.NoError(t, err)
		})
	}
}

func TestEnforceSoftDeleteGate_DeletedOrgRejectsMutations(t *testing.T) {
	cases := []string{
		"organizations.update", "members.create", "members.update",
		"members.delete", "assets.assets.create", "domains.create",
	}
	for _, p := range cases {
		t.Run(p, func(t *testing.T) {
			err := server.EnforceSoftDeleteGateForTest(db.ResourceStateDELETEREQUESTED, p, "acme")
			require.Error(t, err)
			assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		})
	}
}

// --- DeleteOrganization handler validation (pre-LRO) ---

func newLifecycleServer(q db.Querier, lroManager *lro.Manager, caller server.CallerIdentityResolver) *OrganizationsServer {
	return &OrganizationsServer{queries: q, lroManager: lroManager, caller: caller}
}

func newTestLROManager(q db.Querier) *lro.Manager {
	return lro.NewManager(q, slogTestLogger())
}

func TestDeleteOrganization_RejectsNonActiveOrg(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := newLifecycleServer(q, newTestLROManager(q), stubCaller(t))

	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"),
		Slug: "acme",
		Row:  db.Organization{State: db.ResourceStateDELETEREQUESTED, Etag: "v1"},
	})
	_, err := srv.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/acme",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestDeleteOrganization_EtagMismatchFails(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := newLifecycleServer(q, newTestLROManager(q), stubCaller(t))

	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"),
		Slug: "acme",
		Row:  db.Organization{State: db.ResourceStateACTIVE, Etag: "actual"},
	})
	_, err := srv.DeleteOrganization(ctx, &apiv1.DeleteOrganizationRequest{
		Name: "organizations/acme",
		Etag: "stale",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// --- runDeleteOrganization orchestrator state machine ---

func TestRunDeleteOrganization_SoftDeletePhases(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("CancelRunningOpsForOrg", mock.Anything, pgtype.UUID{Bytes: orgID, Valid: true}).Return([]uuid.UUID{}, nil)
	q.On("SoftDeleteOrganization", mock.Anything, db.SoftDeleteOrganizationParams{
		ID: orgID, DeletedBy: pgtype.UUID{},
	}).Return(db.Organization{ID: orgID, Name: "acme", State: db.ResourceStateDELETEREQUESTED}, nil)

	srv := &OrganizationsServer{queries: q}
	progress := &fakeProgress{}
	result, err := srv.runDeleteOrganization(
		context.Background(), progress, orgID,
		"organizations/acme", pgtype.UUID{}, false /* force */, "etag-1")
	require.NoError(t, err)
	assert.Equal(t, []apiv1.DeleteOrganizationMetadata_Phase{
		apiv1.DeleteOrganizationMetadata_CANCELLING_OPERATIONS,
		apiv1.DeleteOrganizationMetadata_MARKING_DELETED,
		apiv1.DeleteOrganizationMetadata_COMPLETED,
	}, progress.phases)
	resultOrg, ok := result.(*apiv1.Organization)
	require.True(t, ok)
	assert.Equal(t, "organizations/acme", resultOrg.GetName())
	q.AssertExpectations(t)
}

func TestRunDeleteOrganization_ForcePhases(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("CancelRunningOpsForOrg", mock.Anything, pgtype.UUID{Bytes: orgID, Valid: true}).Return([]uuid.UUID{}, nil)
	q.On("PurgeOrganization", mock.Anything, db.PurgeOrganizationParams{
		ID:   orgID,
		Etag: "etag-1",
	}).Return(orgID, nil)

	srv := &OrganizationsServer{queries: q}
	progress := &fakeProgress{}
	_, err := srv.runDeleteOrganization(
		context.Background(), progress, orgID,
		"organizations/acme", pgtype.UUID{}, true /* force */, "etag-1")
	require.NoError(t, err)
	assert.Equal(t, []apiv1.DeleteOrganizationMetadata_Phase{
		apiv1.DeleteOrganizationMetadata_CANCELLING_OPERATIONS,
		apiv1.DeleteOrganizationMetadata_PURGING,
		apiv1.DeleteOrganizationMetadata_COMPLETED,
	}, progress.phases)
	// SoftDeleteOrganization must NOT have been called on the force path.
	q.AssertNotCalled(t, "SoftDeleteOrganization", mock.Anything, mock.Anything)
	q.AssertExpectations(t)
}

func TestRunDeleteOrganization_RaceWithConcurrentDelete(t *testing.T) {
	// Soft-delete query returns ErrNoRows when the row's state was
	// flipped between handler-time validation and the LRO firing.
	// The orchestrator must surface FAILED_PRECONDITION.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("CancelRunningOpsForOrg", mock.Anything, mock.Anything).Return([]uuid.UUID{}, nil)
	q.On("SoftDeleteOrganization", mock.Anything, mock.Anything).Return(db.Organization{}, pgx.ErrNoRows)

	srv := &OrganizationsServer{queries: q}
	_, err := srv.runDeleteOrganization(
		context.Background(), &fakeProgress{}, orgID,
		"organizations/acme", pgtype.UUID{}, false, "etag-1")
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

// TestRunDeleteOrganization_ForceEtagDrift verifies that the
// PURGING phase refuses to fire when the row's etag changed since
// the handler validated it (e.g., a soft-delete + undelete cycle
// raced the LRO worker). Without the guard, force-purge would wipe
// the row anyway — the audit's primary concern.
func TestRunDeleteOrganization_ForceEtagDrift(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("CancelRunningOpsForOrg", mock.Anything, mock.Anything).Return([]uuid.UUID{}, nil)
	q.On("PurgeOrganization", mock.Anything, db.PurgeOrganizationParams{
		ID:   orgID,
		Etag: "stale-etag",
	}).Return(uuid.Nil, pgx.ErrNoRows)

	srv := &OrganizationsServer{queries: q}
	_, err := srv.runDeleteOrganization(
		context.Background(), &fakeProgress{}, orgID,
		"organizations/acme", pgtype.UUID{}, true /* force */, "stale-etag")
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Contains(t, err.Error(), "revision changed")
}

func TestRunDeleteOrganization_CancelOpsFailureIsInternal(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("CancelRunningOpsForOrg", mock.Anything, mock.Anything).Return([]uuid.UUID{}, errors.New("db down"))

	srv := &OrganizationsServer{queries: q}
	_, err := srv.runDeleteOrganization(
		context.Background(), &fakeProgress{}, orgID,
		"organizations/acme", pgtype.UUID{}, false, "etag-1")
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- UndeleteOrganization handler validation ---

func TestUndeleteOrganization_RejectsActiveOrg(t *testing.T) {
	q := new(mocks.MockQuerier)
	srv := newLifecycleServer(q, newTestLROManager(q), stubCaller(t))

	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"),
		Slug: "acme",
		Row:  db.Organization{State: db.ResourceStateACTIVE, Etag: "v1"},
	})
	_, err := srv.UndeleteOrganization(ctx, &apiv1.UndeleteOrganizationRequest{
		Name: "organizations/acme",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}
