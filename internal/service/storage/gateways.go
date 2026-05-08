package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/timestamppb"

	"log/slog"

	"github.com/dashkan/pivox/internal/agentstream"
	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/audit"
	"github.com/dashkan/pivox/internal/convert"
	"github.com/dashkan/pivox/internal/crypto"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	"github.com/dashkan/pivox/internal/permission"
	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	storagev1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/storage/v1"
	typespb "github.com/dashkan/pivox/internal/pkg/gen/pivox/types"
	"github.com/dashkan/pivox/internal/resource"
	"github.com/dashkan/pivox/internal/server"
)

type StorageGatewaysServer struct {
	storagev1.UnimplementedStorageGatewaysServer
	queries           db.Querier
	encryptor         crypto.Encryptor
	conns             *agentstream.ConnectionManager
	audit             *audit.Resolver
	sessionSigningKey []byte
}

// StorageGatewaysConfig is the constructor input for
// StorageGatewaysServer.
type StorageGatewaysConfig struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Encryptor wraps Cloud KMS for column-level encryption.
	// Optional.
	Encryptor crypto.Encryptor
	// Conns tracks connected agents and routes outbound messages.
	// Required.
	Conns *agentstream.ConnectionManager
	// AuditResolver inflates audit-field UUIDs into Actor protos.
	// Optional; nil leaves Actor fields unset.
	AuditResolver *audit.Resolver
}

// NewStorageGatewaysServer constructs the server from cfg. Panics on
// a missing required field.
func NewStorageGatewaysServer(cfg StorageGatewaysConfig) *StorageGatewaysServer {
	if cfg.Queries == nil {
		panic("storage: StorageGatewaysConfig.Queries is required")
	}
	if cfg.Conns == nil {
		panic("storage: StorageGatewaysConfig.Conns is required")
	}
	return &StorageGatewaysServer{
		queries:           cfg.Queries,
		encryptor:         cfg.Encryptor,
		conns:             cfg.Conns,
		audit:             cfg.AuditResolver,
		sessionSigningKey: []byte("pivox-dev-session-signing-key-do-not-use-in-prod"), // TODO: load from key management system in prod
	}
}

