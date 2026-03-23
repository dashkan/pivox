package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox-server/internal/apierr"
	"github.com/dashkan/pivox-server/internal/convert"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/lro"
	storagev1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/storage/v1"
)

type EndpointsServer struct {
	storagev1.UnimplementedEndpointsServer
	pool    *pgxpool.Pool
	queries *db.Queries
}

func NewEndpointsServer(pool *pgxpool.Pool, queries *db.Queries) *EndpointsServer {
	return &EndpointsServer{
		pool:    pool,
		queries: queries,
	}
}

// parseEndpointName parses "organizations/{org}/storageGateways/{gw}/endpoints/{endpoint}"
// and returns (orgName, gwName, endpointName).
func parseEndpointName(name string) (string, string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "storageGateways" || parts[4] != "endpoints" {
		return "", "", "", fmt.Errorf("invalid endpoint name %q: expected organizations/*/storageGateways/*/endpoints/*", name)
	}
	return parts[1], parts[3], parts[5], nil
}

// resolveEndpointGateway looks up a storage gateway by org name and gateway name.
func (s *EndpointsServer) resolveEndpointGateway(ctx context.Context, orgName, gwName string) (db.StorageGateway, error) {
	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return db.StorageGateway{}, handleResourceError(err, "Organization", orgName)
	}
	gw, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return db.StorageGateway{}, handleResourceError(err, "StorageGateway", fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName))
	}
	return gw, nil
}

// protoEngineToDB converts a proto Endpoint_Engine to a DB EndpointEngine.
func protoEngineToDB(e storagev1.Endpoint_Engine) db.EndpointEngine {
	switch e {
	case storagev1.Endpoint_S3:
		return db.EndpointEngineS3
	case storagev1.Endpoint_RUSTFS:
		return db.EndpointEngineRUSTFS
	case storagev1.Endpoint_GCS:
		return db.EndpointEngineGCS
	case storagev1.Endpoint_MINIO:
		return db.EndpointEngineMINIO
	default:
		return db.EndpointEngineS3
	}
}

func (s *EndpointsServer) CreateEndpoint(ctx context.Context, req *storagev1.CreateEndpointRequest) (*longrunningpb.Operation, error) {
	orgName, gwName, err := parseGatewayParent(req.GetParent())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	endpoint := req.GetEndpoint()
	endpointID := req.GetEndpointId()
	if endpointID == "" {
		endpointID = uuid.New().String()[:8]
	}

	var annotationsJSON json.RawMessage
	if annotations := endpoint.GetAnnotations(); annotations != nil {
		annotationsJSON, _ = json.Marshal(annotations)
	} else {
		annotationsJSON = json.RawMessage("{}")
	}

	var credentialsJSON []byte
	credentialState := db.CredentialStateUNSET
	if creds := endpoint.GetCredentials(); creds != nil {
		credentialsJSON, _ = json.Marshal(creds)
		credentialState = db.CredentialStateSET
	}

	result, err := s.queries.CreateStorageEndpoint(ctx, db.CreateStorageEndpointParams{
		ID:              uuid.New(),
		GatewayID:       gw.ID,
		Name:            endpointID,
		DisplayName:     endpoint.GetDisplayName(),
		Engine:          protoEngineToDB(endpoint.GetEngine()),
		EndpointUri:     endpoint.GetEndpointUri(),
		Bucket:          endpoint.GetBucket(),
		Region:          endpoint.GetRegion(),
		Credentials:     credentialsJSON,
		CredentialState: credentialState,
		Annotations:     annotationsJSON,
		CreatedBy:       "",
	})
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", "")
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return lro.DoneOperation(convert.EndpointToProto(result, gatewayName))
}

func (s *EndpointsServer) GetEndpoint(ctx context.Context, req *storagev1.GetEndpointRequest) (*storagev1.Endpoint, error) {
	orgName, gwName, endpointName, err := parseEndpointName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	endpoint, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", req.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return convert.EndpointToProto(endpoint, gatewayName), nil
}

