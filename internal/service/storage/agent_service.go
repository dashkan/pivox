package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/dashkan/pivox/internal/server"
)

// AgentServiceServer implements the bidirectional streaming AgentService for
// storage gateway agents connecting to the control plane.
type AgentServiceServer struct {
	agentv1.UnimplementedAgentServiceServer
	pool              db.TxBeginner
	queries           db.Querier
	logger            *slog.Logger
	conns             *agentstream.ConnectionManager
	sessionSigningKey []byte
	corsOrigin        string
}

// AgentServiceConfig is the constructor input for AgentServiceServer.
type AgentServiceConfig struct {
	// Pool is the database pool used for transactional bootstrap +
	// disconnect blocks (handshake agent-create/update; disconnect
	// state flip + connected-count check + gateway state flip).
	// Required.
	Pool db.TxBeginner
	// Queries is the sqlc query interface for non-transactional
	// reads (auditMessage, single-write paths during the stream
	// loop). Required.
	Queries db.Querier
	// Logger is the structured logger. Required.
	Logger *slog.Logger
	// Conns tracks connected agents and routes outbound messages.
	// Required.
	Conns *agentstream.ConnectionManager

	// SessionSigningKey is stamped into HandshakeAck.session_signing_key
	// so connected agents can validate the session JWTs the
	// controller's CreateStorageSession mints. MUST equal
	// StorageGatewaysConfig.SessionSigningKey — main.go is
	// responsible for declaring the key once and passing it to both
	// servers. Optional only for tests; production callers should
	// always provide it. Tracked: #27 cumulative-audit fix; #24
	// (KMS-load).
	SessionSigningKey []byte

	// CORSOrigin is stamped into HandshakeAck.cors_origin so the
	// agent's HTTP server can set the right Access-Control-Allow-
	// Origin header. Optional; agent falls back to "*" when this
	// field is empty AND the agent's own --cors-origin flag is
	// also unset.
	CORSOrigin string
}

// NewAgentServiceServer constructs the server from cfg. Panics on a
// missing required field.
func NewAgentServiceServer(cfg AgentServiceConfig) *AgentServiceServer {
	if cfg.Pool == nil {
		panic("storage: AgentServiceConfig.Pool is required")
	}
	if cfg.Queries == nil {
		panic("storage: AgentServiceConfig.Queries is required")
	}
	if cfg.Logger == nil {
		panic("storage: AgentServiceConfig.Logger is required")
	}
	if cfg.Conns == nil {
		panic("storage: AgentServiceConfig.Conns is required")
	}
	return &AgentServiceServer{
		pool:              cfg.Pool,
		queries:           cfg.Queries,
		logger:            cfg.Logger,
		conns:             cfg.Conns,
		sessionSigningKey: cfg.SessionSigningKey,
		corsOrigin:        cfg.CORSOrigin,
	}
}

