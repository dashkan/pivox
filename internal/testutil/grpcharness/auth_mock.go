package grpcharness

import (
	"context"
	"testing"

	"github.com/dashkan/pivox/internal/authn"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/authnmock"
)

// MockedFirebaseAuth is a partial mock of authn.Service for tests
// that want to assert Firebase provider calls (Create/Update/Delete
// OidcProvider, etc.) without losing the harness's real
// VerifyToken behavior.
//
// Why partial: every gRPC call goes through the auth interceptor,
// which calls VerifyToken. Mocking VerifyToken in every test that
// only cares about Firebase provider calls is noise. This wrapper
// passes VerifyToken through to the same query-backed lookup the
// default testAuthService uses, while routing Firebase methods to
// the embedded mock so tests can assert on them.
//
// Construction is two-step because the harness owns Queries:
//
//	auth := grpcharness.NewMockedFirebaseAuth(t)
//	h := grpcharness.New(t,
//	    grpcharness.WithAuth(auth),
//	    grpcharness.WithOrganizationsServer())
//	auth.SetQueries(h.Queries)
//
//	auth.Mock.EXPECT().
//	    CreateOidcProvider(mock.Anything, mock.MatchedBy(...)).
//	    Return(nil)
//
// AssertExpectations is auto-registered in t.Cleanup by
// authnmock.NewMockService — tests don't need to call it manually.
type MockedFirebaseAuth struct {
	// Mock is the testify-generated mock for Firebase provider
	// methods (and everything else, but tests should only set
	// expectations on the Firebase methods).
	Mock *authnmock.MockService

	// queries backs the VerifyToken passthrough. Set via
	// SetQueries after the harness is constructed.
	queries db.Querier
}

// NewMockedFirebaseAuth constructs a MockedFirebaseAuth ready to
// pass to grpcharness.WithAuth. The caller must call SetQueries
// after the harness is constructed so VerifyToken can resolve
// identities.
func NewMockedFirebaseAuth(t testing.TB) *MockedFirebaseAuth {
	return &MockedFirebaseAuth{
		Mock: authnmock.NewMockService(t),
	}
}

// SetQueries wires the harness's Queries into the auth wrapper so
// VerifyToken can look up identities. Called after grpcharness.New
// returns (which is when h.Queries becomes available).
func (m *MockedFirebaseAuth) SetQueries(q db.Querier) {
	m.queries = q
}

// VerifyToken delegates to the queries-backed identity lookup —
// same behavior as testAuthService.VerifyToken so the harness's
// caller flow continues to work without per-test wiring.
func (m *MockedFirebaseAuth) VerifyToken(ctx context.Context, token string) (*authn.Identity, error) {
	id := &authn.Identity{UID: token}
	if m.queries != nil {
		if row, err := m.queries.GetIdentityByFirebaseUID(ctx, token); err == nil {
			id.Email = row.Email
			id.Claims = map[string]any{"pivox_user_id": row.ID.String()}
		}
	}
	return id, nil
}

// The remaining authn.Service methods forward to the embedded
// mock. Tests set expectations via m.Mock.EXPECT().<Method>(...).

func (m *MockedFirebaseAuth) CreateCustomToken(ctx context.Context, uid string) (string, error) {
	return m.Mock.CreateCustomToken(ctx, uid)
}

func (m *MockedFirebaseAuth) DeleteUser(ctx context.Context, uid string) error {
	return m.Mock.DeleteUser(ctx, uid)
}

func (m *MockedFirebaseAuth) CreateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	return m.Mock.CreateOidcProvider(ctx, cfg)
}

func (m *MockedFirebaseAuth) UpdateOidcProvider(ctx context.Context, cfg authn.OidcProviderConfig) error {
	return m.Mock.UpdateOidcProvider(ctx, cfg)
}

func (m *MockedFirebaseAuth) DeleteOidcProvider(ctx context.Context, providerID string) error {
	return m.Mock.DeleteOidcProvider(ctx, providerID)
}

func (m *MockedFirebaseAuth) CreateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	return m.Mock.CreateSamlProvider(ctx, cfg)
}

func (m *MockedFirebaseAuth) UpdateSamlProvider(ctx context.Context, cfg authn.SamlProviderConfig) error {
	return m.Mock.UpdateSamlProvider(ctx, cfg)
}

func (m *MockedFirebaseAuth) DeleteSamlProvider(ctx context.Context, providerID string) error {
	return m.Mock.DeleteSamlProvider(ctx, providerID)
}

// Compile-time check: MockedFirebaseAuth implements authn.Service.
var _ authn.Service = (*MockedFirebaseAuth)(nil)
