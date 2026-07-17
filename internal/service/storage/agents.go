package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/appkey"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/filter"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
)

type AgentsServer struct {
	storagev1.UnimplementedAgentsServer
	pool    db.RWPool
	queries db.Querier
	codec   *appkey.Codec
}

// AgentsConfig is the constructor input for AgentsServer.
type AgentsConfig struct {
	// Pool is the database pool. Used by ListAgents for the dynamic
	// (filter + order_by + keyset) SELECT. *pgxpool.Pool satisfies it. Required.
	Pool db.RWPool
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Codec opaque-encodes keyset page tokens for ListAgents. Required.
	Codec *appkey.Codec
}

// NewAgentsServer constructs the server from cfg. Panics on a missing
// required field.
func NewAgentsServer(cfg AgentsConfig) *AgentsServer {
	if cfg.Pool == nil {
		panic("storage: AgentsConfig.Pool is required")
	}
	if cfg.Queries == nil {
		panic("storage: AgentsConfig.Queries is required")
	}
	if cfg.Codec == nil {
		panic("storage: AgentsConfig.Codec is required")
	}
	return &AgentsServer{
		pool:    cfg.Pool,
		queries: cfg.Queries,
		codec:   cfg.Codec,
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

// ListAgents is a dynamic AIP-160 filtered + AIP-132 sorted + compound-cursor
// keyset list. The parent storage gateway (resolved via org + gateway lookup,
// membership-gated by the interceptor) is the NON-NEGOTIABLE base scope
// (gateway_id = $), ANDed as the base of the query; the request's
// filter/order_by layer ON TOP of it and can only narrow, never widen. Every
// value is bound as a $N parameter by filter.BuildListQuery; column/direction
// come only from AgentFilter's whitelist. Agents carry no audit columns, so no
// Actor resolution runs.
func (s *AgentsServer) ListAgents(ctx context.Context, req *storagev1.ListAgentsRequest) (*storagev1.ListAgentsResponse, error) {
	orgName, gwName, err := parseGatewayParent(req.GetParent())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}

	gw, err := s.resolveGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	rf := filter.AgentFilter()
	pageSize := filter.ClampPageSize(rf, req.GetPageSize())

	plan, err := filter.PlanOrderBy(rf, req.GetOrderBy())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("order_by", err.Error()))
	}
	cursor, err := filter.DecodeCursor(s.codec, plan, req.GetPageToken())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("page_token", "invalid or malformed"))
	}

	sql, args, err := filter.BuildListQuery(filter.ListQuery{
		Resource: rf,
		Base:     []filter.Predicate{{SQL: "gateway_id = %s", Arg: gw.ID}},
		Filter:   req.GetFilter(),
		Order:    plan,
		PageSize: pageSize,
		Cursor:   cursor,
	})
	if err != nil {
		// The only error source is the filter transpiler (bad user filter).
		return nil, apierr.InvalidArgument(apierr.FieldViolation("filter", err.Error()))
	}

	pgxRows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, apierr.Internal(err, "list agents")
	}
	agents, err := filter.ScanStorageAgents(pgxRows)
	if err != nil {
		return nil, apierr.Internal(err, "list agents")
	}

	agents, nextPageToken, err := filter.Paginate(agents, int(pageSize), func(last db.StorageAgent) (string, error) {
		return filter.EncodeCursor(s.codec, plan, agentSortValue(plan, last), last.ID)
	})
	if err != nil {
		return nil, apierr.Internal(err, "encode page token")
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	pbAgents := make([]*storagev1.Agent, 0, len(agents))
	for _, a := range agents {
		pbAgents = append(pbAgents, convert.AgentToProto(a, gatewayName))
	}

	return &storagev1.ListAgentsResponse{
		Agents:        pbAgents,
		NextPageToken: nextPageToken,
	}, nil
}

// agentSortValue renders the primary order_by column's value for the given row
// as the string the compound page token carries (timestamps as RFC3339Nano).
// For the id-only default it is unused, so "" is returned.
func agentSortValue(plan filter.OrderByPlan, a db.StorageAgent) string {
	switch plan.Field {
	case "hostname":
		return a.Hostname
	case "joinTime":
		return a.JoinTime.UTC().Format(time.RFC3339Nano)
	case "lastSeenTime":
		return a.LastSeenTime.UTC().Format(time.RFC3339Nano)
	default:
		return ""
	}
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
