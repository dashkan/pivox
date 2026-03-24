package filter

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// QueryParams holds input for a filtered query.
type QueryParams struct {
	Filter      string // AIP-160 filter expression
	ParentID    string // parent UUID (resolved by caller) or full resource name for tag_bindings
	OrderBy     string // AIP-132 order_by expression
	PageSize    int32
	Cursor      string // page token (number-based pagination)
	ShowDeleted bool   // if true, include soft-deleted rows
}

// Query builds and executes a filtered SELECT query against the given resource table.
func Query(ctx context.Context, dbtx db.DBTX, rf *ResourceFilter, params QueryParams) (pgx.Rows, error) {
	var (
		conditions []string
		args       []any
		paramIdx   = 1
	)

	// Soft-delete filter.
	if rf.SoftDelete && !params.ShowDeleted {
		conditions = append(conditions, "delete_time IS NULL")
	}

	// Parent filter.
	if params.ParentID != "" && rf.ParentColumn != "" {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", rf.ParentColumn, paramIdx))
		args = append(args, params.ParentID)
		paramIdx++
	}

	// AIP-160 filter.
	if params.Filter != "" {
		wc, err := Transpile(rf, params.Filter, paramIdx)
		if err != nil {
			return nil, err
		}
		if wc.SQL != "" {
			conditions = append(conditions, wc.SQL)
			args = append(args, wc.Args...)
			paramIdx += len(wc.Args)
		}
	}

	// Cursor pagination (UUID-based, UUIDv7 is time-ordered).
	if params.Cursor != "" {
		conditions = append(conditions, fmt.Sprintf("%s > $%d", rf.CursorColumn, paramIdx))
		args = append(args, params.Cursor)
		paramIdx++
	}

	// Page size.
	pageSize := params.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	limit := pageSize + 1

	// Build the query.
	var sb strings.Builder
	sb.WriteString("SELECT * FROM ")
	sb.WriteString(rf.Table)

	if len(conditions) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conditions, " AND "))
	}

	orderBy := rf.OrderBy
	if params.OrderBy != "" {
		parsed, err := ParseOrderBy(rf, params.OrderBy)
		if err != nil {
			return nil, err
		}
		orderBy = parsed
	}
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}

	sb.WriteString(fmt.Sprintf(" LIMIT $%d", paramIdx))
	args = append(args, limit)

	return dbtx.Query(ctx, sb.String(), args...)
}
