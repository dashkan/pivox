package spaces

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

// space-scope Member handlers. Companion to the org-scope Member
// handlers on the Organizations service. Same wire shape (`Member`
// proto from iam/v1 is shared); different DB table (`space_members`)
// and no ≥1-owner boundary check.
//
// IMPORTANT: ListMembers here returns ONLY direct space-level
// bindings. Org-level inheritance (an org-admin acting at space
// scope) is resolved at runtime by the permission resolver and does
// NOT surface as a Member resource at the space scope — that would
// conflate authorization (resolver) with the canonical role-binding
// catalog (this list).

type spaceMemberPath struct {
	orgSlug       string
	spaceSlug     string
	principalKind db.PrincipalKind
	principalID   uuid.UUID
}

// parseSpaceMemberName parses
// `organizations/{org}/spaces/{space}/members/{member}` where
// `{member}` is `user-{uuid}` or `group-{uuid}`.
func parseSpaceMemberName(name string) (spaceMemberPath, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 6 || parts[0] != "organizations" || parts[2] != "spaces" || parts[4] != "members" ||
		parts[1] == "" || parts[3] == "" || parts[5] == "" {
		return spaceMemberPath{}, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member name %q: expected organizations/{org}/spaces/{space}/members/{member}", name)))
	}
	kind, id, err := permission.ParseMemberSegment(parts[5])
	if err != nil {
		return spaceMemberPath{}, err
	}
	return spaceMemberPath{
		orgSlug:       parts[1],
		spaceSlug:     parts[3],
		principalKind: kind,
		principalID:   id,
	}, nil
}

// parseSpaceMemberParent parses
// `organizations/{org}/spaces/{space}` (the parent for space-scope
// Member listing/creation). Returns (orgSlug, spaceSlug).
func parseSpaceMemberParent(parent string) (orgSlug, spaceSlug string, err error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "spaces" || parts[1] == "" || parts[3] == "" {
		return "", "", apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/{org}/spaces/{space}", parent)))
	}
	return parts[1], parts[3], nil
}

// GetMember resolves a space-scope Member by resource name. Reads
// from the `space_members` table only — does NOT include org-level
// inheritance (use TestIamPermissions for the unioned view of
// effective roles).
func (s *SpacesServer) GetMember(ctx context.Context, req *iampb.GetMemberRequest) (*iampb.Member, error) {
	path, err := parseSpaceMemberName(req.GetName())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, path.orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	space, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{
		OrgID: org.ID,
		Name:  path.spaceSlug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	row, err := s.queries.GetSpaceMember(ctx, db.GetSpaceMemberParams{
		SpaceID:       space.ID,
		PrincipalKind: path.principalKind,
		PrincipalID:   path.principalID,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Member", req.GetName())
	}
	return convert.SpaceMemberRowToProto(row, path.orgSlug, path.spaceSlug), nil
}

// ListMembers returns space-scope Members. Direct bindings only —
// see GetMember doc comment for the inheritance caveat.
func (s *SpacesServer) ListMembers(ctx context.Context, req *iampb.ListMembersRequest) (*iampb.ListMembersResponse, error) {
	orgSlug, spaceSlug, err := parseSpaceMemberParent(req.GetParent())
	if err != nil {
		return nil, err
	}
	org, err := s.queries.GetOrganizationByName(ctx, orgSlug)
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Organization", req.GetParent())
	}
	space, err := s.queries.GetSpaceByName(ctx, db.GetSpaceByNameParams{
		OrgID: org.ID,
		Name:  spaceSlug,
	})
	if err != nil {
		return nil, apierr.HandleResourceError(err, "Space", req.GetParent())
	}
	rows, err := s.queries.ListSpaceMembers(ctx, space.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list space members failed", "space_id", space.ID, "error", err)
		return nil, apierr.Internal("list members")
	}
	out := make([]*iampb.Member, len(rows))
	for i, r := range rows {
		out[i] = convert.SpaceMemberToProto(r, orgSlug, spaceSlug)
	}
	return &iampb.ListMembersResponse{Members: out}, nil
}