// Connect implements the bidirectional streaming RPC. The agent sends a
// Handshake as the first message, then continuously sends heartbeats, health
// checks, and telemetry. The server responds with a HandshakeAck containing
// initial configuration, and may push config updates or lifecycle commands.
func (s *AgentServiceServer) Connect(stream agentv1.AgentService_ConnectServer) error {
	ctx := stream.Context()

	// -----------------------------------------------------------------------
	// 1. Resolve the authenticated gateway. The gRPC interceptor
	//    (server.AgentAuthStreamInterceptor) has already validated the
	//    registration token from initial metadata and put the matching
	//    gateway in context — if it's not there, this RPC bypassed auth,
	//    which should be impossible on the service-to-service listener.
	// -----------------------------------------------------------------------
	gateway, ok := server.AuthenticatedGateway(ctx)
	if !ok {
		s.logger.ErrorContext(ctx, "agent connect reached handler without authenticated gateway in context")
		return apierr.Internal("agent gateway context missing")
	}

	// -----------------------------------------------------------------------
	// 2. Wait for the first message -- must be a Handshake.
	// -----------------------------------------------------------------------
	firstMsg, err := stream.Recv()
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to receive first message", "error", err)
		return apierr.Internal("failed to receive first message")
	}

	hs := firstMsg.GetHandshake()
	if hs == nil {
		return apierr.BadRequest("first message must be handshake")
	}

	// -----------------------------------------------------------------------
	// 3. Create or update agent record AND flip gateway to ACTIVE.
	//
	// Tx-wrapped:
	//   - Agent lookup-then-create-or-update is a classic
	//     read-then-write race; two concurrent connections from the
	//     same (gateway, ip) could each see pgx.ErrNoRows and both
	//     try CreateStorageAgent, producing a duplicate-key violation.
	//   - Gateway state flip joins the same tx so the gateway becomes
	//     operational atomically with the agent registration. Three
	//     wins from pulling it in:
	//     (a) reconnect from OFFLINE now flips back to ACTIVE (the
	//         old condition only fired on PROVISIONING — a gateway
	//         that hit OFFLINE because all its agents disconnected
	//         would stay OFFLINE forever even after a fresh agent
	//         connects).
	//     (b) errors on the gateway-state UPDATE are no longer
	//         silently dropped; they roll back the agent record,
	//         which surfaces as a stream error so the agent retries.
	//     (c) any concurrent disconnect-tx that's about to flip the
	//         gateway to OFFLINE will block on the row lock our
	//         UPDATE takes here — preventing the "gateway flips
	//         OFFLINE while a fresh agent is mid-handshake" race.
	// -----------------------------------------------------------------------
	agent, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (db.StorageAgent, error) {
		existing, lookupErr := qtx.GetStorageAgentByGatewayAndIP(ctx, db.GetStorageAgentByGatewayAndIPParams{
			GatewayID: gateway.ID,
			IpAddress: hs.GetIpAddress(),
		})
		if lookupErr != nil && !errors.Is(lookupErr, pgx.ErrNoRows) {
			s.logger.ErrorContext(ctx, "failed to look up existing agent", "error", lookupErr)
			return db.StorageAgent{}, apierr.Internal("failed to look up agent")
		}

		var result db.StorageAgent
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			created, err := qtx.CreateStorageAgent(ctx, db.CreateStorageAgentParams{
				ID:        uuid.New(),
				GatewayID: gateway.ID,
				IpAddress: hs.GetIpAddress(),
				Hostname:  hs.GetHostname(),
				Version:   hs.GetAgentVersion(),
			})
			if err != nil {
				s.logger.ErrorContext(ctx, "failed to create agent", "error", err)
				return db.StorageAgent{}, apierr.Internal("failed to create agent record")
			}
			result = created
		} else {
			// Reconnecting agent -- update state to CONNECTED.
			updated, err := qtx.UpdateStorageAgentState(ctx, db.UpdateStorageAgentStateParams{
				ID:    existing.ID,
				State: db.AgentStateCONNECTED,
			})
			if err != nil {
				s.logger.ErrorContext(ctx, "failed to update agent state", "error", err)
				return db.StorageAgent{}, apierr.Internal("failed to update agent state")
			}
			result = updated
		}

		// Gateway becomes ACTIVE on a successful handshake from the
		// two states where a healthy agent should bring it back online:
		//
		//   PROVISIONING — first-ever agent on a freshly-created gateway.
		//   OFFLINE      — last agent had disconnected; this is a reconnect.
		//
		// Allowlist intentionally excludes DEGRADED. DEGRADED is a state
		// other components (operator action, future health worker) set
		// deliberately to mark a partial-failure mode; a reconnecting
		// agent's handshake should NOT silently overwrite that. Today
		// nothing writes DEGRADED, but the moment something does, a
		// denylist (`!= ACTIVE`) here would clobber it on every
		// reconnect. Allowlist is the safer shape.
		if gateway.State == db.StorageGatewayStatePROVISIONING ||
			gateway.State == db.StorageGatewayStateOFFLINE {
			if err := qtx.UpdateStorageGatewayState(ctx, db.UpdateStorageGatewayStateParams{
				ID:    gateway.ID,
				State: db.StorageGatewayStateACTIVE,
			}); err != nil {
				s.logger.ErrorContext(ctx, "failed to update gateway state to ACTIVE", "error", err)
				return db.StorageAgent{}, apierr.Internal("failed to update gateway state")
			}
		}
		return result, nil
	})
	if err != nil {
		return err
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
		return apierr.Internal("failed to list endpoints")
	}

	endpointConfigs, err := buildEndpointConfigs(endpoints)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to build endpoint configs", "error", err)
		return apierr.Internal("failed to build endpoint configs")
	}

	ack := &agentv1.ControlMessage{
		Id: firstMsg.GetId(),
		Message: &agentv1.ControlMessage_HandshakeAck{
			HandshakeAck: &agentv1.HandshakeAck{
				AgentName: fmt.Sprintf("agent-%s-%s", gateway.Name, hs.GetIpAddress()),
				Endpoints: endpointConfigs,
				// #27 cumulative-audit fix: ship the session
				// signing key + CORS origin to the agent so the
				// agent's validateJWT HMACs against the same key
				// the controller's mintSessionJWT signs with.
				// Without this wire the per-phase tests pass but
				// production storage requests get 401 across the
				// board.
				SessionSigningKey: s.sessionSigningKey,
				CorsOrigin:        s.corsOrigin,
			},
		},
	}

	// -----------------------------------------------------------------------
	// 6. Send HandshakeAck.
	// -----------------------------------------------------------------------
	if err := stream.Send(ack); err != nil {
		s.logger.ErrorContext(ctx, "failed to send handshake ack", "error", err)
		return apierr.Internal("failed to send handshake ack")
	}

	// -----------------------------------------------------------------------
	// 7. Audit the handshake_ack (outbound).
	// -----------------------------------------------------------------------
	s.auditMessage(ctx, gateway.ID, agent.ID, firstMsg.GetId(), "outbound", "handshake_ack", ack)

	// -----------------------------------------------------------------------
	// 8. Register connection and defer unregister on disconnect.
	// -----------------------------------------------------------------------
	s.conns.Register(&agentstream.AgentConnection{
		AgentID:   agent.ID,
		GatewayID: gateway.ID,
		OrgID:     gateway.OrgID,
		Stream:    stream,
	})
	defer s.conns.Unregister(agent.ID)

	// -----------------------------------------------------------------------
	// 10. Log: "agent connected".
	// -----------------------------------------------------------------------
	s.logger.InfoContext(ctx, "agent connected",
		"gateway", gateway.Name,
		"agent_ip", hs.GetIpAddress(),
		"agent_version", hs.GetAgentVersion(),
	)

	// -----------------------------------------------------------------------
	// 11. Enter receive loop.
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
			if err := s.queries.UpdateStorageAgentHeartbeat(ctx, agent.ID); err != nil {
				s.logger.ErrorContext(ctx, "failed to update agent heartbeat", "error", err)
			}
			s.auditMessage(ctx, gateway.ID, agent.ID, msg.GetId(), "inbound", "heartbeat", msg)

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
	// 12. Stream ended -- handle disconnect.
	//
	// Tx-wrapped: the disconnect is a write-then-count-then-conditional-
	// write sequence. Without the tx, a second agent connecting to
	// this gateway between our count and our gateway-state UPDATE
	// would let us flip the gateway to OFFLINE while it's actually
	// in use. Inside a single tx the count sees a consistent
	// snapshot post-our-UPDATE, and any concurrent connect/disconnect
	// serializes around the row locks.
	//
	// Errors inside the tx still log + return from the closure;
	// RunInTx rolls back. We don't surface the error to the gRPC
	// client (the stream is already ending) but we do want the
	// log + structured cleanup.
	// -----------------------------------------------------------------------
	if err := db.RunInTxVoid(ctx, s.pool, func(qtx db.Querier) error {
		if _, err := qtx.UpdateStorageAgentState(ctx, db.UpdateStorageAgentStateParams{
			ID:    agent.ID,
			State: db.AgentStateDISCONNECTED,
		}); err != nil {
			s.logger.ErrorContext(ctx, "failed to set agent state to DISCONNECTED", "error", err)
			return err
		}

		// Check if all agents for this gateway are now disconnected.
		connectedCount, err := qtx.CountConnectedStorageAgentsByGateway(ctx, gateway.ID)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to count connected agents", "error", err)
			return err
		}
		if connectedCount == 0 {
			if err := qtx.UpdateStorageGatewayState(ctx, db.UpdateStorageGatewayStateParams{
				ID:    gateway.ID,
				State: db.StorageGatewayStateOFFLINE,
			}); err != nil {
				s.logger.ErrorContext(ctx, "failed to update gateway state to OFFLINE", "error", err)
				return err
			}
		}
		return nil
	}); err != nil {
		// Per-query failures already logged inside the closure; the
		// tx rolled back. Add a summary line for telemetry — the
		// closure's per-step logs identify which query failed, this
		// summary makes the rolled-back-tx outcome searchable. We
		// don't surface to the gRPC client because the stream is
		// already ending; nothing to do with the error.
		s.logger.WarnContext(ctx, "disconnect cleanup tx rolled back",
			"gateway", gateway.Name,
			"agent_id", agent.ID,
			"error", err,
		)
	}

	s.logger.InfoContext(ctx, "agent disconnected",
		"gateway", gateway.Name,
		"agent_ip", agent.IpAddress,
	)

	return nil
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
	payload, err := agentstream.MarshalAndRedact(msg)
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
	EndpointURI string           `json:"endpoint_uri,omitempty"`
	Bucket      string           `json:"bucket,omitempty"`
	Region      string           `json:"region,omitempty"`
	AccessKey   *s3AccessKeyJSON `json:"access_key,omitempty"`

	// Filesystem fields
	Path string `json:"path,omitempty"`
}

