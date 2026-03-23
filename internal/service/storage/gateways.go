package storage

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
	"github.com/dashkan/pivox-server/internal/crypto"
	db "github.com/dashkan/pivox-server/internal/db/generated"
	"github.com/dashkan/pivox-server/internal/lro"
	storagev1 "github.com/dashkan/pivox-server/internal/pkg/gen/pivox/storage/v1"
	"github.com/dashkan/pivox-server/internal/resource"
)

type StorageGatewaysServer struct {
	storagev1.UnimplementedStorageGatewaysServer
	pool      *pgxpool.Pool
	queries   *db.Queries
	encryptor crypto.Encryptor
}

func NewStorageGatewaysServer(pool *pgxpool.Pool, queries *db.Queries, enc crypto.Encryptor) *StorageGatewaysServer {
	return &StorageGatewaysServer{
		pool:      pool,
		queries:   queries,
		encryptor: enc,
	}
}

// parseStorageGatewayName parses "organizations/{org}/storageGateways/{gw}" and returns (orgName, gwName).
func parseStorageGatewayName(name string) (string, string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "storageGateways" {
		return "", "", fmt.Errorf("invalid storage gateway name %q: expected organizations/*/storageGateways/*", name)
	}
	return parts[1], parts[3], nil
}

func (s *StorageGatewaysServer) CreateStorageGateway(ctx context.Context, req *storagev1.CreateStorageGatewayRequest) (*longrunningpb.Operation, error) {
	gw := req.GetStorageGateway()

	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}
	orgName, _ := resource.ParseSegment(req.GetParent())

	gwName := req.GetStorageGatewayId()
	if gwName == "" {
		gwName = uuid.New().String()[:8]
	}

	registrationToken := uuid.New().String()
	hostname := gwName + ".storage.pivox.app"

	var annotationsJSON json.RawMessage
	if annotations := gw.GetAnnotations(); annotations != nil {
		annotationsJSON, _ = json.Marshal(annotations)
	} else {
		annotationsJSON = json.RawMessage("{}")
	}

	result, err := s.queries.CreateStorageGateway(ctx, db.CreateStorageGatewayParams{
		ID:                uuid.New(),
		OrgID:             orgID,
		Name:              gwName,
		DisplayName:       gw.GetDisplayName(),
		IpAddresses:       gw.GetIpAddresses(),
		RegistrationToken: registrationToken,
		Hostname:          hostname,
		Annotations:       annotationsJSON,
		CreatedBy:         "",
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", gwName)
	}

	return lro.DoneOperation(convert.StorageGatewayToProto(result, orgName))
}

func (s *StorageGatewaysServer) GetStorageGateway(ctx context.Context, req *storagev1.GetStorageGatewayRequest) (*storagev1.StorageGateway, error) {
	orgName, gwName, err := parseStorageGatewayName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	gw, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	return convert.StorageGatewayToProto(gw, orgName), nil
}

func (s *StorageGatewaysServer) ListStorageGateways(_ context.Context, _ *storagev1.ListStorageGatewaysRequest) (*storagev1.ListStorageGatewaysResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "ListStorageGateways not yet implemented")
}

func (s *StorageGatewaysServer) UpdateStorageGateway(ctx context.Context, req *storagev1.UpdateStorageGatewayRequest) (*longrunningpb.Operation, error) {
	gw := req.GetStorageGateway()
	orgName, gwName, err := parseStorageGatewayName(gw.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", gw.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", gw.GetName())
	}

	updateParams := db.UpdateStorageGatewayParams{
		ID:        existing.ID,
		UpdatedBy: "",
	}

	mask := req.GetUpdateMask()
	if mask != nil {
		for _, path := range mask.GetPaths() {
			switch path {
			case "display_name":
				updateParams.DisplayName = pgtype.Text{String: gw.GetDisplayName(), Valid: true}
			case "ip_addresses":
				updateParams.IpAddresses = gw.GetIpAddresses()
			case "target_version":
				updateParams.TargetVersion = pgtype.Text{String: gw.GetTargetVersion(), Valid: true}
			case "annotations":
				annotationsJSON, err := json.Marshal(gw.GetAnnotations())
				if err != nil {
					return nil, status.Errorf(codes.Internal, "failed to marshal annotations")
				}
				updateParams.Annotations = annotationsJSON
			}
		}
	} else {
		// No mask: update all mutable fields.
		updateParams.DisplayName = pgtype.Text{String: gw.GetDisplayName(), Valid: true}
		updateParams.IpAddresses = gw.GetIpAddresses()
		updateParams.TargetVersion = pgtype.Text{String: gw.GetTargetVersion(), Valid: true}
		if annotations := gw.GetAnnotations(); annotations != nil {
			annotationsJSON, _ := json.Marshal(annotations)
			updateParams.Annotations = annotationsJSON
		}
	}

	result, err := s.queries.UpdateStorageGateway(ctx, updateParams)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", gw.GetName())
	}

	return lro.DoneOperation(convert.StorageGatewayToProto(result, orgName))
}

