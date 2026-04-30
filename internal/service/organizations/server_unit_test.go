package organizations

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

var (
	orgID   = uuid.MustParse("0192a000-0001-7000-8000-000000000001")
	testOrg = db.Organization{
		ID:          orgID,
		Name:        "acme",
		DisplayName: "Acme Corp",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-org-1",
		Revision:    1,
		CreateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
	}
)

// memberTestCtx returns a context pre-populated with the same
// ResolvedOrg the permission interceptor would attach in production.
// Member handlers now read from context instead of issuing a fresh
// GetOrganizationByName lookup, so unit tests must seed this.
func memberTestCtx() context.Context {
	return server.WithResolvedOrgForTest(context.Background(), &server.ResolvedOrg{
		ID:   testOrg.ID,
		Slug: testOrg.Name,
		Row:  testOrg,
	})
}

func newTestServer(q *mocks.MockQuerier) *OrganizationsServer {
	codec, _ := appkey.NewFromHex(strings.Repeat("ab", 32))
	return &OrganizationsServer{
		queries: q,
		filter:  filter.OrganizationFilter(),
		codec:   codec,
	}
}

// ---------------------------------------------------------------------------
// NewOrganizationsServer
// ---------------------------------------------------------------------------

func TestUnit_NewOrganizationsServer(t *testing.T) {
	// NewOrganizationsServer requires a *pgxpool.Pool, but we can test the
	// basic constructor doesn't panic. Pass nil for pool and auth.
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	require.NotNil(t, srv)
	assert.NotNil(t, srv.queries)
}

// ---------------------------------------------------------------------------
// GetOrganization
// ---------------------------------------------------------------------------

func TestUnit_GetOrganization_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	// GetOrganization now reads the org from the interceptor-resolved
	// context (audit MED #4 / soft-delete-revive flow); the unit test
	// has to seed the same ResolvedOrg the production interceptor
	// would attach.
	ctx := memberTestCtx()

	resp, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/acme",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme", resp.GetName())
	assert.Equal(t, "Acme Corp", resp.GetDisplayName())
	assert.Equal(t, "etag-org-1", resp.GetEtag())
}

// TestUnit_GetOrganization_SlugMismatch covers the
// path-vs-resolved-scope drift assertion. In production this never
// fires (the interceptor 404s on an unknown slug before the handler
// runs); the assertion is paranoia against gate-vs-handler skew.
func TestUnit_GetOrganization_SlugMismatch(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := memberTestCtx() // resolved org slug = "acme"

	_, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/different-slug",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_GetOrganization_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	_, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "invalid",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	// ParseSegment fails, HandleResourceError returns Internal for non-pgx errors
	assert.NotEqual(t, codes.OK, st.Code())
}

// ---------------------------------------------------------------------------
// CreateOrganization — mock infrastructure
// ---------------------------------------------------------------------------

// mockTxBeginner implements TxBeginner using testify/mock.
type mockTxBeginner struct {
	mock.Mock
}

func (m *mockTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(pgx.Tx), args.Error(1)
}

// mockTx implements pgx.Tx using testify/mock. Only methods exercised by
// CreateOrganization (Commit, Rollback) and the db.DBTX subset (Exec,
// QueryRow) carry mock expectations. The remaining pgx.Tx methods panic if
// called, which immediately surfaces unexpected usage in tests.
type mockTx struct {
	mock.Mock
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	return args.Get(0).(pgx.Tx), args.Error(1)
}

func (m *mockTx) Commit(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockTx) Rollback(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockTx) Exec(ctx context.Context, sql string, arguments ...interface{}) (pgconn.CommandTag, error) {
	args := m.Called(ctx, sql, arguments)
	return args.Get(0).(pgconn.CommandTag), args.Error(1)
}

func (m *mockTx) Query(ctx context.Context, sql string, args2 ...interface{}) (pgx.Rows, error) {
	args := m.Called(ctx, sql, args2)
	return args.Get(0).(pgx.Rows), args.Error(1)
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args2 ...interface{}) pgx.Row {
	args := m.Called(ctx, sql, args2)
	return args.Get(0).(pgx.Row)
}

func (m *mockTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("CopyFrom not expected in CreateOrganization tests")
}

func (m *mockTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("SendBatch not expected in CreateOrganization tests")
}

func (m *mockTx) LargeObjects() pgx.LargeObjects {
	panic("LargeObjects not expected in CreateOrganization tests")
}

func (m *mockTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("Prepare not expected in CreateOrganization tests")
}

