package mocksetup

import (
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ExpectGetOrgByName configures the mock to return `org` when
// GetOrganizationByName is called with the given name. ctx is
// matched with mock.Anything.
func ExpectGetOrgByName(q *mocks.MockQuerier, name string, org db.Organization) {
	q.On("GetOrganizationByName", mock.Anything, name).Return(org, nil)
}

// ExpectGetOrgByNameNotFound configures the mock to return
// pgx.ErrNoRows for the given name — the canonical "org doesn't
// exist" branch that handlers map to NotFound.
func ExpectGetOrgByNameNotFound(q *mocks.MockQuerier, name string) {
	q.On("GetOrganizationByName", mock.Anything, name).Return(db.Organization{}, pgx.ErrNoRows)
}

// ExpectGetOrgByNameError configures the mock to return an arbitrary
// error for the given name. Useful for testing handler behavior on
// non-NoRows DB failures (connection drop, timeout, etc.).
func ExpectGetOrgByNameError(q *mocks.MockQuerier, name string, err error) {
	q.On("GetOrganizationByName", mock.Anything, name).Return(db.Organization{}, err)
}
