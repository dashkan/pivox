package spaces

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
)

// TestIamPermissions returns the subset of `req.permissions` the
// caller is allowed against the given space. Resolution unions
// direct space-level bindings with parent-org-level bindings — an
// org-admin is implicitly a space-admin without an explicit
// space-scope Member row. This is the meaningful divergence from
// `Organizations.TestIamPermissions`, which resolves only against
// org bindings.
//
// SECURITY: This RPC is in PermissionInterceptor's exempt set —
// answering "which permissions do I have" can't itself require a
// permission (would be circular). That means the gate that protects
// every other RPC does NOT run for this one. This handler MUST do
// its own caller-identity resolution (s.caller below) and MUST NOT
// trust any field on the request to identify the caller. Do not
// remove the s.caller call without auditing the entire control flow.
func (s *SpacesServer) TestIamPermissions(ctx context.Context, req *iampb.TestIamPermissionsRequest) (*iampb.TestIamPermissionsResponse, error) {
	identity, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	target, err := parseSpaceResourceTarget(req.GetResource())
	if err != nil {
		return nil, err
	}
	allowed, err := s.resolver.TestPermissions(ctx, identity, target, req.GetPermissions())
	if err != nil {
		slog.ErrorContext(ctx, "resolve test permissions failed", "resource", req.GetResource(), "error", err)
		return nil, apierr.Internal("resolve permissions")
	}
	return &iampb.TestIamPermissionsResponse{Permissions: allowed}, nil
}

// parseSpaceResourceTarget extracts a permission.Target from an
// `organizations/{org}/spaces/{space}` resource path. The trailing
// segment is parsed as a UUID — TestIamPermissions callers pass
// resolved IDs, not slugs.
func parseSpaceResourceTarget(resource string) (permission.Target, error) {
	parts := strings.Split(resource, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" || parts[1] == "" || parts[3] == "" {
		return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
			fmt.Sprintf("invalid resource %q: expected organizations/{org}/spaces/{space}", resource)))
	}
	id, err := uuid.Parse(parts[3])
	if err != nil {
		return permission.Target{}, apierr.InvalidArgument(apierr.FieldViolation("resource",
			fmt.Sprintf("invalid space id in resource %q: %v", resource, err)))
	}
	return permission.SpaceTarget(id), nil
}