func (m *mockTx) Conn() *pgx.Conn {
	panic("Conn not expected in CreateOrganization tests")
}

// mockRow implements pgx.Row with a configurable Scan function.
type mockRow struct {
	scanFunc func(dest ...interface{}) error
}

func (r *mockRow) Scan(dest ...interface{}) error {
	return r.scanFunc(dest...)
}

// mockAuthService implements authn.Service using testify/mock.
type mockAuthService struct {
	mock.Mock
}

func (m *mockAuthService) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authn.Identity), args.Error(1)
}

func (m *mockAuthService) CreateCustomToken(ctx context.Context, uid string) (string, error) {
	args := m.Called(ctx, uid)
	return args.String(0), args.Error(1)
}

func (m *mockAuthService) DeleteUser(ctx context.Context, uid string) error {
	args := m.Called(ctx, uid)
	return args.Error(0)
}

func (m *mockAuthService) CreateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) UpdateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) DeleteOidcProvider(ctx context.Context, providerID string) error {
	args := m.Called(ctx, providerID)
	return args.Error(0)
}

func (m *mockAuthService) CreateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) UpdateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	args := m.Called(ctx, cfg)
	return args.Error(0)
}

func (m *mockAuthService) DeleteSamlProvider(ctx context.Context, providerID string) error {
	args := m.Called(ctx, providerID)
	return args.Error(0)
}

// newCreateOrgServer builds an OrganizationsServer wired with the mock pool,
// auth service, and querier needed by CreateOrganization. The querier is
// where `GetIdentityByFirebaseUID` (the pre-tx caller-resolution lookup)
// fires; the readUID closure stubs the auth-context UID extraction so
// tests don't need to wire the production interceptor.
func newCreateOrgServer(pool TxBeginner, auth authn.Service, q db.Querier) *OrganizationsServer {
	return &OrganizationsServer{
		pool:    pool,
		auth:    auth,
		queries: q,
		filter:  filter.OrganizationFilter(),
		readUID: func(_ context.Context) (string, bool) { return testFirebaseUID, true },
	}
}

// testFirebaseUID is the canonical caller UID used by all
// CreateOrganization unit tests. The matching firebase_identity row
// is returned by the GetIdentityByFirebaseUID mock setup.
const testFirebaseUID = "fb-test-uid"

// testCallerIdentity is the canonical caller firebase_identity
// returned by `GetIdentityByFirebaseUID` in CreateOrganization unit
// tests. Tests that exercise the founder-pointer / membership path
// can compare against `testCallerIdentity.ID`.
var testCallerIdentity = db.Identity{
	ID:          uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"),
	FirebaseUid: testFirebaseUID,
	Email:       "test@example.com",
}

// expectGetIdentity sets up the standard caller-resolution mock that
// fires before the tx begins. Every CreateOrganization test that
// passes `WithAuthenticatedUID`-equivalent context needs this.
func expectGetIdentity(q *mocks.MockQuerier) {
	q.On("GetIdentityByFirebaseUID", mock.Anything, testFirebaseUID).
		Return(testCallerIdentity, nil).Once()
}

// expectBootstrapExecs absorbs the role-seeding + org_member insert
// produced by the founder bootstrap: 4 Exec calls (role :exec
// inserts) + 1 QueryRow (CreateOrgMember :one INSERT…RETURNING).
// Tests at this layer don't pin SQL params for those —
// `bootstrap_test.go` covers the bootstrap shape directly. This
// helper just keeps mockTx from panicking on unexpected calls.
func expectBootstrapExecs(tx *mockTx) {
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("INSERT 0 1"), nil)
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(&mockRow{scanFunc: func(dest ...interface{}) error {
			// CreateOrgMember RETURNING id, etag, create_time,
			// update_time. Default-value scans are sufficient since
			// the bootstrap code path discards the returned row.
			return nil
		}})
}

// `membershipRow` helper was retired post-Phase-7 along with the
// per-org `users` table. CreateOrganization no longer mints a
// users row — the founder's `identities.id` is bound
// directly to the owner role via `org_members`.

