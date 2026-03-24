package resource

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/apierr"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// ParseSegment extracts the segment after the collection prefix.
// e.g. "organizations/meridian" → "meridian"
func ParseSegment(name string) (string, error) {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[1] == "" {
		return "", fmt.Errorf("invalid resource name %q", name)
	}
	return parts[1], nil
}

// CollectionFromName extracts the collection prefix from a resource name.
// e.g. "organizations/meridian" → "organizations"
func CollectionFromName(name string) string {
	parts := strings.SplitN(name, "/", 2)
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

// ResolveOrgParent resolves an "organizations/{name}" parent to its UUID.
func ResolveOrgParent(ctx context.Context, queries *db.Queries, parent string) (uuid.UUID, error) {
	collection := CollectionFromName(parent)
	if collection != "organizations" {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: expected organizations/*", parent)))
	}

	segment, err := ParseSegment(parent)
	if err != nil {
		return uuid.Nil, apierr.InvalidArgument(apierr.FieldViolation("parent",
			fmt.Sprintf("invalid parent %q: %v", parent, err)))
	}

	org, err := queries.GetOrganizationByName(ctx, segment)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, apierr.NotFound("parent", parent)
		}
		return uuid.Nil, apierr.Internal("failed to validate parent")
	}

	return org.ID, nil
}
