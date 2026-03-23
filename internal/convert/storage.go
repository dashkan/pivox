package convert

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/types/known/timestamppb"

	db "github.com/dashkan/pivox-server/internal/db/generated"
	storagev1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/storage/v1"
)

// StorageGatewayToProto converts a DB storage gateway to proto.
// orgName is the organization slug (e.g. "meridian-broadcasting").
func StorageGatewayToProto(gw db.StorageGateway, orgName string) *storagev1.StorageGateway {
	pb := &storagev1.StorageGateway{
		Name:              fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gw.Name),
		DisplayName:       gw.DisplayName,
		State:             storageGatewayState(gw.State),
		Hostname:          gw.Hostname,
		IpAddresses:       gw.IpAddresses,
		RegistrationToken: gw.RegistrationToken,
		TargetVersion:     gw.TargetVersion,
		CurrentVersion:    gw.CurrentVersion,
		CacheConfig: &storagev1.CacheConfig{
			MaxSizeGb:      gw.CacheMaxSizeGb,
			EvictionPolicy: cacheEvictionPolicy(gw.CacheEviction),
			TtlHours:       gw.CacheTtlHours,
		},
		CertState:  certState(gw.CertState),
		Etag:       gw.Etag,
		Creator:    gw.CreatedBy,
		Updater:    gw.UpdatedBy,
		CreateTime: timestamppb.New(gw.CreateTime),
		UpdateTime: timestamppb.New(gw.UpdateTime),
	}
	if gw.CertExpiryTime.Valid {
		pb.CertExpiryTime = timestamppb.New(gw.CertExpiryTime.Time)
	}
	if len(gw.Annotations) > 0 {
		annotations := make(map[string]string)
		_ = json.Unmarshal(gw.Annotations, &annotations)
		pb.Annotations = annotations
	}
	return pb
}

// AgentToProto converts a DB agent to proto.
// gatewayName is the full resource name of the parent storage gateway
// (e.g. "organizations/acme/storageGateways/gw-1").
func AgentToProto(a db.StorageAgent, gatewayName string) *storagev1.Agent {
	pb := &storagev1.Agent{
		Name:         fmt.Sprintf("%s/agents/%s", gatewayName, a.ID.String()),
		IpAddress:    a.IpAddress,
		Hostname:     a.Hostname,
		State:        agentState(a.State),
		Version:      a.Version,
		CacheUsedGb:  a.CacheUsedGb,
		JoinTime:     timestamppb.New(a.JoinTime),
		LastSeenTime: timestamppb.New(a.LastSeenTime),
	}
	if a.CertExpiryTime.Valid {
		pb.CertExpiryTime = timestamppb.New(a.CertExpiryTime.Time)
	}
	return pb
}

// EndpointToProto converts a DB endpoint to proto.
// gatewayName is the full resource name of the parent storage gateway
// (e.g. "organizations/acme/storageGateways/gw-1").
func EndpointToProto(ep db.StorageEndpoint, gatewayName string) *storagev1.Endpoint {
	pb := &storagev1.Endpoint{
		Name:            fmt.Sprintf("%s/endpoints/%s", gatewayName, ep.Name),
		DisplayName:     ep.DisplayName,
		Engine:          endpointEngine(ep.Engine),
		EndpointUri:     ep.EndpointUri,
		Bucket:          ep.Bucket,
		Region:          ep.Region,
		State:           endpointState(ep.State),
		CredentialState: credentialState(ep.CredentialState),
		Etag:            ep.Etag,
		Creator:         ep.CreatedBy,
		Updater:         ep.UpdatedBy,
		CreateTime:      timestamppb.New(ep.CreateTime),
		UpdateTime:      timestamppb.New(ep.UpdateTime),
	}
	if len(ep.Annotations) > 0 {
		annotations := make(map[string]string)
		_ = json.Unmarshal(ep.Annotations, &annotations)
		pb.Annotations = annotations
	}
	return pb
}

func storageGatewayState(s db.StorageGatewayState) storagev1.StorageGateway_State {
	switch s {
	case db.StorageGatewayStatePROVISIONING:
		return storagev1.StorageGateway_PROVISIONING
	case db.StorageGatewayStateACTIVE:
		return storagev1.StorageGateway_ACTIVE
	case db.StorageGatewayStateDEGRADED:
		return storagev1.StorageGateway_DEGRADED
	case db.StorageGatewayStateOFFLINE:
		return storagev1.StorageGateway_OFFLINE
	default:
		return storagev1.StorageGateway_STATE_UNSPECIFIED
	}
}

func certState(s db.CertState) storagev1.StorageGateway_CertState {
	switch s {
	case db.CertStatePENDING:
		return storagev1.StorageGateway_PENDING
	case db.CertStateACTIVE:
		return storagev1.StorageGateway_CERT_ACTIVE
	case db.CertStateEXPIRING:
		return storagev1.StorageGateway_EXPIRING
	case db.CertStateEXPIRED:
		return storagev1.StorageGateway_EXPIRED
	default:
		return storagev1.StorageGateway_CERT_STATE_UNSPECIFIED
	}
}

func cacheEvictionPolicy(p db.EvictionPolicy) storagev1.CacheConfig_EvictionPolicy {
	switch p {
	case db.EvictionPolicyLRU:
		return storagev1.CacheConfig_LRU
	case db.EvictionPolicyLFU:
		return storagev1.CacheConfig_LFU
	default:
		return storagev1.CacheConfig_EVICTION_POLICY_UNSPECIFIED
	}
}

func agentState(s db.AgentState) storagev1.Agent_State {
	switch s {
	case db.AgentStateCONNECTING:
		return storagev1.Agent_CONNECTING
	case db.AgentStateCONNECTED:
		return storagev1.Agent_CONNECTED
	case db.AgentStateDRAINING:
		return storagev1.Agent_DRAINING
	case db.AgentStateUPGRADING:
		return storagev1.Agent_UPGRADING
	case db.AgentStateDISCONNECTED:
		return storagev1.Agent_DISCONNECTED
	default:
		return storagev1.Agent_STATE_UNSPECIFIED
	}
}

func endpointEngine(e db.EndpointEngine) storagev1.Endpoint_Engine {
	switch e {
	case db.EndpointEngineS3:
		return storagev1.Endpoint_S3
	case db.EndpointEngineRUSTFS:
		return storagev1.Endpoint_RUSTFS
	case db.EndpointEngineGCS:
		return storagev1.Endpoint_GCS
	case db.EndpointEngineMINIO:
		return storagev1.Endpoint_MINIO
	default:
		return storagev1.Endpoint_ENGINE_UNSPECIFIED
	}
}

func endpointState(s db.EndpointState) storagev1.Endpoint_State {
	switch s {
	case db.EndpointStateACTIVE:
		return storagev1.Endpoint_ACTIVE
	case db.EndpointStateINACTIVE:
		return storagev1.Endpoint_INACTIVE
	case db.EndpointStateUNREACHABLE:
		return storagev1.Endpoint_UNREACHABLE
	default:
		return storagev1.Endpoint_STATE_UNSPECIFIED
	}
}

func credentialState(s db.CredentialState) storagev1.Endpoint_CredentialState {
	switch s {
	case db.CredentialStateUNSET:
		return storagev1.Endpoint_UNSET
	case db.CredentialStateSET:
		return storagev1.Endpoint_SET
	case db.CredentialStateINVALID:
		return storagev1.Endpoint_INVALID
	default:
		return storagev1.Endpoint_CREDENTIAL_STATE_UNSPECIFIED
	}
}
