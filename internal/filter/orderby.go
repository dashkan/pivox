package filter

import (
	"fmt"
	"strings"
)

// ParseOrderBy parses an AIP-132 order_by string into a SQL ORDER BY clause.
// The input is a comma-separated list of fields, each optionally followed by
// "asc" or "desc". Field names are mapped to SQL column names via the
// ResourceFilter's field mappings. The special field "name" is always allowed.
//
// Examples:
//
//	"displayName"            → "display_name ASC"
//	"createTime desc, name"  → "create_time DESC, name ASC"
func ParseOrderBy(rf *ResourceFilter, orderBy string) (string, error) {
	parts := strings.Split(orderBy, ",")
	var clauses []string

	for _, part := range parts {
		fields := strings.Fields(strings.TrimSpace(part))
		if len(fields) == 0 {
			continue
		}

		fieldName := fields[0]
		direction := "ASC"

		if len(fields) == 2 {
			switch strings.ToLower(fields[1]) {
			case "asc":
				direction = "ASC"
			case "desc":
				direction = "DESC"
			default:
				return "", fmt.Errorf("invalid order direction %q for field %q; must be \"asc\" or \"desc\"", fields[1], fieldName)
			}
		} else if len(fields) > 2 {
			return "", fmt.Errorf("invalid order_by term %q", part)
		}

		// "name" is always orderable (it's the primary key / resource name).
		if fieldName == "name" {
			clauses = append(clauses, fmt.Sprintf("name %s", direction))
			continue
		}

		fm, ok := rf.Fields[fieldName]
		if !ok {
			return "", fmt.Errorf("invalid order_by field %q", fieldName)
		}
		// JSONB map fields cannot be ordered.
		if fm.JSONB {
			return "", fmt.Errorf("field %q does not support ordering", fieldName)
		}

		clauses = append(clauses, fmt.Sprintf("%s %s", fm.Column, direction))
	}

	if len(clauses) == 0 {
		return "", nil
	}

	return strings.Join(clauses, ", "), nil
}
