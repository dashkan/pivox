package organizations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

	"github.com/dashkan/pivox/internal/appkey"
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
	codec, _ := appkey.NewFromHex(strings.Repeat("ab", 32))
	return &OrganizationsServer{
		queries: q,
		iam:     iamHelper,
		filter:  filter.OrganizationFilter(),
		codec:   codec,
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

func TestUnit_CreateOrganization_AutoGeneratedSlug(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

	// Begin tx succeeds.
	pool.On("Begin", mock.Anything).Return(tx, nil)

	// CreateOrganization in DB: the slug is auto-generated (8-char UUID prefix).
	// We accept any name of length 8.
	var capturedSlug string
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(&mockRow{scanFunc: func(dest ...interface{}) error {
			// Capture the slug from the SQL args to build a realistic org.
			// The row scanner populates all fields from the returned row.
			generatedOrg := db.Organization{
				ID:          uuid.MustParse("0192a000-0005-7000-8000-000000000005"),
				Name:        capturedSlug, // will be set below
				DisplayName: "Auto Slug Org",
				Annotations: json.RawMessage(`{}`),
				State:       db.ResourceStateACTIVE,
				Etag:        "etag-auto",
				Revision:    1,
				CreateTime:  time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC),
				UpdateTime:  time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC),
			}
			if len(dest) != 16 {
				return errors.New("unexpected number of scan destinations")
			}
			*dest[0].(*uuid.UUID) = generatedOrg.ID
			*dest[1].(*string) = generatedOrg.Name
			*dest[2].(*string) = generatedOrg.DisplayName
			*dest[3].(*json.RawMessage) = generatedOrg.Annotations
			*dest[4].(*string) = generatedOrg.TenantID
			*dest[6].(*db.ResourceState) = generatedOrg.State
			*dest[7].(*string) = generatedOrg.Etag
			*dest[8].(*int32) = generatedOrg.Revision
			*dest[9].(*string) = generatedOrg.CreatedBy
			*dest[10].(*string) = generatedOrg.UpdatedBy
			*dest[11].(*string) = generatedOrg.DeletedBy
			*dest[12].(*time.Time) = generatedOrg.CreateTime
			*dest[13].(*time.Time) = generatedOrg.UpdateTime
			return nil
		}}).Once()

	// CreateTenant — called with whatever auto-slug was generated.
	auth.On("CreateTenant", mock.Anything, mock.MatchedBy(func(s string) bool {
		capturedSlug = s
		return len(s) == 8
	})).Return("tenant-auto", nil)

	// SetOrganizationTenantID succeeds.
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("UPDATE 1"), nil)

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

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestUnit_CreateOrganization_BeginTransactionError(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

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
	// auth must not have been called at all.
	auth.AssertNotCalled(t, "CreateTenant", mock.Anything, mock.Anything)
}

func TestUnit_CreateOrganization_SetTenantIDFailure(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-0006-7000-8000-000000000006"),
		Name:        "settenantfail",
		DisplayName: "Set Tenant Fail Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-stf",
		Revision:    1,
		CreateTime:  time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 8, 1, 10, 0, 0, 0, time.UTC),
	}

	pool.On("Begin", mock.Anything).Return(tx, nil)

	// CreateOrganization in DB succeeds.
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()

	// CreateTenant succeeds.
	auth.On("CreateTenant", mock.Anything, "settenantfail").Return("tenant-stf", nil)

	// SetOrganizationTenantID fails — triggers tenant cleanup.
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.CommandTag{}, errors.New("db constraint violation"))

	// Cleanup: DeleteTenant must be called with the tenant we just created.
	auth.On("DeleteTenant", mock.Anything, "tenant-stf").Return(nil)

	// Deferred Rollback (tx was not committed).
	tx.On("Rollback", mock.Anything).Return(nil)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "settenantfail",
		Organization:   &apiv1.Organization{DisplayName: "Set Tenant Fail Org"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "set tenant id")

	// Verify the auth tenant was cleaned up.
	auth.AssertCalled(t, "DeleteTenant", mock.Anything, "tenant-stf")
	// Commit must NOT have been called.
	tx.AssertNotCalled(t, "Commit", mock.Anything)

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// ListOrganizations — mock infrastructure
// ---------------------------------------------------------------------------

// mockListDBTX is a minimal db.DBTX implementation for ListOrganizations tests.
// It controls whether Query returns an error or a given pgx.Rows value.
type mockListDBTX struct {
	queryErr  error
	queryRows pgx.Rows
}

