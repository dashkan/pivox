package permission

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/dashkan/pivox/internal/apierr"
)

// PrincipalKind discriminates the parsed Member resource name's
// typed-prefix segment. The polymorphic SQL discriminator (the
// `principal_kind` enum) was retired when org_members/space_members
// switched to typed `user_id`/`group_id` columns; this Go-side enum
// stays because the wire-level Member resource name still uses
// `user-{uuid}` / `group-{uuid}` prefixes that callers parse.
type PrincipalKind string

const (
	PrincipalKindUser  PrincipalKind = "user"
	PrincipalKindGroup PrincipalKind = "group"
)

// ParseMemberSegment splits a Member resource name's typed-prefix
// `{member}` segment into (principal_kind, principal_id). Reused by
// both the org-scope (Organizations service) and space-scope (Spaces
// service) Member handlers — the segment shape is identical at
// every scope (`user-{uuid}` or `group-{uuid}`).
//
// Lives in `internal/permission` because the {kind, id} pair drives
// the resolver's effective-role lookup, not because permissions are
// directly involved in parsing — but it's the closest existing home
// for IAM-shaped types and avoids creating a new package for one
// pure function.
//
// Returns InvalidArgument-shaped errors so handlers can surface them
// without re-mapping.
func ParseMemberSegment(seg string) (PrincipalKind, uuid.UUID, error) {
	idx := strings.IndexByte(seg, '-')
	if idx <= 0 || idx == len(seg)-1 {
		return "", uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member segment %q: expected user-{uuid} or group-{uuid}", seg)))
	}
	prefix, idStr := seg[:idx], seg[idx+1:]
	id, err := uuid.Parse(idStr)
	if err != nil {
		return "", uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member uuid %q in segment %q: %v", idStr, seg, err)))
	}
	switch prefix {
	case "user":
		return PrincipalKindUser, id, nil
	case "group":
		return PrincipalKindGroup, id, nil
	default:
		return "", uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("name",
			fmt.Sprintf("invalid member prefix %q: expected user or group", prefix)))
	}
}