type s3AccessKeyJSON struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

func (c endpointConfigJSON) accessKeyID() string {
	if c.AccessKey != nil {
		return c.AccessKey.AccessKeyID
	}
	return ""
}

func (c endpointConfigJSON) secretAccessKey() string {
	if c.AccessKey != nil {
		return c.AccessKey.SecretAccessKey
	}
	return ""
}

func parseEndpointConfig(ep db.StorageEndpoint) (*agentv1.EndpointConfig, error) {
	var raw endpointConfigJSON
	if err := json.Unmarshal(ep.Configuration, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal configuration: %w", err)
	}

	cfg := &agentv1.EndpointConfig{
		Name: ep.Name,
		CacheConfig: &agentv1.EndpointCacheConfig{
			Enabled:        ep.CacheEnabled,
			MaxSizeGb:      ep.CacheMaxSizeGb,
			EvictionPolicy: string(ep.CacheEviction),
			TtlHours:       ep.CacheTtlHours,
		},
	}

	switch raw.Type {
	case "s3":
		cfg.Configuration = &agentv1.EndpointConfig_S3{
			S3: &agentv1.S3EndpointConfig{
				EndpointUri:     raw.EndpointURI,
				Bucket:          raw.Bucket,
				Region:          raw.Region,
				AccessKeyId:     raw.accessKeyID(),
				SecretAccessKey: raw.secretAccessKey(),
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
