package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	db "github.com/dashkan/pivox-server/internal/db/generated"
	agentv1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/agent/v1"
)

// AgentServiceServer implements the bidirectional streaming AgentService for
// storage gateway agents connecting to the control plane.
type AgentServiceServer struct {
	agentv1.UnimplementedAgentServiceServer
	pool    *pgxpool.Pool
	queries *db.Queries
	logger  *slog.Logger
}

// NewAgentServiceServer creates a new AgentServiceServer.
func NewAgentServiceServer(pool *pgxpool.Pool, queries *db.Queries, logger *slog.Logger) *AgentServiceServer {
	return &AgentServiceServer{
		pool:    pool,
		queries: queries,
		logger:  logger,
	}
}

// Connect implements the bidirectional streaming RPC. The agent sends a
// Handshake as the first message, then continuously sends heartbeats, health
// checks, and telemetry. The server responds with a HandshakeAck containing
// initial configuration, and may push config updates or lifecycle commands.
func (s *AgentServiceServer) Connect(stream agentv1.AgentService_ConnectServer) error {
	ctx := stream.Context()

	// -----------------------------------------------------------------------
	// 1. Wait for the first message -- must be a Handshake.
	// -----------------------------------------------------------------------
	firstMsg, err := stream.Recv()
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to receive first message", "error", err)
		return status.Error(codes.Internal, "failed to receive first message")
	}

	hs := firstMsg.GetHandshake()
	if hs == nil {
		return status.Error(codes.InvalidArgument, "first message must be handshake")
	}

	// -----------------------------------------------------------------------
	// 2. Validate registration_token against DB.
	// -----------------------------------------------------------------------
	gateway, err := s.queries.GetStorageGatewayByToken(ctx, hs.GetRegistrationToken())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return status.Error(codes.Unauthenticated, "invalid registration token")
		}
		s.logger.ErrorContext(ctx, "failed to look up gateway by token", "error", err)
		return status.Error(codes.Internal, "failed to validate registration token")
	}

	// -----------------------------------------------------------------------
	// 3. Create or update agent record.
	// -----------------------------------------------------------------------
	var agent db.StorageAgent

	existing, lookupErr := s.queries.GetStorageAgentByGatewayAndIP(ctx, db.GetStorageAgentByGatewayAndIPParams{
		GatewayID: gateway.ID,
		IpAddress: hs.GetIpAddress(),
	})
	if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
		s.logger.ErrorContext(ctx, "failed to look up existing agent", "error", lookupErr)
		return status.Error(codes.Internal, "failed to look up agent")
	}

	if errors.Is(lookupErr, pgx.ErrNoRows) {
		// Create new agent record.
		agent, err = s.queries.CreateStorageAgent(ctx, db.CreateStorageAgentParams{
			ID:        uuid.New(),
			GatewayID: gateway.ID,
			IpAddress: hs.GetIpAddress(),
			Hostname:  hs.GetHostname(),
			Version:   hs.GetAgentVersion(),
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to create agent", "error", err)
			return status.Error(codes.Internal, "failed to create agent record")
		}
	} else {
		// Reconnecting agent -- update state to CONNECTED.
		agent, err = s.queries.UpdateStorageAgentState(ctx, db.UpdateStorageAgentStateParams{
			ID:    existing.ID,
			State: db.AgentStateCONNECTED,
		})
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to update agent state", "error", err)
			return status.Error(codes.Internal, "failed to update agent state")
		}
	}

	// -----------------------------------------------------------------------
	// 4. Audit the handshake (inbound).
	// -----------------------------------------------------------------------
	s.auditMessage(ctx, gateway.ID, agent.ID, firstMsg.GetId(), "inbound", "handshake", firstMsg)

	// -----------------------------------------------------------------------
	// 5. Build HandshakeAck with endpoint configs and cache config.
	// -----------------------------------------------------------------------
	endpoints, err := s.queries.ListStorageEndpointsByGateway(ctx, gateway.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to list endpoints", "error", err)
		return status.Error(codes.Internal, "failed to list endpoints")
	}

	endpointConfigs, err := buildEndpointConfigs(endpoints)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to build endpoint configs", "error", err)
		return status.Error(codes.Internal, "failed to build endpoint configs")
	}

	ack := &agentv1.ControlMessage{
		Id: firstMsg.GetId(),
		Message: &agentv1.ControlMessage_HandshakeAck{
			HandshakeAck: &agentv1.HandshakeAck{
				AgentName: fmt.Sprintf("agent-%s-%s", gateway.Name, hs.GetIpAddress()),
				Endpoints: endpointConfigs,
				CacheConfig: &agentv1.CacheConfig{
					MaxSizeGb:      gateway.CacheMaxSizeGb,
					EvictionPolicy: strings.ToLower(string(gateway.CacheEviction)),
					TtlHours:       gateway.CacheTtlHours,
				},
			},
		},
	}

	// -----------------------------------------------------------------------
	// 6. Send HandshakeAck.
	// -----------------------------------------------------------------------
	if err := stream.Send(ack); err != nil {
		s.logger.ErrorContext(ctx, "failed to send handshake ack", "error", err)
		return status.Error(codes.Internal, "failed to send handshake ack")
	}

	// -----------------------------------------------------------------------
	// 7. Audit the handshake_ack (outbound).
	// -----------------------------------------------------------------------
	s.auditMessage(ctx, gateway.ID, agent.ID, firstMsg.GetId(), "outbound", "handshake_ack", ack)

	// -----------------------------------------------------------------------
	// 8. Update gateway state to ACTIVE if it was PROVISIONING.
	// -----------------------------------------------------------------------
	if gateway.State == db.StorageGatewayStatePROVISIONING {
		if err := s.queries.UpdateStorageGatewayState(ctx, db.UpdateStorageGatewayStateParams{
			ID:    gateway.ID,
			State: db.StorageGatewayStateACTIVE,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to update gateway state to ACTIVE", "error", err)
		}
	}

	// -----------------------------------------------------------------------
	// 9. Log: "agent connected".
	// -----------------------------------------------------------------------
	s.logger.InfoContext(ctx, "agent connected",
		"gateway", gateway.Name,
		"agent_ip", hs.GetIpAddress(),
		"agent_version", hs.GetAgentVersion(),
	)

	// -----------------------------------------------------------------------
	// 10. Enter receive loop.
	// -----------------------------------------------------------------------
	for {
		msg, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			s.logger.ErrorContext(ctx, "stream receive error", "error", err)
			break
		}

		switch m := msg.GetMessage().(type) {
		case *agentv1.AgentMessage_Heartbeat:
			// Update last_seen_time, DO NOT audit.
			if err := s.queries.UpdateStorageAgentHeartbeat(ctx, agent.ID); err != nil {
				s.logger.ErrorContext(ctx, "failed to update agent heartbeat", "error", err)
			}

		case *agentv1.AgentMessage_EndpointHealth:
			// Audit and log.
			s.auditMessage(ctx, gateway.ID, agent.ID, msg.GetId(), "inbound", "endpoint_health", msg)
			s.logger.InfoContext(ctx, "endpoint health report",
				"gateway", gateway.Name,
				"agent_ip", agent.IpAddress,
				"endpoint", m.EndpointHealth.GetEndpointName(),
				"reachable", m.EndpointHealth.GetReachable(),
				"latency_ms", m.EndpointHealth.GetLatencyMs(),
			)

		case *agentv1.AgentMessage_Telemetry:
			// DO NOT audit (too noisy).
			_ = m

		case *agentv1.AgentMessage_UpgradeStatus:
			// Audit and log.
			s.auditMessage(ctx, gateway.ID, agent.ID, msg.GetId(), "inbound", "upgrade_status", msg)
			s.logger.InfoContext(ctx, "upgrade status",
				"gateway", gateway.Name,
				"agent_ip", agent.IpAddress,
				"phase", m.UpgradeStatus.GetPhase().String(),
				"version", m.UpgradeStatus.GetVersion(),
			)

		case *agentv1.AgentMessage_SyncStatus:
			// Audit and log.
			s.auditMessage(ctx, gateway.ID, agent.ID, msg.GetId(), "inbound", "sync_status", msg)
			s.logger.InfoContext(ctx, "sync status",
				"gateway", gateway.Name,
				"agent_ip", agent.IpAddress,
				"pending_writes", m.SyncStatus.GetPendingWrites(),
				"synced_writes", m.SyncStatus.GetSyncedWrites(),
			)
		}
	}

	// -----------------------------------------------------------------------
	// 11. Stream ended -- handle disconnect.
	// -----------------------------------------------------------------------
	if _, err := s.queries.UpdateStorageAgentState(ctx, db.UpdateStorageAgentStateParams{
		ID:    agent.ID,
		State: db.AgentStateDISCONNECTED,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to set agent state to DISCONNECTED", "error", err)
	}

	// Check if all agents for this gateway are now disconnected.
	connectedCount, err := s.queries.CountConnectedStorageAgentsByGateway(ctx, gateway.ID)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to count connected agents", "error", err)
	} else if connectedCount == 0 {
		if err := s.queries.UpdateStorageGatewayState(ctx, db.UpdateStorageGatewayStateParams{
			ID:    gateway.ID,
			State: db.StorageGatewayStateOFFLINE,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to update gateway state to OFFLINE", "error", err)
		}
	}

	s.logger.InfoContext(ctx, "agent disconnected",
		"gateway", gateway.Name,
		"agent_ip", agent.IpAddress,
	)

	return nil
}

