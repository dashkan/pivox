package storage

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	db "github.com/dashkan/pivox/internal/db/generated"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

var (
	agentID   = uuid.MustParse("0192a000-0020-7000-8000-000000000001")
	testAgent = db.StorageAgent{
		ID:           agentID,
		GatewayID:    gwID,
		IpAddress:    "10.0.0.5",
		Hostname:     "agent-host-1",
		Version:      "1.1.0",
		State:        db.AgentStateCONNECTED,
		JoinTime:     time.Date(2025, 6, 15, 10, 0, 0, 0, time.UTC),
		LastSeenTime: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
	}
)

func newAgentsServer(q *mocks.MockQuerier) *AgentsServer {
	return NewAgentsServer(q)
}

// ---------------------------------------------------------------------------
// GetAgent
// ---------------------------------------------------------------------------

func TestUnit_GetAgent_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newAgentsServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetStorageAgent", mock.Anything, agentID).Return(testAgent, nil)

	resp, err := srv.GetAgent(ctx, &storagev1.GetAgentRequest{
		Name: "organizations/acme/storageGateways/gw-1/agents/" + agentID.String(),
	})

	require.NoError(t, err)
	assert.Equal(t, "organizations/acme/storageGateways/gw-1/agents/"+agentID.String(), resp.GetName())
	assert.Equal(t, "10.0.0.5", resp.GetIpAddress())
	assert.Equal(t, "agent-host-1", resp.GetHostname())
	assert.Equal(t, "1.1.0", resp.GetVersion())
	assert.Equal(t, storagev1.Agent_CONNECTED, resp.GetState())
	mockQ.AssertExpectations(t)
}

func TestUnit_GetAgent_InvalidName(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newAgentsServer(mockQ)
	ctx := context.Background()

	_, err := srv.GetAgent(ctx, &storagev1.GetAgentRequest{
		Name: "bad/format",
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
}

func TestUnit_GetAgent_NotFound(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newAgentsServer(mockQ)
	ctx := context.Background()

	missing := uuid.MustParse("0192a000-9999-7000-8000-000000000001")
	mockQ.On("GetStorageAgent", mock.Anything, missing).Return(db.StorageAgent{}, pgx.ErrNoRows)

	_, err := srv.GetAgent(ctx, &storagev1.GetAgentRequest{
		Name: "organizations/acme/storageGateways/gw-1/agents/" + missing.String(),
	})

	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// ListAgents
// ---------------------------------------------------------------------------

func TestUnit_ListAgents_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newAgentsServer(mockQ)
	ctx := context.Background()

	mockQ.On("GetOrganizationByName", mock.Anything, "acme").Return(gwOrg, nil)
	mockQ.On("GetStorageGatewayByName", mock.Anything, db.GetStorageGatewayByNameParams{
		OrgID: gwOrgID,
		Name:  "gw-1",
	}).Return(testGateway, nil)
	mockQ.On("ListStorageAgentsByGateway", mock.Anything, gwID).Return([]db.StorageAgent{testAgent}, nil)

	resp, err := srv.ListAgents(ctx, &storagev1.ListAgentsRequest{
		Parent: "organizations/acme/storageGateways/gw-1",
	})

	require.NoError(t, err)
	require.Len(t, resp.GetAgents(), 1)
	assert.Contains(t, resp.GetAgents()[0].GetName(), agentID.String())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// DrainAgent
// ---------------------------------------------------------------------------

func TestUnit_DrainAgent_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newAgentsServer(mockQ)
	ctx := context.Background()

	drainedAgent := testAgent
	drainedAgent.State = db.AgentStateDRAINING

	mockQ.On("UpdateStorageAgentState", mock.Anything, db.UpdateStorageAgentStateParams{
		ID:    agentID,
		State: db.AgentStateDRAINING,
	}).Return(drainedAgent, nil)

	resp, err := srv.DrainAgent(ctx, &storagev1.DrainAgentRequest{
		Name: "organizations/acme/storageGateways/gw-1/agents/" + agentID.String(),
	})

	require.NoError(t, err)
	assert.Equal(t, storagev1.Agent_DRAINING, resp.GetState())
	mockQ.AssertExpectations(t)
}

// ---------------------------------------------------------------------------
// RemoveAgent
// ---------------------------------------------------------------------------

func TestUnit_RemoveAgent_Success(t *testing.T) {
	mockQ := new(mocks.MockQuerier)
	srv := newAgentsServer(mockQ)
	ctx := context.Background()

	agentName := "organizations/acme/storageGateways/gw-1/agents/" + agentID.String()
	mockQ.On("DeleteStorageAgent", mock.Anything, agentID).Return(nil)

	resp, err := srv.RemoveAgent(ctx, &storagev1.RemoveAgentRequest{
		Name: agentName,
	})

	require.NoError(t, err)
	assert.Equal(t, agentName, resp.GetName())
	mockQ.AssertExpectations(t)
}
