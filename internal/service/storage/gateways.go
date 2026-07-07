package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
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
	pool              db.TxBeginner
	queries           db.Querier
	encryptor         crypto.Encryptor
	conns             *agentstream.ConnectionManager
	audit             *audit.Resolver
	sessionSigningKey []byte
	maxSessionTTL     time.Duration
	cookieDomain      string
}

// defaultMaxSessionTTL is the cap applied when StorageGatewaysConfig
// .MaxSessionTTL is the zero value. Chosen to cover a typical
// workday without forcing mid-day refresh, while keeping the
// post-revocation window bounded. Tracked in #27.
const defaultMaxSessionTTL = 8 * time.Hour

// defaultSessionTTL is the TTL applied when CreateStorageSession's
// caller does not supply one. Matches the implicit "hourly" cadence
// documented at internal/storageagent/session.go.
const defaultSessionTTL = 1 * time.Hour

// StorageGatewaysConfig is the constructor input for
// StorageGatewaysServer.
type StorageGatewaysConfig struct {
	// Pool begins transactions for tx-wrapped writes (used by the
	// validate_only dry-run path). *pgxpool.Pool satisfies it. Required.
	Pool db.TxBeginner
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
	// MaxSessionTTL caps the lifetime of a CreateStorageSession-
	// minted session. A caller-supplied TTL above this is silently
	// clamped (per AIP — best-effort honor of caller intent).
	// Optional; zero-value falls back to defaultMaxSessionTTL (8h).
	MaxSessionTTL time.Duration
	// CookieDomain is the value emitted in the Set-Cookie response
	// header's `Domain=` attribute for browser session cookies.
	// Optional; zero-value omits the Domain attribute entirely so
	// the cookie scopes to the response origin only (right default
	// for self-hosted; SaaS deployments configure ".pivox.app" or
	// equivalent).
	CookieDomain string

	// SessionSigningKey is the HMAC secret used to sign session
	// JWTs minted by CreateStorageSession. Optional; zero-value
	// falls back to a hardcoded dev literal (NOT FOR PRODUCTION).
	//
	// The same value MUST be threaded into AgentServiceConfig.
	// SessionSigningKey so HandshakeAck.session_signing_key carries
	// it to every connected agent. Without that wire, the
	// controller signs with one key and the agent's validateJWT
	// HMACs against another, and every storage request 401s. This
	// cross-phase invariant is what the cumulative #27 audit
	// caught and is now config-driven so main.go can declare the
	// key once and pass it to both servers.
	//
	// Production key loading is tracked separately in #24 (KMS).
	SessionSigningKey []byte
}

