package mocksetup

import (
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/mock"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// ExpectGetStorageGatewayByName configures the mock to return `gw`
// for GetStorageGatewayByName with the given params (org_id + name).
func ExpectGetStorageGatewayByName(q *mocks.MockQuerier, params db.GetStorageGatewayByNameParams, gw db.StorageGateway) {
	q.On("GetStorageGatewayByName", mock.Anything, params).Return(gw, nil)
}

// ExpectGetStorageGatewayByNameNotFound configures the mock to
// return pgx.ErrNoRows for the given params — the canonical "gateway
// doesn't exist" branch.
func ExpectGetStorageGatewayByNameNotFound(q *mocks.MockQuerier, params db.GetStorageGatewayByNameParams) {
	q.On("GetStorageGatewayByName", mock.Anything, params).Return(db.StorageGateway{}, pgx.ErrNoRows)
}