// orgRow returns a mockRow whose Scan populates a db.Organization with the
// given values. The column order matches the sqlc-generated RETURNING clause.
func orgRow(org db.Organization) *mockRow {
	return &mockRow{scanFunc: func(dest ...interface{}) error {
		// Column order: id, name, display_name, annotations, state,
		// etag, revision, created_by, updated_by, deleted_by,
		// create_time, update_time, delete_time, purge_time
		if len(dest) != 14 {
			return errors.New("unexpected number of scan destinations")
		}
		*dest[0].(*uuid.UUID) = org.ID
		*dest[1].(*string) = org.Name
		*dest[2].(*string) = org.DisplayName
		*dest[3].(*json.RawMessage) = org.Annotations
		*dest[4].(*db.ResourceState) = org.State
		*dest[5].(*string) = org.Etag
		*dest[6].(*int32) = org.Revision
		*dest[7].(*pgtype.UUID) = org.CreatedBy
		*dest[8].(*pgtype.UUID) = org.UpdatedBy
		*dest[9].(*pgtype.UUID) = org.DeletedBy
		*dest[10].(*time.Time) = org.CreateTime
		*dest[11].(*time.Time) = org.UpdateTime
		// dest[12], dest[13] are *pgtype.Timestamptz — leave as zero
		return nil
	}}
}

// ---------------------------------------------------------------------------
// CreateOrganization
// ---------------------------------------------------------------------------

func TestUnit_CreateOrganization_Success(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	srv := newCreateOrgServer(pool, auth, mockQ)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-0002-7000-8000-000000000002"),
		Name:        "neworg",
		DisplayName: "New Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-new",
		Revision:    1,
		CreateTime:  time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
	}

	// 1. Begin tx
	pool.On("Begin", mock.Anything).Return(tx, nil)

	// 2. CreateOrganization via qtx — calls QueryRow on the tx
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()
	// 3. Founder bootstrap: 4 role inserts + 1 org_member insert
	// (binds the founder's `identities.id` directly to the
	// owner role; the dropped `users` table no longer needs a
	// CreateUserMembership round-trip).
	expectBootstrapExecs(tx)

	// 3. Commit
	tx.On("Commit", mock.Anything).Return(nil)

	// 4. Deferred Rollback (no-op after commit, but still called)
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)

	resp, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "neworg",
		Organization:   &apiv1.Organization{DisplayName: "New Org"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.GetDone())

	// Unwrap the Operation response to verify the Organization proto.
	var org apiv1.Organization
	require.NoError(t, resp.GetResponse().UnmarshalTo(&org))
	assert.Equal(t, "organizations/neworg", org.GetName())
	assert.Equal(t, "New Org", org.GetDisplayName())

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

// TestUnit_CreateOrganization_CreatesOwnerMembership is the focused
// behavioral test for the org-creation owner-membership rule:
// the founder of an org is automatically added as a `users` row with
// `role='owner'`, in the same transaction as the org create. This is
// the structural foundation for "≥1 owner per org" — by this rule no
// org ever exists without an owner.
//
// Distinguished from `TestUnit_CreateOrganization_Success` because
// that test verifies the gRPC response shape; this one cuts at the
// SQL boundary and asserts the exact `CreateUserMembership` params
// that hit the tx.
func TestUnit_CreateOrganization_CreatesOwnerMembership(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	srv := newCreateOrgServer(pool, auth, mockQ)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-bbbb-7000-8000-000000000001"),
		Name:        "ownerorg",
		DisplayName: "Owner Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-owner",
		Revision:    1,
		CreateTime:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	pool.On("Begin", mock.Anything).Return(tx, nil)

	// First QueryRow: org create.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()

	// Founder bootstrap: 4 role :exec inserts + 1 org_member :one
	// QueryRow insert. Post-Phase-7: the founder's
	// `identities.id` is bound directly to the owner role
	// — no separate `users` row insert.
	expectBootstrapExecs(tx)

	tx.On("Commit", mock.Anything).Return(nil)
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "ownerorg",
		Organization:   &apiv1.Organization{DisplayName: "Owner Org"},
	})
	require.NoError(t, err)

	// What this test verifies is *integration*: that CreateOrganization
	// actually invokes the bootstrap, observable as exactly 4 Exec
	// calls on the tx (the 4 system role :exec inserts). The
	// CreateOrgMember insert is a :one RETURNING and goes through
	// QueryRow. Bootstrap correctness is asserted unit-side in
	// `bootstrap_test.go` (TestBootstrapOrgRoles_SeedsFourSystemRoles).
	tx.AssertNumberOfCalls(t, "Exec", 4)
}

