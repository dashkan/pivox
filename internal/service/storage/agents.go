package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
)

type AgentsServer struct {
	storagev1.UnimplementedAgentsServer
	queries *db.Queries
}

func NewAgentsServer(queries *db.Queries) *AgentsServer {
	return &AgentsServer{
		queries: queries,
	}
}

// parseAgentName parses "organizations/{org}/storageGateways/{gw}/agents/{agent}"
// and returns (orgName, gwName, agentID).
func parseAgentName(name string) (string, string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "storageGateways" || parts[4] != "agents" {
		return "", "", "", fmt.Errorf("invalid agent name %q: expected organizations/*/storageGateways/*/agents/*", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// parseGatewayParent parses "organizations/{org}/storageGateways/{gw}"
// and returns (orgName, gwName).
func parseGatewayParent(parent string) (string, string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "storageGateways" {
		return "", "", fmt.Errorf("invalid parent %q: expected organizations/*/storageGateways/*", parent)
	}
	return parts[1], parts[3], nil
}

// resolveGateway looks up a storage gateway by org name and gateway name, returning the gateway.
func (s *AgentsServer) resolveGateway(ctx context.Context, orgName, gwName string) (db.StorageGateway, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return db.StorageGateway{}, apierr.HandleResourceError(err, "Organization", orgName)
	}
	gw, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return db.StorageGateway{}, apierr.HandleResourceError(err, "StorageGateway", fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName))
	}
	return gw, nil
}

func (s *AgentsServer) GetAgent(ctx context.Context, req *storagev1.GetAgentRequest) (*storagev1.Agent, error) {
	orgName, gwName, agentIDStr, err := parseAgentName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", fmt.Sprintf("invalid agent ID %q", agentIDStr)))
	}

	agent, err := s.queries.GetStorageAgent(ctx, agentID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Agent", req.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return convert.AgentToProto(agent, gatewayName), nil
}

func (s *AgentsServer) ListAgents(ctx context.Context, req *storagev1.ListAgentsRequest) (*storagev1.ListAgentsResponse, error) {
	orgName, gwName, err := parseGatewayParent(req.GetParent())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}

	gw, err := s.resolveGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	agents, err := s.queries.ListStorageAgentsByGateway(ctx, gw.ID)
	if err != nil {
		return nil, apierr.Internal("failed to list agents")
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	pbAgents := make([]*storagev1.Agent, 0, len(agents))
	for _, a := range agents {
		pbAgents = append(pbAgents, convert.AgentToProto(a, gatewayName))
	}

	return &storagev1.ListAgentsResponse{
		Agents: pbAgents,
	}, nil
}

func (s *AgentsServer) DrainAgent(ctx context.Context, req *storagev1.DrainAgentRequest) (*storagev1.Agent, error) {
	orgName, gwName, agentIDStr, err := parseAgentName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", fmt.Sprintf("invalid agent ID %q", agentIDStr)))
	}

	agent, err := s.queries.UpdateStorageAgentState(ctx, db.UpdateStorageAgentStateParams{
		ID:    agentID,
		State: db.AgentStateDRAINING,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Agent", req.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return convert.AgentToProto(agent, gatewayName), nil
}

func (s *AgentsServer) RemoveAgent(ctx context.Context, req *storagev1.RemoveAgentRequest) (*storagev1.Agent, error) {
	_, _, agentIDStr, err := parseAgentName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}
	agentID, err := uuid.Parse(agentIDStr)
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", fmt.Sprintf("invalid agent ID %q", agentIDStr)))
	}

	err = s.queries.DeleteStorageAgent(ctx, agentID)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Agent", req.GetName())
	}

	return &storagev1.Agent{
		Name: req.GetName(),
	}, nil
}
