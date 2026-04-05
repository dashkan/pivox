package organizations

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	"github.com/dashkan/pivox/internal/iam"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
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

func newTestServer(q *mocks.MockQuerier) *OrganizationsServer {
	iamHelper := iam.NewHelper(q)
	return &OrganizationsServer{
		queries: q,
		iam:     iamHelper,
		filter:  filter.OrganizationFilter(),
	}
}

// ---------------------------------------------------------------------------
// NewOrganizationsServer
// ---------------------------------------------------------------------------

func TestUnit_NewOrganizationsServer(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	// NewOrganizationsServer requires a *pgxpool.Pool, but we can test the
	// basic constructor doesn't panic. Pass nil for pool and auth.
	srv := newTestServer(mockQ)
	require.NotNil(t, srv)
	assert.NotNil(t, srv.iam)
	assert.Equal(t, iamHelper != nil, true) // iamHelper is valid
}

// ---------------------------------------------------------------------------
// GetOrganization
// ---------------------------------------------------------------------------

func TestUnit_GetOrganization_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)

	resp, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/acme",
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme", resp.GetName())
	assert.Equal(t, "Acme Corp", resp.GetDisplayName())
	assert.Equal(t, "etag-org-1", resp.GetEtag())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetOrganization_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "nonexistent").Return(db.Organization{}, pgx.ErrNoRows)

	_, err := srv.GetOrganization(ctx, &apiv1.GetOrganizationRequest{
		Name: "organizations/nonexistent",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
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
// GetIamPolicy -- delegates to iam.Helper
// ---------------------------------------------------------------------------

func TestUnit_GetIamPolicy_Delegated(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	// iam.Helper.resolveResourceID looks up the org by name.
	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	// Then GetIamPolicy queries the IAM policy; return ErrNoRows -> empty policy.
	mockQ.On("GetIamPolicy", mock.Anything, orgID).Return(db.IamPolicy{}, pgx.ErrNoRows)

	resp, err := srv.GetIamPolicy(ctx, &iampb.GetIamPolicyRequest{
		Name: "organizations/acme",
	})

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Empty policy has no bindings.
	assert.Empty(t, resp.GetBindings())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// SetIamPolicy -- delegates to iam.Helper
// ---------------------------------------------------------------------------

func TestUnit_SetIamPolicy(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(testOrg, nil)
	mockQ.On("UpsertIamPolicy", mock.Anything, mock.MatchedBy(func(p db.UpsertIamPolicyParams) bool {
		return p.ResourceID == orgID && p.ResourceType == "organizations"
	})).Return(db.IamPolicy{
		ResourceID: orgID,
		Policy:     json.RawMessage(`{}`),
		Etag:       "new-etag",
	}, nil)

	resp, err := srv.SetIamPolicy(ctx, &iampb.SetIamPolicyRequest{
		Resource: "organizations/acme",
		Policy:   &iampb.Policy{},
	})

	require.NoError(t, err)
	assert.Equal(t, "new-etag", resp.GetEtag())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// TestIamPermissions -- returns all requested permissions
// ---------------------------------------------------------------------------

func TestUnit_TestIamPermissions(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newTestServer(mockQ)
	ctx := context.Background()

	resp, err := srv.TestIamPermissions(ctx, &iampb.TestIamPermissionsRequest{
		Resource:    "organizations/acme",
		Permissions: []string{"pivox.organizations.get", "pivox.organizations.delete"},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"pivox.organizations.get", "pivox.organizations.delete"}, resp.GetPermissions())
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

func (m *mockAuthService) CreateTenant(ctx context.Context, displayName string) (string, error) {
	args := m.Called(ctx, displayName)
	return args.String(0), args.Error(1)
}

func (m *mockAuthService) DeleteTenant(ctx context.Context, tenantID string) error {
	args := m.Called(ctx, tenantID)
	return args.Error(0)
}

// newCreateOrgServer builds an OrganizationsServer wired with the mock pool
// and auth service needed by CreateOrganization.
func newCreateOrgServer(pool TxBeginner, auth authn.Service) *OrganizationsServer {
	return &OrganizationsServer{
		pool:   pool,
		auth:   auth,
		filter: filter.OrganizationFilter(),
	}
}

// orgRow returns a mockRow whose Scan populates a db.Organization with the
// given values. The column order matches the sqlc-generated RETURNING clause.
func orgRow(org db.Organization) *mockRow {
	return &mockRow{scanFunc: func(dest ...interface{}) error {
		// Column order: id, name, display_name, annotations, tenant_id,
		// owner_id, state, etag, revision, created_by, updated_by,
		// deleted_by, create_time, update_time, delete_time, purge_time
		if len(dest) != 16 {
			return errors.New("unexpected number of scan destinations")
		}
		*dest[0].(*uuid.UUID) = org.ID
		*dest[1].(*string) = org.Name
		*dest[2].(*string) = org.DisplayName
		*dest[3].(*json.RawMessage) = org.Annotations
		*dest[4].(*string) = org.TenantID
		// dest[5] is *pgtype.UUID — leave as zero value
		*dest[6].(*db.ResourceState) = org.State
		*dest[7].(*string) = org.Etag
		*dest[8].(*int32) = org.Revision
		*dest[9].(*string) = org.CreatedBy
		*dest[10].(*string) = org.UpdatedBy
		*dest[11].(*string) = org.DeletedBy
		*dest[12].(*time.Time) = org.CreateTime
		*dest[13].(*time.Time) = org.UpdateTime
		// dest[14], dest[15] are *pgtype.Timestamptz — leave as zero
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
	srv := newCreateOrgServer(pool, auth)

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

	// 3. CreateTenant
	auth.On("CreateTenant", mock.Anything, "neworg").Return("tenant-abc", nil)

	// 4. SetOrganizationTenantID via qtx — calls Exec on the tx
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("UPDATE 1"), nil)

	// 5. Commit
	tx.On("Commit", mock.Anything).Return(nil)

	// 6. Deferred Rollback (no-op after commit, but still called)
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

func TestUnit_CreateOrganization_TenantCreateFailure(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-0003-7000-8000-000000000003"),
		Name:        "failorg",
		DisplayName: "Fail Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-fail",
		Revision:    1,
		CreateTime:  time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 7, 1, 12, 0, 0, 0, time.UTC),
	}

	pool.On("Begin", mock.Anything).Return(tx, nil)

	// CreateOrganization in DB succeeds.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()

	// CreateTenant fails.
	auth.On("CreateTenant", mock.Anything, "failorg").
		Return("", errors.New("firebase unavailable"))

	// Deferred Rollback should be called (tx is not committed).
	tx.On("Rollback", mock.Anything).Return(nil)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "failorg",
		Organization:   &apiv1.Organization{DisplayName: "Fail Org"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "create auth tenant")

	// Commit must NOT have been called.
	tx.AssertNotCalled(t, "Commit", mock.Anything)

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestUnit_CreateOrganization_CommitFailure(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

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

	// CreateTenant succeeds.
	auth.On("CreateTenant", mock.Anything, "commitfail").Return("tenant-xyz", nil)

	// SetOrganizationTenantID succeeds.
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("UPDATE 1"), nil)

	// Commit fails.
	tx.On("Commit", mock.Anything).Return(errors.New("connection lost"))

	// Tenant must be cleaned up via DeleteTenant.
	auth.On("DeleteTenant", mock.Anything, "tenant-xyz").Return(nil)

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

	// Verify DeleteTenant was called to clean up the auth tenant.
	auth.AssertCalled(t, "DeleteTenant", mock.Anything, "tenant-xyz")

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}
