package fixtures

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// Smoke tests for the fixture builders. They aren't testing
// production code — they assert the factories themselves return
// what their docstrings claim. Catches regressions if someone
// changes the default values without updating callers.

func TestOrg_Defaults(t *testing.T) {
	o := Org()
	assert.Equal(t, DefaultOrgID, o.ID)
	assert.Equal(t, "acme", o.Name)
	assert.Equal(t, "Acme Corp", o.DisplayName)
	assert.Equal(t, db.ResourceStateACTIVE, o.State)
	assert.Equal(t, "etag-default", o.Etag)
	assert.Equal(t, DefaultTime, o.CreateTime)
}

func TestOrg_OptionsCompose(t *testing.T) {
	customID := uuid.MustParse("00000000-0000-7000-8000-0000000000ff")
	o := Org(
		OrgID(customID),
		OrgName("widgets"),
		OrgState(db.ResourceStateDELETEREQUESTED),
		OrgEtag("etag-custom"),
	)
	assert.Equal(t, customID, o.ID)
	assert.Equal(t, "widgets", o.Name)
	assert.Equal(t, db.ResourceStateDELETEREQUESTED, o.State)
	assert.Equal(t, "etag-custom", o.Etag)
	// Defaults preserved for unspecified fields.
	assert.Equal(t, "Acme Corp", o.DisplayName)
}

func TestOperation_Defaults(t *testing.T) {
	op := Operation()
	assert.Equal(t, DefaultOperationID, op.ID)
	assert.Equal(t, "organizations/acme", op.Parent)
	assert.False(t, op.Done)
	assert.False(t, op.OrgID.Valid)
}

func TestOperation_FailedOption(t *testing.T) {
	op := Operation(OpFailed(5, "not found"))
	assert.True(t, op.Done)
	assert.True(t, op.ErrorCode.Valid)
	assert.Equal(t, int32(5), op.ErrorCode.Int32)
	assert.Equal(t, "not found", op.ErrorMessage.String)
}

func TestStorageGateway_Defaults(t *testing.T) {
	g := StorageGateway()
	assert.Equal(t, DefaultStorageGatewayID, g.ID)
	assert.Equal(t, DefaultOrgID, g.OrgID)
	assert.Equal(t, "gw-1", g.Name)
	assert.Equal(t, db.StorageGatewayStateACTIVE, g.State)
}
