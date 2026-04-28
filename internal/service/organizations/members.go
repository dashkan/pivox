package organizations

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/permission"
	iampb "github.com/dashkan/pivox/internal/pkg/gen/pivox/iam/v1"
)

// org-scope Member handlers. Member CRUD lives here (rather than on
// a shared `Iam` service) because the ≥1-owner boundary check and
// the org-only TransferOwnership semantic make these org-shaped
// operations. Space-scope Member handlers live on the Spaces service
// and operate on `space_members` instead.
//
// The Member proto type is shared (`pivox.iam.v1.Member`); only the
// URL pattern + DB table dispatch differs per scope.

// orgMemberPath captures the parsed pieces of an org-scope Member
// resource name: `organizations/{org}/members/{member}` where
// `{member}` is `user-{uuid}` or `group-{uuid}`.
type orgMemberPath struct {
	orgSlug       string
	principalKind db.PrincipalKind
	principalID   uuid.UUID
}

// parseOrgMemberName parses `organizations/{org}/members/{member}`.
// The URL pattern at the gRPC layer already constrains the shape, but
// we re-parse defensively in case grpc-gateway passes through a
// malformed name.
func parseOrgMemberName(name string) (orgMemberPath, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "members" || parts[1] == "" || parts[3] == "" {
		return orgMemberPath{}, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member name %q: expected organizations/{org}/members/{member}", name)))
	}
	kind, id, err := permission.ParseMemberSegment(parts[3])
	if err != nil {
		return orgMemberPath{}, err
	}
	return orgMemberPath{orgSlug: parts[1], principalKind: kind, principalID: id}, nil
}

// parseOrgMemberParent parses `organizations/{org}` (the parent for
// org-scope Member listing/creation). Returns the org slug.
func parseOrgMemberParent(parent string) (string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 2 || parts[0] != "organizations" || parts[1] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/{org}", parent)))
	}
	return parts[1], nil
}

// GetMember resolves an org-scope Member by resource name. Reads from
// the `org_members` table; space-scope members live on the Spaces
// service.
func (s *OrganizationsServer) GetMember(ctx context.Context, req *iampb.GetMemberRequest) (*iampb.Member, error) {
	path, err := parseOrgMemberName(req.GetName())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, path.orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	row, err := s.queries.GetOrgMember(ctx, db.GetOrgMemberParams{
		OrgID:         org.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	return convert.OrgMemberRowToProto(row, path.orgSlug), nil
}

// ListMembers returns all org-scope Members. v1 returns up to 1000
// rows (the SQL LIMIT) without pagination — system-role member
// counts in normal orgs are far below that ceiling.
func (s *OrganizationsServer) ListMembers(ctx context.Context, req *iampb.ListMembersRequest) (*iampb.ListMembersResponse, error) {
	orgSlug, err := parseOrgMemberParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}
	rows, err := s.queries.ListOrgMembers(ctx, org.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list org members failed", "org_id", org.ID, "error", err)
		return nil, apierr.Internal("list members")
	}
	out := make([]*iampb.Member, len(rows))
	for i, r := range rows {
		out[i] = convert.OrgMemberToProto(r, orgSlug)
	}
	return &iampb.ListMembersResponse{Members: out}, nil
}