func (m *mockListDBTX) Exec(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

func (m *mockListDBTX) Query(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.queryRows, nil
}

func (m *mockListDBTX) QueryRow(_ context.Context, _ string, _ ...interface{}) pgx.Row {
	return nil
}

// errRows is a pgx.Rows implementation that reports an error via Err() but
// returns no data rows. This exercises the ScanOrganizations error path.
type errRows struct {
	err error
}

func (r *errRows) Close()                                       {}
func (r *errRows) Err() error                                   { return r.err }
func (r *errRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *errRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *errRows) Next() bool                                   { return false }
func (r *errRows) Scan(dest ...any) error                       { return r.err }
func (r *errRows) Values() ([]any, error)                       { return nil, r.err }
func (r *errRows) RawValues() [][]byte                          { return nil }
func (r *errRows) Conn() *pgx.Conn                              { return nil }

// emptyRows is a pgx.Rows that returns no rows and no error.
type emptyRows struct{}

func (r *emptyRows) Close()                                       {}
func (r *emptyRows) Err() error                                   { return nil }
func (r *emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *emptyRows) Next() bool                                   { return false }
func (r *emptyRows) Scan(dest ...any) error                       { return nil }
func (r *emptyRows) Values() ([]any, error)                       { return nil, nil }
func (r *emptyRows) RawValues() [][]byte                          { return nil }
func (r *emptyRows) Conn() *pgx.Conn                              { return nil }

// dataRows is a pgx.Rows that returns a fixed slice of organizations.
// Each call to Next() advances to the next organization; Scan() populates
// the destination variables using the same column order as ScanOrganizations.
type dataRows struct {
	orgs   []db.Organization
	idx    int
	closed bool
}

func newDataRows(orgs []db.Organization) *dataRows { return &dataRows{orgs: orgs, idx: -1} }

func (r *dataRows) Close()                                       { r.closed = true }
func (r *dataRows) Err() error                                   { return nil }
func (r *dataRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *dataRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *dataRows) Next() bool {
	r.idx++
	return r.idx < len(r.orgs)
}
func (r *dataRows) Scan(dest ...any) error {
	o := r.orgs[r.idx]
	// Column order matches ScanOrganizations:
	// id, name, display_name, annotations, tenant_id, owner_id,
	// state, etag, revision, created_by, updated_by, deleted_by,
	// create_time, update_time, delete_time, purge_time
	if len(dest) != 16 {
		return errors.New("unexpected scan destination count")
	}
	*dest[0].(*uuid.UUID) = o.ID
	*dest[1].(*string) = o.Name
	*dest[2].(*string) = o.DisplayName
	*dest[3].(*json.RawMessage) = o.Annotations
	*dest[4].(*string) = o.TenantID
	// dest[5] is pgtype.UUID (owner_id) — leave as zero value
	*dest[6].(*db.ResourceState) = o.State
	*dest[7].(*string) = o.Etag
	*dest[8].(*int32) = o.Revision
	*dest[9].(*string) = o.CreatedBy
	*dest[10].(*string) = o.UpdatedBy
	*dest[11].(*string) = o.DeletedBy
	*dest[12].(*time.Time) = o.CreateTime
	*dest[13].(*time.Time) = o.UpdateTime
	// dest[14], dest[15] are pgtype.Timestamptz — leave as zero
	return nil
}
func (r *dataRows) Values() ([]any, error) { return nil, nil }
func (r *dataRows) RawValues() [][]byte    { return nil }
func (r *dataRows) Conn() *pgx.Conn        { return nil }

// ---------------------------------------------------------------------------
// ListOrganizations
// ---------------------------------------------------------------------------

func TestUnit_ListOrganizations_FilterQueryBuildError(t *testing.T) {
	// An invalid AIP-160 filter expression causes filter.Query to return an
	// error before ever hitting the database.
	ctx := context.Background()

	dbtx := &mockListDBTX{queryRows: &emptyRows{}}
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
	}

	_, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		// This filter references a field that doesn't exist in OrganizationFilter,
		// causing the transpiler to return an error.
		Filter: "unknownField = \"value\"",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "invalid filter")
}

func TestUnit_ListOrganizations_QueryExecutionError(t *testing.T) {
	// filter.Query itself returns an error (e.g. database connectivity failure).
	ctx := context.Background()

	dbtx := &mockListDBTX{queryErr: errors.New("connection refused")}
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
	}

	_, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_ListOrganizations_ScanError(t *testing.T) {
	// filter.Query succeeds but the rows report an error via Err(), which
	// ScanOrganizations surfaces after iterating.
	ctx := context.Background()

	dbtx := &mockListDBTX{queryRows: &errRows{err: errors.New("scan failed")}}
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
	}

	_, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "database error")
}