func (s *StorageGatewaysServer) DeleteStorageGateway(ctx context.Context, req *storagev1.DeleteStorageGatewayRequest) (*longrunningpb.Operation, error) {
	orgName, gwName, err := parseStorageGatewayName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	if err := s.queries.DeleteStorageGateway(ctx, existing.ID); err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	return lro.DoneOperation(&storagev1.StorageGateway{Name: req.GetName()})
}

func (s *StorageGatewaysServer) RotateRegistrationToken(ctx context.Context, req *storagev1.RotateRegistrationTokenRequest) (*storagev1.StorageGateway, error) {
	orgName, gwName, err := parseStorageGatewayName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	existing, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	newToken := uuid.New().String()

	result, err := s.queries.RotateRegistrationToken(ctx, db.RotateRegistrationTokenParams{
		ID:                existing.ID,
		RegistrationToken: newToken,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	return convert.StorageGatewayToProto(result, orgName), nil
}

func (s *StorageGatewaysServer) GetInstallScript(ctx context.Context, req *storagev1.GetInstallScriptRequest) (*storagev1.GetInstallScriptResponse, error) {
	orgName, gwName, err := parseStorageGatewayName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	gw, err := s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	var flags []string
	if req.GetCacheDir() != "" {
		flags = append(flags, fmt.Sprintf("--cache-dir %s", req.GetCacheDir()))
	}
	if req.GetCacheSizeGb() > 0 {
		flags = append(flags, fmt.Sprintf("--cache-size-gb %d", req.GetCacheSizeGb()))
	}
	if req.GetPort() > 0 {
		flags = append(flags, fmt.Sprintf("--port %d", req.GetPort()))
	}
	if req.GetBindAddress() != "" {
		flags = append(flags, fmt.Sprintf("--bind-address %s", req.GetBindAddress()))
	}
	if req.GetHttpProxy() != "" {
		flags = append(flags, fmt.Sprintf("--http-proxy %s", req.GetHttpProxy()))
	}
	if req.GetHttpsProxy() != "" {
		flags = append(flags, fmt.Sprintf("--https-proxy %s", req.GetHttpsProxy()))
	}
	if req.GetNoProxy() != "" {
		flags = append(flags, fmt.Sprintf("--no-proxy %s", req.GetNoProxy()))
	}
	if req.GetTelemetry() {
		flags = append(flags, "--telemetry")
	}

	script := fmt.Sprintf("curl -sSL https://get.pivox.app/agent | bash -s -- --token %s", gw.RegistrationToken)
	if len(flags) > 0 {
		script += " " + strings.Join(flags, " ")
	}

	return &storagev1.GetInstallScriptResponse{
		Script: script,
	}, nil
}

func (s *StorageGatewaysServer) GetUninstallScript(ctx context.Context, req *storagev1.GetUninstallScriptRequest) (*storagev1.GetUninstallScriptResponse, error) {
	// Validate the resource name exists.
	orgName, gwName, err := parseStorageGatewayName(req.GetName())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	org, err := s.queries.GetOrganizationByName(ctx, orgName)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", orgName)
	}

	_, err = s.queries.GetStorageGatewayByName(ctx, db.GetStorageGatewayByNameParams{
		OrgID: org.ID,
		Name:  gwName,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", req.GetName())
	}

	script := "curl -sSL https://get.pivox.app/agent/uninstall | bash"

	return &storagev1.GetUninstallScriptResponse{
		Script: script,
	}, nil
}

func (s *StorageGatewaysServer) UpgradeGateway(_ context.Context, _ *storagev1.UpgradeGatewayRequest) (*longrunningpb.Operation, error) {
	return nil, status.Errorf(codes.Unimplemented, "UpgradeGateway not yet implemented")
}