func (s *EndpointsServer) ListEndpoints(ctx context.Context, req *storagev1.ListEndpointsRequest) (*storagev1.ListEndpointsResponse, error) {
	orgName, gwName, err := parseGatewayParent(req.GetParent())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("parent", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	endpoints, err := s.queries.ListStorageEndpointsByGateway(ctx, gw.ID)
	if err != nil {
		return nil, apierr.Internal("failed to list endpoints")
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	pbEndpoints := make([]*storagev1.Endpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		pbEndpoints = append(pbEndpoints, convert.EndpointToProto(ep, gatewayName))
	}

	return &storagev1.ListEndpointsResponse{
		Endpoints: pbEndpoints,
	}, nil
}

func (s *EndpointsServer) UpdateEndpoint(ctx context.Context, req *storagev1.UpdateEndpointRequest) (*longrunningpb.Operation, error) {
	endpoint := req.GetEndpoint()
	orgName, gwName, endpointName, err := parseEndpointName(endpoint.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("endpoint.name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", endpoint.GetName())
	}

	updateParams := db.UpdateStorageEndpointParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: endpoint.GetDisplayName(), Valid: true}
			case "endpoint_uri":
				updateParams.EndpointUri = pgtype.Text{String: endpoint.GetEndpointUri(), Valid: true}
			case "bucket":
				updateParams.Bucket = pgtype.Text{String: endpoint.GetBucket(), Valid: true}
			case "region":
				updateParams.Region = pgtype.Text{String: endpoint.GetRegion(), Valid: true}
			case "annotations":
				annotationsJSON, err := json.Marshal(endpoint.GetAnnotations())
				if err != nil {
					return nil, apierr.Internal("failed to marshal annotations")
				}
				updateParams.Annotations = annotationsJSON
			}
		}
	} else {
		updateParams.DisplayName = pgtype.Text{String: endpoint.GetDisplayName(), Valid: true}
		updateParams.EndpointUri = pgtype.Text{String: endpoint.GetEndpointUri(), Valid: true}
		updateParams.Bucket = pgtype.Text{String: endpoint.GetBucket(), Valid: true}
		updateParams.Region = pgtype.Text{String: endpoint.GetRegion(), Valid: true}
		if annotations := endpoint.GetAnnotations(); annotations != nil {
			annotationsJSON, _ := json.Marshal(annotations)
			updateParams.Annotations = annotationsJSON
		} else {
			updateParams.Annotations = existing.Annotations
		}
	}

	result, err := s.queries.UpdateStorageEndpoint(ctx, updateParams)
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", endpoint.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return lro.DoneOperation(convert.EndpointToProto(result, gatewayName))
}

func (s *EndpointsServer) DeleteEndpoint(ctx context.Context, req *storagev1.DeleteEndpointRequest) (*longrunningpb.Operation, error) {
	orgName, gwName, endpointName, err := parseEndpointName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", req.GetName())
	}

	err = s.queries.DeleteStorageEndpoint(ctx, existing.ID)
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", req.GetName())
	}

	return lro.DoneOperation(&storagev1.Endpoint{
		Name: req.GetName(),
	})
}

func (s *EndpointsServer) SetEndpointCredentials(ctx context.Context, req *storagev1.SetEndpointCredentialsRequest) (*storagev1.Endpoint, error) {
	orgName, gwName, endpointName, err := parseEndpointName(req.GetName())
	if err != nil {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("name", err.Error()))
	}

	gw, err := s.resolveEndpointGateway(ctx, orgName, gwName)
	if err != nil {
		return nil, err
	}

	existing, err := s.queries.GetStorageEndpointByName(ctx, db.GetStorageEndpointByNameParams{
		GatewayID: gw.ID,
		Name:      endpointName,
	})
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", req.GetName())
	}

	credentialsJSON, err := json.Marshal(req.GetCredentials())
	if err != nil {
		return nil, apierr.Internal("failed to marshal credentials")
	}

	result, err := s.queries.SetStorageEndpointCredentials(ctx, db.SetStorageEndpointCredentialsParams{
		ID:          existing.ID,
		Credentials: credentialsJSON,
	})
	if err != nil {
		return nil, handleResourceError(err, "Endpoint", req.GetName())
	}

	gatewayName := fmt.Sprintf("organizations/%s/storageGateways/%s", orgName, gwName)
	return convert.EndpointToProto(result, gatewayName), nil
}

func (s *EndpointsServer) TestEndpointConnection(ctx context.Context, req *storagev1.TestEndpointConnectionRequest) (*storagev1.TestEndpointConnectionResponse, error) {
	return nil, status.Error(codes.Unimplemented, "TestEndpointConnection is not yet implemented")
}