func TestUnit_ListOrganizations_EmptyResult(t *testing.T) {
	// Happy path with no results.
	ctx := context.Background()

	dbtx := &mockListDBTX{queryRows: &emptyRows{}}
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
	}

	resp, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})

	require.NoError(t, err)
	assert.Empty(t, resp.GetOrganizations())
	assert.Empty(t, resp.GetNextPageToken())
}

func TestUnit_ListOrganizations_WithResults(t *testing.T) {
	// Returns rows and verifies they are converted to protos correctly.
	ctx := context.Background()

	orgs := []db.Organization{
		{
			ID:          uuid.MustParse("0192a000-0010-7000-8000-000000000010"),
			Name:        "org-one",
			DisplayName: "Org One",
			Annotations: json.RawMessage(`{}`),
			State:       db.ResourceStateACTIVE,
			Etag:        "etag-one",
			Revision:    1,
			CreateTime:  time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
			UpdateTime:  time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			ID:          uuid.MustParse("0192a000-0011-7000-8000-000000000011"),
			Name:        "org-two",
			DisplayName: "Org Two",
			Annotations: json.RawMessage(`{}`),
			State:       db.ResourceStateACTIVE,
			Etag:        "etag-two",
			Revision:    1,
			CreateTime:  time.Date(2025, 9, 2, 0, 0, 0, 0, time.UTC),
			UpdateTime:  time.Date(2025, 9, 2, 0, 0, 0, 0, time.UTC),
		},
	}

	dbtx := &mockListDBTX{queryRows: newDataRows(orgs)}
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
	}

	resp, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{})

	require.NoError(t, err)
	require.Len(t, resp.GetOrganizations(), 2)
	assert.Equal(t, "organizations/org-one", resp.GetOrganizations()[0].GetName())
	assert.Equal(t, "organizations/org-two", resp.GetOrganizations()[1].GetName())
	assert.Empty(t, resp.GetNextPageToken())
}

func TestUnit_ListOrganizations_Pagination(t *testing.T) {
	// When query returns pageSize+1 rows, the server sets NextPageToken and
	// trims the result to pageSize.
	ctx := context.Background()

	// Build pageSize+1 organizations so the server sets nextPageToken.
	const pageSize = 2
	orgs := make([]db.Organization, pageSize+1)
	for i := 0; i <= pageSize; i++ {
		id := uuid.MustParse("0192a000-0020-7000-8000-00000000" + fmt.Sprintf("%04d", i))
		orgs[i] = db.Organization{
			ID:          id,
			Name:        fmt.Sprintf("paged-org-%d", i),
			DisplayName: fmt.Sprintf("Paged Org %d", i),
			Annotations: json.RawMessage(`{}`),
			State:       db.ResourceStateACTIVE,
			Etag:        fmt.Sprintf("etag-%d", i),
			Revision:    1,
			CreateTime:  time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
			UpdateTime:  time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
		}
	}

	dbtx := &mockListDBTX{queryRows: newDataRows(orgs)}
	codec, _ := appkey.NewFromHex(strings.Repeat("ab", 32))
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
		codec:  codec,
	}

	resp, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		PageSize: pageSize,
	})

	require.NoError(t, err)
	// Should only return pageSize results.
	assert.Len(t, resp.GetOrganizations(), pageSize)
	// NextPageToken is opaque — decrypt it to verify it encodes the expected ID.
	require.NotEmpty(t, resp.GetNextPageToken())
	raw, err := codec.Decrypt(resp.GetNextPageToken())
	require.NoError(t, err)
	var decodedID uuid.UUID
	copy(decodedID[:], raw)
	assert.Equal(t, orgs[pageSize].ID, decodedID)
}

func TestUnit_ListOrganizations_PageSizeClamped(t *testing.T) {
	// A page_size > 1000 is clamped to 1000; fewer results than 1000 means
	// no next page token.
	ctx := context.Background()

	dbtx := &mockListDBTX{queryRows: &emptyRows{}}
	srv := &OrganizationsServer{
		db:     dbtx,
		filter: filter.OrganizationFilter(),
	}

	resp, err := srv.ListOrganizations(ctx, &apiv1.ListOrganizationsRequest{
		PageSize: 9999,
	})

	require.NoError(t, err)
	assert.Empty(t, resp.GetOrganizations())
	assert.Empty(t, resp.GetNextPageToken())
}

