package organizations

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
	"github.com/dashkan/pivox/internal/server"
)

// TestIamPermissions returns the subset of `req.permissions` the
// caller is allowed against the given org resource. Used by UI
// clients for permission-gated affordance rendering ("which buttons
// should I enable?") with one round-trip instead of N HasPermission
// calls.
//
// Org-scope variant — resolves only against direct + group-derived
// org-level role bindings. The space-scope variant on the Spaces
// service unions inherited org bindings with direct space bindings;
// the two are different operations sharing a wire shape.
//
// SECURITY: This RPC is in PermissionInterceptor's exempt set —
// answering "which permissions do I have" can't itself require a
// permission (would be circular). That means the gate that protects
// every other RPC does NOT run for this one. This handler MUST do
// its own caller-identity resolution (s.caller below) and MUST NOT
// trust any field on the request to identify the caller. Do not
// remove the s.caller call without auditing the entire control flow.
//
// Returns Unauthenticated if the caller has no auth context. Returns
// the empty set (and OK) if the caller has no role bindings — UI
// treats that as "no permissions granted" and greys out everything.
func (s *OrganizationsServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	identity := server.MustUserID(ctx)
	target, err := parseOrgResourceTarget(req.GetResource())
	if err != nil {
		return nil, err
	}
	allowed, err := s.resolver.TestPermissions(ctx, identity, target, req.GetPermissions())
	if err != nil {
		slog.ErrorContext(ctx, "resolve test permissions failed", "resource", req.GetResource(), "error", err)
		return nil, apierr.Internal(err, "resolve permissions")
	}
	return &iampb.TestIamPermissionsResponse{Permissions: allowed}, nil
}

// parseOrgResourceTarget extracts a permission.Target from an
// `organizations/{org}` resource path. The trailing segment is parsed
// as a UUID — TestIamPermissions callers pass resolved IDs, not slugs
// (the org already exists by the time the UI is asking "what can I
// do here?"). The URL pattern at the wire layer already enforces the
// shape; this re-parses defensively for grpc-gateway pass-through.
func parseOrgResourceTarget(resource string) (permission.Target, error) {
	parts := strings.Split(resource, "/")
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
			fmt.Sprintf("invalid resource %q: expected organizations/{org}", resource)))
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
			fmt.Sprintf("invalid org id in resource %q: %v", resource, err)))
	}
	return permission.OrgTarget(id), nil
}