// resolveGatewayActors gathers created_by/updated_by UUIDs across the
// page and resolves them in a single batched call.
func (s *StorageGatewaysServer) resolveGatewayActors(ctx context.Context, rows []db.StorageGateway) (map[uuid.UUID]*typespb.Actor, error) {
	if s.audit == nil {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows)*2)
	for _, r := range rows {
		if r.CreatedBy.Valid {
			ids = append(ids, r.CreatedBy.Bytes)
		}
		if r.UpdatedBy.Valid {
			ids = append(ids, r.UpdatedBy.Bytes)
		}
	}
	actors, err := s.audit.Resolve(ctx, ids)
	if err != nil {
		slog.ErrorContext(ctx, "resolve gateway actors failed", "error", err)
		return nil, apierr.Internal("resolve actors")
	}
	return actors, nil
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
		CreatedBy:         convert.PgUUID(server.MustPivoxUserID(ctx)),
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "StorageGateway", gwName)
	}

	actors, resolveErr := s.resolveGatewayActors(ctx, []db.StorageGateway{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "create gateway: actor resolution failed; returning proto without audit actors",
			"gateway_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.StorageGatewayToProto(result, orgName, actors))
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

	actors, err := s.resolveGatewayActors(ctx, []db.StorageGateway{gw})
	if err != nil {
		return nil, err
	}
	return convert.StorageGatewayToProto(gw, orgName, actors), nil
}

func (s *StorageGatewaysServer) ListStorageGateways(_ context.Context, _ *storagev1.ListStorageGatewaysRequest) (*storagev1.ListStorageGatewaysResponse, error) {
	return nil, apierr.Unimplemented("ListStorageGateways not yet implemented")
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
		UpdatedBy: convert.PgUUID(server.MustPivoxUserID(ctx)),
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
					return nil, apierr.Internal("failed to marshal annotations")
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

	actors, resolveErr := s.resolveGatewayActors(ctx, []db.StorageGateway{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "update gateway: actor resolution failed; returning proto without audit actors",
			"gateway_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return lro.DoneOperation(convert.StorageGatewayToProto(result, orgName, actors))
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

	actors, resolveErr := s.resolveGatewayActors(ctx, []db.StorageGateway{result})
	if resolveErr != nil {
		slog.WarnContext(ctx, "rotate registration token: actor resolution failed; returning proto without audit actors",
			"gateway_id", result.ID, "error", resolveErr)
		actors = nil
	}
	return convert.StorageGatewayToProto(result, orgName, actors), nil
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
	return nil, apierr.Unimplemented("UpgradeGateway not yet implemented")
}

func (s *StorageGatewaysServer) CreateStorageSession(ctx context.Context, req *storagev1.CreateStorageSessionRequest) (*storagev1.CreateStorageSessionResponse, error) {
	// Resolve parent → orgID. Returns NotFound for an unknown
	// organization slug; the caller can't probe org existence beyond
	// what GetOrganization already exposes (membership-gated).
	orgID, err := resource.ResolveOrgParent(ctx, s.queries, req.GetParent())
	if err != nil {
		return nil, err
	}
	orgSlug, err := resource.ParseSegment(req.GetParent())
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}

	// Determine the caller's effective access level at `parent`.
	//
	// The pattern shape branches on whether the caller has
	// org-wide storage read access via an org-role binding, or
	// only per-space access:
	//
	//   - Org-wide: any org-role at `parent` that the permission
	//     catalog says grants `assets.assets.read`. Emits a single
	//     `/{endpoint}/{org-slug}/*` per endpoint (O(endpoints)
	//     wire weight, dynamically covers spaces added during
	//     the session's TTL, matches the org-role semantic).
	//
	//   - Per-space: caller has no qualifying org-role but does
	//     have direct or group-mediated `space_members` rows in
	//     `parent`. Emits the cross-product
	//     `/{endpoint}/{org-slug}/{space-slug}/*` per (endpoint,
	//     space) pair.
	//
	//   - Neither: PermissionDenied. Covers both "caller is not in
	//     the org at all" and "caller is in the org under no role
	//     that grants storage read AND has no per-space binding."
	//
	// The trigger is catalog-driven (read from
	// `permission.matrix`, not enumerated by role name), so a
	// future role added without storage-read won't accidentally
	// trigger the org-wide branch. Citations:
	// `internal/permission/permissions_gen.go:381` (RoleViewer's
	// AssetsAssetsRead grant; equivalent rows in the Owner / Admin
	// / Editor blocks). All four current system roles include
	// storage read, so any org-member today gets org-wide patterns.
	identityID := server.MustPivoxUserID(ctx)
	orgRoles, err := s.queries.GetEffectiveOrgRoles(ctx, db.GetEffectiveOrgRolesParams{
		OrgID:      orgID,
		IdentityID: convert.PgUUID(identityID),
	})
	if err != nil {
		slog.ErrorContext(ctx, "get effective org roles for storage session", "error", err, "org_id", orgID)
		return nil, apierr.Internal("get effective org roles")
	}
	hasOrgWideRead := false
	for _, role := range orgRoles {
		if permission.Has(role, permission.AssetsAssetsRead) {
			hasOrgWideRead = true
			break
		}
	}

	// If the caller has no qualifying org-role, fall back to direct
	// per-space membership — DB query, mirrors GetEffectiveSpaceRoles'
	// resolution shape but inverted to "for this user, which spaces?"
	var spaces []db.ListSpaceMembershipsForIdentityInOrgRow
	if !hasOrgWideRead {
		spaces, err = s.queries.ListSpaceMembershipsForIdentityInOrg(ctx, db.ListSpaceMembershipsForIdentityInOrgParams{
			OrgID:      orgID,
			IdentityID: convert.PgUUID(identityID),
		})
		if err != nil {
			slog.ErrorContext(ctx, "list space memberships for storage session", "error", err, "org_id", orgID)
			return nil, apierr.Internal("list space memberships")
		}
		if len(spaces) == 0 {
			return nil, apierr.PermissionDenied("caller has no storage access in the requested organization; storage session cannot be minted")
		}
	}

	// Enumerate the org's endpoint short names. Patterns multiply
	// across (endpoint × space): the agent's HTTP router keys on
	// the leading path segment (the endpoint short name) before
	// session.Authorize sees `r.URL.Path`, so patterns must include
	// the endpoint segment.
	//
	// Atomicity: this read is NOT in the same transaction as the
	// membership query above. A space added or endpoint added
	// between the two reads produces a session that's missing the
	// new (space, endpoint) pair until the next session mint. TTL
	// is 1h default, so self-correction is bounded. Acceptable today;
	// not worth a transaction (both queries are read-only).
	endpointNames, err := s.queries.ListStorageEndpointShortNamesByOrg(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "list endpoint short names for storage session", "error", err, "org_id", orgID)
		return nil, apierr.Internal("list endpoint short names")
	}
	if len(endpointNames) == 0 {
		// Symmetric to the no-spaces rejection above: minting a
		// session with zero patterns produces a token that
		// authorizes nothing — confusing UX. Surface as
		// FailedPrecondition so the operator's response is "create
		// an endpoint" rather than "fix the user's permissions."
		return nil, apierr.FailedPrecondition("organization has no storage endpoints; storage session cannot be minted")
	}

	// Defensive validation on the path segments that get
	// interpolated into pattern strings. Today there is NO proto-
	// level buf.validate constraint on the endpoint / space / org
	// name fields (no string.pattern in any api/proto/* file as of
	// this commit), so a `*`, `/`, or `..` in any of these segments
	// would silently produce a malformed pattern. Reject up front
	// rather than mint a token whose pattern shape is undefined.
	// The proto-level gap is the better fix and is filed as the
	// follow-up to #27 phase 2; this defense is the in-scope
	// mitigation.
	if err := validatePathSegment(orgSlug, "organization slug"); err != nil {
		return nil, apierr.Internal(err.Error())
	}
	for _, ep := range endpointNames {
		if err := validatePathSegment(ep, "endpoint name"); err != nil {
			return nil, apierr.Internal(err.Error())
		}
	}
	for _, sp := range spaces {
		if err := validatePathSegment(sp.Name, "space name"); err != nil {
			return nil, apierr.Internal(err.Error())
		}
	}

	// Generate opaque token.
	token := uuid.New().String()

	// Compute expiry (default 1 hour).
	ttl := time.Hour
	if req.GetTtl() != nil {
		ttl = req.GetTtl().AsDuration()
	}
	expiry := time.Now().Add(ttl)

	// Build patterns. Branches on hasOrgWideRead — never OR'd with
	// per-space because the org-wide pattern subsumes the per-space
	// ones (a future refactor accidentally OR'ing them would
	// silently double the wire weight without changing semantics).
	//
	// Org-wide:  `/{endpoint}/{org-slug}/*`
	// Per-space: `/{endpoint}/{org-slug}/{space-slug}/*`
	//
	// Both shapes use session.Authorize's `/*`-suffix prefix-match
	// (internal/storageagent/session.go:233). See docs/storage.md
	// and #83 for the storage_key alignment that makes the assets
	// pipeline produce paths matching either shape.
	var patterns []string
	if hasOrgWideRead {
		patterns = make([]string, 0, len(endpointNames))
		for _, ep := range endpointNames {
			patterns = append(patterns,
				fmt.Sprintf("/%s/%s/*", ep, orgSlug))
		}
	} else {
		patterns = make([]string, 0, len(endpointNames)*len(spaces))
		for _, ep := range endpointNames {
			for _, sp := range spaces {
				patterns = append(patterns,
					fmt.Sprintf("/%s/%s/%s/*", ep, orgSlug, sp.Name))
			}
		}
	}

	// Push SessionGrant scoped to the target org. Replaces the
	// cross-org SendToAll broadcast that was the original gap #27
	// motivates: a session minted for org A is now invisible to
	// agents in any other org. ConnectionManager.SendToOrg uses the
	// per-connection OrgID populated at handshake-registration time
	// (`agent_service.go` Connect handler), so this is a constant-
	// time filter — no DB lookup at send time.
	grant := &agentv1.ControlMessage{
		Id: uuid.New().String(),
		Message: &agentv1.ControlMessage_SessionGrant{
			SessionGrant: &agentv1.SessionGrant{
				Token:    token,
				Patterns: patterns,
				Expiry:   timestamppb.New(expiry),
			},
		},
	}
	s.conns.SendToOrg(orgID, grant)

	// Mint JWT.
	jwt := mintSessionJWT(token, expiry, s.sessionSigningKey)

	// Set cookie via gRPC metadata for browser flows.
	cookie := fmt.Sprintf("pivox_session=%s; Domain=.pivox.app; Path=/; Secure; HttpOnly; SameSite=Lax; Max-Age=%d", jwt, int(ttl.Seconds()))
	_ = grpc.SetHeader(ctx, metadata.Pairs("set-cookie", cookie))

	// Native clients (macOS, future WinUI) read the JWT from the
	// response body and attach it as Authorization: Bearer on
	// subsequent storage requests. The token is the same value as
	// the cookie's pivox_session= attribute; only the transport
	// differs.
	return &storagev1.CreateStorageSessionResponse{
		Expiry: timestamppb.New(expiry),
		Token:  jwt,
	}, nil
}

// validatePathSegment rejects strings that would produce a malformed
// or attacker-controllable URL pattern when interpolated into
// /{endpoint}/{org}/{space}/* shapes. Defensive — proto-level
// validation is the better long-term fix (filed as a follow-up to
// #27 phase 2). Rejected: empty, contains '/', contains '*', equals
// '.' or '..', leading '.'.
func validatePathSegment(s, kind string) error {
	if s == "" {
		return fmt.Errorf("%s is empty; pattern shape would be malformed", kind)
	}
	if strings.ContainsAny(s, "/*") {
		return fmt.Errorf("%s %q contains illegal character ('/' or '*')", kind, s)
	}
	if s == "." || s == ".." || strings.HasPrefix(s, ".") {
		return fmt.Errorf("%s %q has illegal leading-dot shape", kind, s)
	}
	return nil
}

func mintSessionJWT(token string, expiry time.Time, signingKey []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := fmt.Sprintf(`{"token":"%s","exp":%d}`, token, expiry.Unix())
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))

	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(header + "." + payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return header + "." + payload + "." + sig
}
