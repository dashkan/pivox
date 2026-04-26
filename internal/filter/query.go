package filter

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox/internal/appkey"
	db "github.com/dashkan/pivox/internal/db/generated"
)

// QueryParams holds input for a filtered query.
type QueryParams struct {
	Filter      string // AIP-160 filter expression
	ParentID    string // parent UUID (resolved by caller) or full resource name for tag_bindings
	UserID      string // authenticated user id; required iff rf.UserColumn is set
	OrderBy     string // AIP-132 order_by expression
	PageSize    int32
	Cursor      string        // encrypted page_token from the client (opaque)
	ShowDeleted bool          // if true, include soft-deleted rows
	Codec       *appkey.Codec // required when Cursor is non-empty; used to decrypt it
}

// Query builds and executes a filtered SELECT query against the given resource table.
func Query(ctx context.Context, dbtx db.DBTX, rf *ResourceFilter, params QueryParams) (pgx.Rows, error) {
	sql, args, err := buildQuery(rf, params)
	if err != nil {
		return nil, err
	}
	return dbtx.Query(ctx, sql, args...)
}

// buildQuery assembles the final SQL + args. Separated from Query so the SQL
// assembly is unit-testable without a real DB connection.
func buildQuery(rf *ResourceFilter, params QueryParams) (string, []any, error) {
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

	// User-scoped access-control predicate. Always applied when the resource
	// declares a UserColumn. A missing UserID in that case is a server
	// misconfiguration — we refuse to return unscoped rows.
	if rf.UserColumn != "" {
		if params.UserID == "" {
			return "", nil, fmt.Errorf("filter: UserID required when ResourceFilter.UserColumn is set (%s)", rf.UserColumn)
		}
		conditions = append(conditions, fmt.Sprintf("%s = $%d", rf.UserColumn, paramIdx))
		args = append(args, params.UserID)
		paramIdx++
	}

	// AIP-160 filter.
	if params.Filter != "" {
		wc, err := Transpile(rf, params.Filter, paramIdx)
		if err != nil {
			return "", nil, err
		}
		if wc.SQL != "" {
			conditions = append(conditions, wc.SQL)
			args = append(args, wc.Args...)
			paramIdx += len(wc.Args)
		}
	}

	// Cursor pagination. params.Cursor is the opaque client-visible
	// page_token; decrypt first. Direction matches the cursor column's
	// sort order (ASC → `> $cursor` for next-newer; DESC → `< $cursor`
	// for next-older).
	if params.Cursor != "" {
		rawCursor, err := decodeCursor(params.Codec, params.Cursor)
		if err != nil {
			return "", nil, err
		}
		op := ">"
		if strings.EqualFold(rf.CursorDirection, "DESC") {
			op = "<"
		}
		conditions = append(conditions, fmt.Sprintf("%s %s $%d", rf.CursorColumn, op, paramIdx))
		args = append(args, rawCursor)
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

	// Assemble SQL.
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
			return "", nil, err
		}
		orderBy = parsed
	}
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}

	fmt.Fprintf(&sb, " LIMIT $%d", paramIdx)
	args = append(args, limit)

	return sb.String(), args, nil
}