// NewStorageGatewaysServer constructs the server from cfg. Panics on
// a missing required field.
func NewStorageGatewaysServer(cfg StorageGatewaysConfig) *StorageGatewaysServer {
	if cfg.Pool == nil {
		panic("storage: StorageGatewaysConfig.Pool is required")
	}
	if cfg.Queries == nil {
		panic("storage: StorageGatewaysConfig.Queries is required")
	}
	if cfg.Conns == nil {
		panic("storage: StorageGatewaysConfig.Conns is required")
	}
	maxTTL := cfg.MaxSessionTTL
	if maxTTL <= 0 {
		maxTTL = defaultMaxSessionTTL
	}
	signingKey := cfg.SessionSigningKey
	if len(signingKey) == 0 {
		// Dev/test fallback. Production callers MUST pass the key
		// explicitly so it can also be plumbed to AgentServiceConfig
		// (the HandshakeAck wire). Tracked: #24.
		signingKey = []byte("pivox-dev-session-signing-key-do-not-use-in-prod")
	}
	return &StorageGatewaysServer{
		pool:              cfg.Pool,
		queries:           cfg.Queries,
		encryptor:         cfg.Encryptor,
		conns:             cfg.Conns,
		audit:             cfg.AuditResolver,
		sessionSigningKey: signingKey,
		maxSessionTTL:     maxTTL,
		cookieDomain:      cfg.CookieDomain,
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
		return nil, apierr.Internal(err, "resolve actors")
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

	// validate_only runs the INSERT against real constraints and rolls it
	// back, so a would-fail request (e.g. duplicate name) returns the same
	// error a live one would while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.StorageGateway, error) {
		return qtx.CreateStorageGateway(ctx, db.CreateStorageGatewayParams{
			ID:                uuid.New(),
			OrgID:             orgID,
			Name:              gwName,
			DisplayName:       gw.GetDisplayName(),
			IpAddresses:       gw.GetIpAddresses(),
			RegistrationToken: registrationToken,
			Hostname:          hostname,
			Annotations:       annotationsJSON,
			CreatedBy:         convert.PgUUID(server.MustUserID(ctx)),
		})
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
		UpdatedBy: convert.PgUUID(server.MustUserID(ctx)),
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
					return nil, apierr.Internal(err, "failed to marshal annotations")
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

	// validate_only runs the UPDATE against real constraints and rolls it
	// back, so a would-fail request returns the same error a live one would
	// while persisting nothing.
	result, err := db.RunInTxValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) (db.StorageGateway, error) {
		return qtx.UpdateStorageGateway(ctx, updateParams)
	})
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

	// validate_only runs the DELETE against real state and rolls it back,
	// so a would-fail request returns the same error a live one would while
	// persisting nothing.
	if err := db.RunInTxVoidValidate(ctx, s.pool, req.GetValidateOnly(), func(qtx db.Querier) error {
		return qtx.DeleteStorageGateway(ctx, existing.ID)
	}); err != nil {
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
	// The trigger is catalog-driven: it's read from the identity's
	// effective org permissions (resolved via `role_permissions`),
	// not enumerated by role name. So a future role added without
	// storage-read won't accidentally trigger the org-wide branch.
	// All four current system roles include storage read, so any
	// org-member today gets org-wide patterns.
	identityID := server.MustUserID(ctx)
	orgPerms, err := s.queries.EffectiveOrgPermissions(ctx, db.EffectiveOrgPermissionsParams{
		OrgID:      orgID,
		IdentityID: convert.PgUUID(identityID),
	})
	if err != nil {
		slog.ErrorContext(ctx, "get effective org permissions for storage session", "error", err, "org_id", orgID)
		return nil, apierr.Internal(err, "get effective org permissions")
	}
	hasOrgWideRead := slices.Contains(orgPerms, permission.AssetsAssetsRead)

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
			return nil, apierr.Internal(err, "list space memberships")
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
		return nil, apierr.Internal(err, "list endpoint short names")
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
		return nil, apierr.Internal(err, err.Error())
	}
	for _, ep := range endpointNames {
		if err := validatePathSegment(ep, "endpoint name"); err != nil {
			return nil, apierr.Internal(err, err.Error())
		}
	}
	for _, sp := range spaces {
		if err := validatePathSegment(sp.Name, "space name"); err != nil {
			return nil, apierr.Internal(err, err.Error())
		}
	}

	// Generate opaque token.
	token := uuid.New().String()

	// Compute expiry. Default is defaultSessionTTL (1h) when the
	// caller omits Ttl entirely. A caller-supplied positive TTL is
	// honored up to s.maxSessionTTL (clamped silently above, per
	// AIP — best-effort honor). A non-nil but non-positive Ttl is
	// rejected with InvalidArgument: silently coercing zero or
	// negative to a default would invert the caller's explicit
	// intent (e.g., a client that sends Ttl=0 to revoke fast
	// would instead receive an hour-long session).
	ttl := defaultSessionTTL
	if req.GetTtl() != nil {
		d := req.GetTtl().AsDuration()
		if d <= 0 {
			return nil, apierr.InvalidArgument(apierr.FieldViolation("ttl",
				"ttl must be positive when set"))
		}
		ttl = d
	}
	ttl = min(ttl, s.maxSessionTTL)
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
	// SendToOrg stamps this request's trace context into the grant so the
	// agent's SessionGrant handling joins the CreateStorageSession trace
	// (cloud -> agent causal link). No-op when tracing is disabled.
	s.conns.SendToOrg(ctx, orgID, grant)

	// Mint JWT. Claims include the caller's Pivox identity UUID
	// (sub) and the target org's slug (org) so gateway-side audit
	// logs can attribute requests without a directory lookup —
	// see #27 phase 5.
	jwt := mintSessionJWT(token, identityID.String(), orgSlug, expiry, s.sessionSigningKey)

	// Set cookie via gRPC metadata for browser flows. The Domain
	// attribute is conditional on s.cookieDomain being non-empty;
	// when unset, the cookie scopes to the response origin only
	// (right default for self-hosted deployments). Per-org
	// subdomains in SaaS deployments configure CookieDomain
	// accordingly.
	cookie := fmt.Sprintf("pivox_session=%s; ", jwt)
	if s.cookieDomain != "" {
		cookie += fmt.Sprintf("Domain=%s; ", s.cookieDomain)
	}
	cookie += fmt.Sprintf("Path=/; Secure; HttpOnly; SameSite=Lax; Max-Age=%d", int(ttl.Seconds()))
	_ = grpc.SetHeader(ctx, metadata.Pairs("set-cookie", cookie))

	// Native clients (macOS, future WinUI) read the JWT from the
	// response body and attach it as Authorization: Bearer on
	// subsequent storage requests. The token is the same value as
	// the cookie's pivox_session= attribute; only the transport
	// differs.
	return &storagev1.CreateStorageSessionResponse{
		ExpireTime: timestamppb.New(expiry),
		Token:      jwt,
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

// mintSessionJWT produces an HS256-signed JWT carrying the session
// claims a Pivox storage session needs:
//
//   - token: the opaque session UUID. Agents look up granted access
//     patterns by this value (the SessionGrant push carried the
//     same string); cookie-based authorization on the agent's HTTP
//     server splits on this claim.
//   - sub: the caller's Pivox identity UUID. Lets gateway-side audit
//     logs attribute requests without a directory lookup. Added in
//     #27 phase 5. (RFC 7519 Subject claim — preferred over the
//     issue's literal "user_id" so JWT-aware tooling sees the
//     standard name.)
//   - org: the target organization's slug. Same audit-attribution
//     purpose as sub; also documents which org's pattern set the
//     session is scoped to (matches the SessionGrant routing from
//     #27 phase 3). Added in #27 phase 5.
//   - exp: Unix-second expiry timestamp.
//
// Claims are emitted via json.Marshal of an explicit map rather
// than fmt.Sprintf'd JSON: the inputs are safe today (UUIDs +
// validated slugs), but Marshal is defense-in-depth against any
// future caller passing strings that need JSON-escaping.
func mintSessionJWT(token, identitySubject, orgSlug string, expiry time.Time, signingKey []byte) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	claims := map[string]any{
		"token": token,
		"sub":   identitySubject,
		"org":   orgSlug,
		"exp":   expiry.Unix(),
	}
	claimsBytes, _ := json.Marshal(claims) // map[string]any of UUIDs + slug + int64 cannot fail
	payload := base64.RawURLEncoding.EncodeToString(claimsBytes)

	mac := hmac.New(sha256.New, signingKey)
	mac.Write([]byte(header + "." + payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))

	return header + "." + payload + "." + sig
}