// redactSecretKeyPattern matches "secretAccessKey":"<value>" in protojson output.
var redactSecretKeyPattern = regexp.MustCompile(`("secretAccessKey"\s*:\s*)"[^"]*"`)

// marshalAndRedact marshals a protobuf message to JSON using protojson and
// redacts secret_access_key values by replacing them with "***".
func marshalAndRedact(msg proto.Message) ([]byte, error) {
	data, err := protojson.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("protojson marshal: %w", err)
	}
	redacted := redactSecretKeyPattern.ReplaceAll(data, []byte(`$1"***"`))
	return redacted, nil
}

// auditMessage persists an audit record for a given message. It marshals the
// proto message to JSON, redacts secrets, and writes to the audit table.
// Errors are logged but do not interrupt the stream.
func (s *AgentServiceServer) auditMessage(
	ctx context.Context,
	gatewayID uuid.UUID,
	agentID uuid.UUID,
	messageID string,
	direction string,
	messageType string,
	msg proto.Message,
) {
	payload, err := marshalAndRedact(msg)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to marshal audit payload",
			"error", err, "message_type", messageType)
		return
	}

	if err := s.queries.CreateStorageAgentAudit(ctx, db.CreateStorageAgentAuditParams{
		ID:          uuid.New(),
		GatewayID:   gatewayID,
		AgentID:     pgtype.UUID{Bytes: agentID, Valid: true},
		MessageID:   messageID,
		Direction:   direction,
		MessageType: messageType,
		Payload:     payload,
	}); err != nil {
		s.logger.ErrorContext(ctx, "failed to write audit record",
			"error", err, "message_type", messageType, "direction", direction)
	}
}

