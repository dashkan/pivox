package organizations

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// --- parseDomainSegment ---

func TestParseDomainSegment_Valid(t *testing.T) {
	got, err := parseDomainSegment("organizations/acme/domains/example.com", "acme")
	require.NoError(t, err)
	assert.Equal(t, "example.com", got)
}

func TestParseDomainSegment_OrgMismatch(t *testing.T) {
	_, err := parseDomainSegment("organizations/different/domains/example.com", "acme")
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestParseDomainSegment_Malformed(t *testing.T) {
	cases := []string{
		"",
		"organizations/acme",
		"organizations/acme/domains/",
		"users/me/domains/example.com",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			_, err := parseDomainSegment(c, "acme")
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// --- generateVerificationToken ---

func TestGenerateVerificationToken_Unique(t *testing.T) {
	a, err := generateVerificationToken()
	require.NoError(t, err)
	b, err := generateVerificationToken()
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	// 32 bytes → 43 chars unpadded base64url.
	assert.Len(t, a, 43)
}

// --- GetDomain ---

func TestGetDomain_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetDomainByName", mock.Anything, mock.Anything).Return(db.Domain{}, pgx.ErrNoRows)
	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.GetDomain(ctx, &apiv1.GetDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestGetDomain_ReturnsRow(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetDomainByName", mock.Anything, mock.Anything).Return(db.Domain{
		Domain: "x.com", State: db.DomainStatePENDING,
	}, nil)
	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	got, err := srv.GetDomain(ctx, &apiv1.GetDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/domains/x.com", got.GetName())
	assert.Equal(t, apiv1.Domain_PENDING, got.GetState())
}

// --- CreateDomain validation ---

func TestCreateDomain_RejectsEmptyDomain(t *testing.T) {
	srv := &OrganizationsServer{}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.CreateDomain(ctx, &apiv1.CreateDomainRequest{
		Parent: "organizations/acme",
		Domain: &apiv1.Domain{Domain: ""},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateDomain_DomainIDMustMatchOrBeEmpty(t *testing.T) {
	srv := &OrganizationsServer{}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.CreateDomain(ctx, &apiv1.CreateDomainRequest{
		Parent:   "organizations/acme",
		Domain:   &apiv1.Domain{Domain: "example.com"},
		DomainId: "wrong.com",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestCreateDomain_AlreadyExistsHidesHoldingOrg(t *testing.T) {
	// AIP-133 / security: a globally-claimed domain returns
	// ALREADY_EXISTS without naming the holder. Verify the
	// pgconn unique-violation maps cleanly.
	q := new(mocks.MockQuerier)
	q.On("CreateDomain", mock.Anything, mock.Anything).
		Return(db.Domain{}, &pgconn.PgError{Code: apierr.PgUniqueViolation, Message: "duplicate key"})
	srv := &OrganizationsServer{queries: q, caller: stubCaller(t)}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.CreateDomain(ctx, &apiv1.CreateDomainRequest{
		Parent: "organizations/acme",
		Domain: &apiv1.Domain{Domain: "example.com"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
	// Error message must not name the holding org.
	assert.NotContains(t, err.Error(), "different")
}

// --- DeleteDomain preconditions ---

func TestDeleteDomain_NotFound(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{}, pgx.ErrNoRows)
	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestDeleteDomain_EtagMismatch(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{
		Domain: "x.com", Etag: "actual",
	}, nil)
	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{
		Name: "organizations/acme/domains/x.com",
		Etag: "stale",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestDeleteDomain_LastVerifiedOnEnabledSSORefuses(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{
		ID:     uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"),
		Domain: "x.com", State: db.DomainStateVERIFIED,
	}, nil)
	q.On("GetSsoConfigByOrgIDForUpdate", mock.Anything, orgID).Return(db.SsoConfig{Enabled: true}, nil)
	q.On("CountVerifiedDomainsByOrg", mock.Anything, orgID).Return(int64(1), nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	q.AssertNotCalled(t, "DeleteDomain", mock.Anything, mock.Anything)
}

func TestDeleteDomain_VerifiedWithExtraVerifiedAllowed(t *testing.T) {
	// Same setup as above but the org has 2+ verified domains, so
	// removing one is safe even with SSO enabled.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	domainID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{
		ID: domainID, Domain: "x.com", State: db.DomainStateVERIFIED, Etag: "v1",
	}, nil)
	q.On("GetSsoConfigByOrgIDForUpdate", mock.Anything, orgID).Return(db.SsoConfig{Enabled: true}, nil)
	q.On("CountVerifiedDomainsByOrg", mock.Anything, orgID).Return(int64(3), nil)
	q.On("CancelDomainOpsForDomain", mock.Anything, "organizations/acme/domains/x.com").
		Return([]uuid.UUID{}, nil)
	q.On("DeleteDomain", mock.Anything, db.DeleteDomainParams{ID: domainID, OrgID: orgID}).Return(nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.NoError(t, err)
	q.AssertExpectations(t)
}

func TestDeleteDomain_NoSsoConfigSkipsGuard(t *testing.T) {
	// No SSO config row → no precondition. Even removing a
	// verified domain proceeds.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	domainID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{
		ID: domainID, Domain: "x.com", State: db.DomainStateVERIFIED,
	}, nil)
	q.On("GetSsoConfigByOrgIDForUpdate", mock.Anything, orgID).Return(db.SsoConfig{}, pgx.ErrNoRows)
	q.On("CancelDomainOpsForDomain", mock.Anything, mock.Anything).Return([]uuid.UUID{}, nil)
	q.On("DeleteDomain", mock.Anything, db.DeleteDomainParams{ID: domainID, OrgID: orgID}).Return(nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.NoError(t, err)
	q.AssertExpectations(t)
	q.AssertNotCalled(t, "CountVerifiedDomainsByOrg", mock.Anything, mock.Anything)
}

func TestDeleteDomain_PendingRowSkipsSSO(t *testing.T) {
	// Removing a non-VERIFIED domain doesn't affect SSO posture,
	// so the precondition isn't checked at all.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	domainID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{
		ID: domainID, Domain: "x.com", State: db.DomainStatePENDING,
	}, nil)
	q.On("CancelDomainOpsForDomain", mock.Anything, mock.Anything).Return([]uuid.UUID{}, nil)
	q.On("DeleteDomain", mock.Anything, db.DeleteDomainParams{ID: domainID, OrgID: orgID}).Return(nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.NoError(t, err)
	q.AssertNotCalled(t, "GetSsoConfigByOrgIDForUpdate", mock.Anything, mock.Anything)
}

func TestDeleteDomain_CancelOpsFailureSurfacesInternal(t *testing.T) {
	// If cancellation of in-flight LROs fails, the row is NOT
	// deleted — we don't want a verifying goroutine to keep
	// running against a deleted row.
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByNameForUpdate", mock.Anything, mock.Anything).Return(db.Domain{
		Domain: "x.com", State: db.DomainStatePENDING,
	}, nil)
	q.On("CancelDomainOpsForDomain", mock.Anything, mock.Anything).Return([]uuid.UUID{}, errors.New("db down"))

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	_, err := srv.DeleteDomain(ctx, &apiv1.DeleteDomainRequest{Name: "organizations/acme/domains/x.com"})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	q.AssertNotCalled(t, "DeleteDomain", mock.Anything, mock.Anything)
}

// --- runVerifyDomain LRO work fn ---

// captureProgress records the metadata phase + attempt count for
// each Update call so tests can assert state-machine progression.
type captureProgress struct {
	updates []*apiv1.CreateDomainMetadata
}

func (c *captureProgress) Update(_ context.Context, m proto.Message) {
	if md, ok := m.(*apiv1.CreateDomainMetadata); ok {
		c.updates = append(c.updates, md)
	}
}

func TestRunVerifyDomain_VerifiedOnFirstCheck(t *testing.T) {
	// The verify-domain worker has already flipped the row to
	// VERIFIED before the LRO fires its first poll. The work fn
	// returns immediately with the verified Domain.
	SetDomainPollIntervalForTest(time.Millisecond)
	defer SetDomainPollIntervalForTest(0)

	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	domainID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByID", mock.Anything, db.GetDomainByIDParams{ID: domainID, OrgID: orgID}).
		Return(db.Domain{ID: domainID, Domain: "x.com", State: db.DomainStateVERIFIED}, nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	progress := &captureProgress{}
	result, err := srv.runVerifyDomain(context.Background(), progress, domainID, orgID, "acme",
		"organizations/acme/domains/x.com", time.Now().Add(time.Hour))
	require.NoError(t, err)
	d, ok := result.(*apiv1.Domain)
	require.True(t, ok)
	assert.Equal(t, apiv1.Domain_VERIFIED, d.GetState())
	require.Len(t, progress.updates, 1)
	assert.Equal(t, apiv1.CreateDomainMetadata_VERIFIED, progress.updates[0].GetPhase())
}

func TestRunVerifyDomain_FailedSurfacesFailedPrecondition(t *testing.T) {
	SetDomainPollIntervalForTest(time.Millisecond)
	defer SetDomainPollIntervalForTest(0)

	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	domainID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByID", mock.Anything, mock.Anything).
		Return(db.Domain{ID: domainID, Domain: "x.com", State: db.DomainStateFAILED}, nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	progress := &captureProgress{}
	_, err := srv.runVerifyDomain(context.Background(), progress, domainID, orgID, "acme",
		"organizations/acme/domains/x.com", time.Now().Add(time.Hour))
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, apiv1.CreateDomainMetadata_FAILED, progress.updates[0].GetPhase())
}

func TestRunVerifyDomain_DeadlineElapsedMarksFailed(t *testing.T) {
	// Row is still PENDING when the grace window elapses → work fn
	// flips state to FAILED and reports EXPIRED phase.
	SetDomainPollIntervalForTest(time.Millisecond)
	defer SetDomainPollIntervalForTest(0)

	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	domainID := uuid.MustParse("0192a000-bbbb-7000-8000-000000000002")
	q := new(mocks.MockQuerier)
	q.On("GetDomainByID", mock.Anything, mock.Anything).
		Return(db.Domain{ID: domainID, Domain: "x.com", State: db.DomainStatePENDING}, nil)
	q.On("MarkDomainFailed", mock.Anything, domainID).
		Return(db.Domain{ID: domainID, Domain: "x.com", State: db.DomainStateFAILED}, nil)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	progress := &captureProgress{}
	_, err := srv.runVerifyDomain(context.Background(), progress, domainID, orgID, "acme",
		"organizations/acme/domains/x.com", time.Now().Add(-time.Hour) /* already past */)
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	assert.Equal(t, apiv1.CreateDomainMetadata_EXPIRED, progress.updates[0].GetPhase())
}

func TestRunVerifyDomain_RowDeletedSurfacesFailedPrecondition(t *testing.T) {
	// DeleteDomain ran while the LRO was polling; the row is gone.
	// Work fn returns FailedPrecondition rather than Internal so
	// the operation reflects the user-driven cancellation.
	SetDomainPollIntervalForTest(time.Millisecond)
	defer SetDomainPollIntervalForTest(0)

	q := new(mocks.MockQuerier)
	q.On("GetDomainByID", mock.Anything, mock.Anything).
		Return(db.Domain{}, pgx.ErrNoRows)

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	_, err := srv.runVerifyDomain(context.Background(), &captureProgress{},
		uuid.New(), uuid.New(), "acme", "organizations/acme/domains/x.com",
		time.Now().Add(time.Hour))
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}

func TestRunVerifyDomain_CtxCancelExitsCleanly(t *testing.T) {
	// Server shutdown or DeleteDomain-cancel propagates ctx
	// cancellation. Work fn returns ctx.Err(); LRO Manager maps
	// that to operation status Cancelled.
	SetDomainPollIntervalForTest(50 * time.Millisecond)
	defer SetDomainPollIntervalForTest(0)

	q := new(mocks.MockQuerier)
	q.On("GetDomainByID", mock.Anything, mock.Anything).
		Return(db.Domain{Domain: "x.com", State: db.DomainStatePENDING}, nil).Maybe()

	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel so ctx.Done() fires before the first ticker tick

	_, err := srv.runVerifyDomain(ctx, &captureProgress{}, uuid.New(), uuid.New(), "acme",
		"organizations/acme/domains/x.com", time.Now().Add(time.Hour))
	require.Error(t, err)
	// ctx.Err() — Canceled or DeadlineExceeded — bubbles up.
	assert.True(t, errors.Is(err, context.Canceled))
}

// --- ListDomains ---

func TestListDomains_ReturnsRows(t *testing.T) {
	orgID := uuid.MustParse("0192a000-aaaa-7000-8000-000000000001")
	q := new(mocks.MockQuerier)
	q.On("ListDomainsByOrg", mock.Anything, orgID).Return([]db.Domain{
		{Domain: "a.com", State: db.DomainStateVERIFIED, VerifiedTime: pgtype.Timestamptz{Valid: false}},
		{Domain: "b.com", State: db.DomainStatePENDING},
	}, nil)
	srv := &OrganizationsServer{txer: &db.PassthroughTxer{Q: q}, queries: q}
	ctx := server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID: orgID, Slug: "acme",
	})
	resp, err := srv.ListDomains(ctx, &apiv1.ListDomainsRequest{Parent: "organizations/acme"})
	require.NoError(t, err)
	require.Len(t, resp.GetDomains(), 2)
	assert.Equal(t, "organizations/acme/domains/a.com", resp.GetDomains()[0].GetName())
}