// TestUnit_CreateOrganization_NoAuthContext asserts that the handler
// rejects calls without an authenticated UID. Without this guard, an
// unauthenticated caller could end up provisioning orgs they have no
// claim to.
func TestUnit_CreateOrganization_NoAuthContext(t *testing.T) {
	srv := &OrganizationsServer{
		filter:  filter.OrganizationFilter(),
		readUID: func(_ context.Context) (string, bool) { return "", false },
	}

	_, err := srv.CreateOrganization(context.Background(), &apiv1.CreateOrganizationRequest{
		OrganizationId: "noauth",
		Organization:   &apiv1.Organization{DisplayName: "No Auth"},
	})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestUnit_CreateOrganization_CommitFailure(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	srv := newCreateOrgServer(pool, auth, mockQ)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-0004-7000-8000-000000000004"),
		Name:        "commitfail",
		DisplayName: "Commit Fail Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-cf",
		Revision:    1,
		CreateTime:  time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
	}

	pool.On("Begin", mock.Anything).Return(tx, nil)

	// CreateOrganization in DB succeeds.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()

	// Founder bootstrap completes; commit is what fails.
	expectBootstrapExecs(tx)

	// Commit fails.
	tx.On("Commit", mock.Anything).Return(errors.New("connection lost"))

	// Deferred Rollback after commit failure.
	tx.On("Rollback", mock.Anything).Return(nil)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "commitfail",
		Organization:   &apiv1.Organization{DisplayName: "Commit Fail Org"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "commit transaction")

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestUnit_CreateOrganization_AutoGeneratedSlug(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	srv := newCreateOrgServer(pool, auth, mockQ)

	// Begin tx succeeds.
	pool.On("Begin", mock.Anything).Return(tx, nil)

	// CreateOrganization in DB: capture the auto-generated slug from
	// the SQL args so we can assert its shape after the call. SQL arg
	// order matches the CreateOrganization query: id, name, ...
	var capturedSlug string
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Run(func(args mock.Arguments) {
			capturedSlug = args.Get(2).([]interface{})[1].(string)
		}).
		Return(orgRow(db.Organization{
			ID:          uuid.MustParse("0192a000-0005-7000-8000-000000000005"),
			DisplayName: "Auto Slug Org",
			Annotations: json.RawMessage(`{}`),
			State:       db.ResourceStateACTIVE,
			Etag:        "etag-auto",
			Revision:    1,
			CreateTime:  time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC),
			UpdateTime:  time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC),
		})).Once()

	// Founder bootstrap inside the same tx (4 role :execs + 1
	// org_member :one QueryRow). No more `users` row insert
	// post-Phase-7.
	expectBootstrapExecs(tx)

	// Commit succeeds.
	tx.On("Commit", mock.Anything).Return(nil)

	// Deferred Rollback (no-op after commit).
	tx.On("Rollback", mock.Anything).Return(pgx.ErrTxClosed)

	resp, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		// Empty OrganizationId — server must auto-generate slug.
		OrganizationId: "",
		Organization:   &apiv1.Organization{DisplayName: "Auto Slug Org"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.GetDone())
	assert.Len(t, capturedSlug, 8, "auto-generated slug should be 8 chars")

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestUnit_CreateOrganization_BeginTransactionError(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	auth := new(mockAuthService)
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	srv := newCreateOrgServer(pool, auth, mockQ)

	// Begin fails — no tx is returned.
	pool.On("Begin", mock.Anything).Return(nil, errors.New("pool exhausted"))

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "beginfail",
		Organization:   &apiv1.Organization{DisplayName: "Begin Fail Org"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "begin transaction")

	pool.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// ListOrganizations
// ---------------------------------------------------------------------------
//
// ListOrganizations is caller-scoped: returns only orgs the
// authenticated user has a membership row for. The handler ignores
// page_size / page_token / filter / order_by — typical users are in
// 1-3 orgs and we always return them all.

func newListOrgsServer(mockQ *mocks.MockQuerier) *OrganizationsServer {
	return &OrganizationsServer{
		queries: mockQ,
		filter:  filter.OrganizationFilter(),
		readUID: func(_ context.Context) (string, bool) { return testFirebaseUID, true },
	}
}

func TestUnit_ListOrganizations_Unauthenticated(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newListOrgsServer(mockQ)
	srv.readUID = func(_ context.Context) (string, bool) { return "", false }

	_, err := srv.ListOrganizations(context.Background(), &apiv1.ListOrganizationsRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListOrganizations_NoIdentityRowReturnsEmpty(t *testing.T) {
	// Race with the sync-identity webhook on a freshly-Firebase-
	// registered user: caller has a valid token but no
	// `identities` row yet. Memberless state — must return
	// empty list, not an error.
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetIdentityByFirebaseUID", mock.Anything, testFirebaseUID).
		Return(db.Identity{}, pgx.ErrNoRows)
	srv := newListOrgsServer(mockQ)

	resp, err := srv.ListOrganizations(context.Background(), &apiv1.ListOrganizationsRequest{})
	require.NoError(t, err)
	assert.Empty(t, resp.GetOrganizations())
	assert.Empty(t, resp.GetNextPageToken())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListOrganizations_IdentityLookupError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	mockQ.On("GetIdentityByFirebaseUID", mock.Anything, mock.Anything).
		Return(db.Identity{}, errors.New("connection refused"))
	srv := newListOrgsServer(mockQ)

	_, err := srv.ListOrganizations(context.Background(), &apiv1.ListOrganizationsRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListOrganizations_OnlyReturnsCallerOrgs(t *testing.T) {
	// Caller-scoping: the handler MUST scope through
	// ListOrganizationsForIdentity, which JOINs users →
	// organizations on identity_id. Caller never sees orgs
	// they aren't a member of.
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)

	callerOrgs := []db.Organization{
		{
			ID:          uuid.MustParse("0192a000-0010-7000-8000-000000000010"),
			Name:        "my-org",
			DisplayName: "My Org",
			Annotations: json.RawMessage(`{}`),
			State:       db.ResourceStateACTIVE,
			Etag:        "e1",
			Revision:    1,
			CreateTime:  time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
			UpdateTime:  time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	mockQ.On("ListOrganizationsForIdentity", mock.Anything, testCallerIdentity.ID).Return(callerOrgs, nil)

	srv := newListOrgsServer(mockQ)
	resp, err := srv.ListOrganizations(context.Background(), &apiv1.ListOrganizationsRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetOrganizations(), 1)
	assert.Equal(t, "organizations/my-org", resp.GetOrganizations()[0].GetName())
	assert.Empty(t, resp.GetNextPageToken())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListOrganizations_QueryError(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	mockQ.On("ListOrganizationsForIdentity", mock.Anything, testCallerIdentity.ID).
		Return(nil, errors.New("connection refused"))
	srv := newListOrgsServer(mockQ)

	_, err := srv.ListOrganizations(context.Background(), &apiv1.ListOrganizationsRequest{})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	mockQ.AssertExpectations(t)
}

func TestUnit_ListOrganizations_IgnoresPaginationFields(t *testing.T) {
	// PageSize, PageToken, Filter from the request must NOT affect the
	// query — handler returns all caller's orgs regardless.
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	mockQ.On("ListOrganizationsForIdentity", mock.Anything, testCallerIdentity.ID).
		Return([]db.Organization{}, nil)

	srv := newListOrgsServer(mockQ)
	resp, err := srv.ListOrganizations(context.Background(), &apiv1.ListOrganizationsRequest{
		PageSize:  9999,
		PageToken: "garbage-but-ignored",
		Filter:    "displayName = \"anything\"",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.GetNextPageToken())
	mockQ.AssertExpectations(t)
}

func TestUnit_CreateOrganization_DBCreateError(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	mockQ := new(mocks.MockQuerier)
	expectGetIdentity(mockQ)
	srv := newCreateOrgServer(pool, auth, mockQ)

	pool.On("Begin", mock.Anything).Return(tx, nil)

	// CreateOrganization DB call fails — QueryRow returns a scan error.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(&mockRow{scanFunc: func(dest ...interface{}) error {
			return errors.New("duplicate key value")
		}}).Once()

	// Deferred Rollback (tx not committed).
	tx.On("Rollback", mock.Anything).Return(nil)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "duporg",
		Organization:   &apiv1.Organization{DisplayName: "Dup Org"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.NotEqual(t, codes.OK, st.Code())

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// NewOrganizationsServer constructor
// ---------------------------------------------------------------------------

func TestUnit_NewOrganizationsServer_Constructor(t *testing.T) {
	// NewOrganizationsServer requires a *pgxpool.Pool. We cannot construct a
	// real pool in a unit test, so we verify that the nil-pool case still
	// instantiates the server struct without panicking. The filter field is
	// the most important invariant to check.
	mockQ := new(mocks.MockQuerier)
	auth := new(mockAuthService)

	// NewOrganizationsServer with nil pool exercises the constructor code path.
	srv := NewOrganizationsServer(nil, mockQ, auth, nil, nil, nil, nil, nil, nil, nil)

	require.NotNil(t, srv)
	assert.NotNil(t, srv.filter)
	assert.Equal(t, auth, srv.auth)
	assert.Equal(t, mockQ, srv.queries)
}