// buildEndpointConfigs converts DB StorageEndpoint records to proto
// EndpointConfig messages by parsing the JSONB configuration field.
func buildEndpointConfigs(endpoints []db.StorageEndpoint) ([]*agentv1.EndpointConfig, error) {
	configs := make([]*agentv1.EndpointConfig, 0, len(endpoints))
	for _, ep := range endpoints {
		cfg, err := parseEndpointConfig(ep)
		if err != nil {
			return nil, fmt.Errorf("endpoint %s: %w", ep.Name, err)
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

// endpointConfigJSON is the shape of the JSONB configuration column stored in
// the storage_endpoints table.
type endpointConfigJSON struct {
	Type string `json:"type"`

	// S3 fields
	EndpointURI     string `json:"endpoint_uri,omitempty"`
	Bucket          string `json:"bucket,omitempty"`
	Region          string `json:"region,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`

	// Filesystem fields
	Path string `json:"path,omitempty"`
}

func parseEndpointConfig(ep db.StorageEndpoint) (*agentv1.EndpointConfig, error) {
	var raw endpointConfigJSON
	if err := json.Unmarshal(ep.Configuration, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal configuration: %w", err)
	}

	cfg := &agentv1.EndpointConfig{
		Name: ep.Name,
	}

	switch raw.Type {
	case "s3":
		cfg.Configuration = &agentv1.EndpointConfig_S3{
			S3: &agentv1.S3EndpointConfig{
				EndpointUri:     raw.EndpointURI,
				Bucket:          raw.Bucket,
				Region:          raw.Region,
				AccessKeyId:     raw.AccessKeyID,
				SecretAccessKey: raw.SecretAccessKey,
			},
		}
	case "filesystem":
		cfg.Configuration = &agentv1.EndpointConfig_Filesystem{
			Filesystem: &agentv1.FileSystemEndpointConfig{
				Path: raw.Path,
			},
		}
	default:
		return nil, fmt.Errorf("unknown endpoint type: %q", raw.Type)
	}

	return cfg, nil
}