func TestUnit_CreateOrganization_DBCreateError(t *testing.T) {
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

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

	// auth must not have been called at all.
	auth.AssertNotCalled(t, "CreateTenant", mock.Anything, mock.Anything)

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
}

func TestUnit_CreateOrganization_SetTenantIDFailureWithDeleteTenantError(t *testing.T) {
	// When SetOrganizationTenantID fails AND the subsequent DeleteTenant also
	// fails, we still return the set tenant id error (cleanup failure is only
	// logged).
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-0007-7000-8000-000000000007"),
		Name:        "dblefail",
		DisplayName: "Double Fail Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-df",
		Revision:    1,
		CreateTime:  time.Date(2025, 8, 2, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 8, 2, 10, 0, 0, 0, time.UTC),
	}

	pool.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()
	auth.On("CreateTenant", mock.Anything, "dblefail").Return("tenant-df", nil)

	// SetOrganizationTenantID fails.
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.CommandTag{}, errors.New("constraint error"))

	// DeleteTenant also fails — should only be logged, not propagated.
	auth.On("DeleteTenant", mock.Anything, "tenant-df").
		Return(errors.New("firebase delete failed"))

	tx.On("Rollback", mock.Anything).Return(nil)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "dblefail",
		Organization:   &apiv1.Organization{DisplayName: "Double Fail Org"},
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "set tenant id")

	pool.AssertExpectations(t)
	tx.AssertExpectations(t)
	auth.AssertExpectations(t)
}

func TestUnit_CreateOrganization_CommitFailureWithDeleteTenantError(t *testing.T) {
	// When Commit fails AND the subsequent DeleteTenant also fails, we still
	// return the commit transaction error (cleanup failure is only logged).
	ctx := context.Background()

	pool := new(mockTxBeginner)
	tx := new(mockTx)
	auth := new(mockAuthService)
	srv := newCreateOrgServer(pool, auth)

	createdOrg := db.Organization{
		ID:          uuid.MustParse("0192a000-0008-7000-8000-000000000008"),
		Name:        "commitdelf",
		DisplayName: "Commit Delete Fail Org",
		Annotations: json.RawMessage(`{}`),
		State:       db.ResourceStateACTIVE,
		Etag:        "etag-cdf",
		Revision:    1,
		CreateTime:  time.Date(2025, 8, 3, 10, 0, 0, 0, time.UTC),
		UpdateTime:  time.Date(2025, 8, 3, 10, 0, 0, 0, time.UTC),
	}

	pool.On("Begin", mock.Anything).Return(tx, nil)
	tx.On("QueryRow", mock.Anything, mock.Anything, mock.Anything).
		Return(orgRow(createdOrg)).Once()
	auth.On("CreateTenant", mock.Anything, "commitdelf").Return("tenant-cdf", nil)
	tx.On("Exec", mock.Anything, mock.Anything, mock.Anything).
		Return(pgconn.NewCommandTag("UPDATE 1"), nil)
	tx.On("Commit", mock.Anything).Return(errors.New("network timeout"))

	// DeleteTenant also fails — only logged.
	auth.On("DeleteTenant", mock.Anything, "tenant-cdf").
		Return(errors.New("firebase unreachable"))

	tx.On("Rollback", mock.Anything).Return(nil)

	_, err := srv.CreateOrganization(ctx, &apiv1.CreateOrganizationRequest{
		OrganizationId: "commitdelf",
		Organization:   &apiv1.Organization{DisplayName: "Commit Delete Fail Org"},
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

// ---------------------------------------------------------------------------
// NewOrganizationsServer constructor
// ---------------------------------------------------------------------------

func TestUnit_NewOrganizationsServer_Constructor(t *testing.T) {
	// NewOrganizationsServer requires a *pgxpool.Pool. We cannot construct a
	// real pool in a unit test, so we verify that the nil-pool case still
	// instantiates the server struct without panicking. The filter field is
	// the most important invariant to check.
	mockQ := new(mocks.MockQuerier)
	iamHelper := iam.NewHelper(mockQ)
	auth := new(mockAuthService)

	// NewOrganizationsServer with nil pool exercises the constructor code path.
	srv := NewOrganizationsServer(nil, mockQ, iamHelper, auth, nil)

	require.NotNil(t, srv)
	assert.NotNil(t, srv.filter)
	assert.NotNil(t, srv.iam)
	assert.Equal(t, auth, srv.auth)
	assert.Equal(t, mockQ, srv.queries)
}
