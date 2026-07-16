package filter

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// OrderByPlan is the resolved, single-field AIP-132 order_by for a keyset list.
//
// A keyset-paginated list orders by at most ONE user-chosen column plus the id
// tiebreaker (`ORDER BY <col> <dir>, id <dir>`). The compound cursor encodes
// exactly that pair, so PlanOrderBy rejects a multi-field order_by — supporting
// N sort columns would need an N-tuple cursor, which the pilot deliberately
// does not build. An empty order_by yields the zero OrderByPlan (Field == ""),
// meaning "order by id only" with the simple 16-byte keyset.
type OrderByPlan struct {
	Field      string     // AIP field name; "" means default id ordering
	Column     string     // whitelisted SQL column for Field (never user input)
	Type       *expr.Type // column CEL type (drives timestamp cursor parsing)
	Descending bool       // true → DESC (flips both ORDER BY and the keyset op)
}

// PlanOrderBy resolves an AIP-132 order_by string against a resource's Sortable
// whitelist for the keyset path. Rules:
//
//   - "" → default id ordering (zero OrderByPlan).
//   - exactly one field, optionally followed by "asc"/"desc".
//   - the field MUST be in rf.Sortable; the resulting Column comes only from
//     that whitelist entry, never from the request.
//   - more than one comma-separated field → error (see OrderByPlan doc).
//
// Column/direction are structural, not values: the column is a fixed
// whitelisted identifier and the direction selects ASC/DESC — neither is ever a
// bound parameter, but neither can be attacker-chosen text either.
func PlanOrderBy(rf *ResourceFilter, orderBy string) (OrderByPlan, error) {
	orderBy = strings.TrimSpace(orderBy)
	if orderBy == "" {
		return OrderByPlan{}, nil
	}
	if strings.Contains(orderBy, ",") {
		return OrderByPlan{}, fmt.Errorf("keyset order_by supports a single field, got %q", orderBy)
	}

	fields := strings.Fields(orderBy)
	if len(fields) == 0 {
		return OrderByPlan{}, nil
	}
	if len(fields) > 2 {
		return OrderByPlan{}, fmt.Errorf("invalid order_by term %q", orderBy)
	}

	field := fields[0]
	descending := false
	if len(fields) == 2 {
		switch strings.ToLower(fields[1]) {
		case "asc":
			descending = false
		case "desc":
			descending = true
		default:
			return OrderByPlan{}, fmt.Errorf("invalid order direction %q for field %q; must be \"asc\" or \"desc\"", fields[1], field)
		}
	}

	sf, ok := rf.Sortable[field]
	if !ok {
		return OrderByPlan{}, fmt.Errorf("invalid order_by field %q", field)
	}
	return OrderByPlan{Field: field, Column: sf.Column, Type: sf.Type, Descending: descending}, nil
}

// KeysetCursor is a decoded page cursor: the primary sort column's value (nil
// for id-only ordering) plus the row id tiebreaker. Produce one with
// DecodeCursor.
type KeysetCursor struct {
	SortValue any // nil when ordering by id only; else string or time.Time
	ID        uuid.UUID
}

// Predicate is one ANDed base-scope condition supplied by the handler from the
// interceptor-resolved scope (org id, space id). SQL is a FIXED, trusted
// fragment containing exactly one %s that BuildListQuery replaces with the
// bound $N placeholder for Arg — e.g. {SQL: "org_id = %s", Arg: orgID} or
// {SQL: "space_id IS NOT DISTINCT FROM %s", Arg: spaceID}. Only literal SQL
// text belongs in SQL; the value travels in Arg and is always bound, never
// interpolated. This is the base scope the AIP filter layers ON TOP of — it can
// only narrow, never widen.
type Predicate struct {
	SQL string
	Arg any
}

// ListQuery describes a dynamically-assembled, keyset-paginated SELECT: a fixed
// base scope, an optional AIP-160 filter transpiled to a parameterized WHERE, a
// resolved single-field order_by, and a keyset cursor. Every value — base args,
// filter operands, cursor values, page size — is bound as a $N parameter;
// only the table name, column names (from the Sortable/Filterable whitelists)
// and the ASC/DESC direction are assembled into the SQL text, and those come
// solely from server-controlled declarations.
type ListQuery struct {
	Resource *ResourceFilter // supplies Table + the filter/sort whitelists
	Base     []Predicate     // ANDed base scope (always applied first)
	Filter   string          // AIP-160 filter expression (request field)
	Order    OrderByPlan     // resolved via PlanOrderBy
	PageSize int32           // clamped to [1,1000]; the query fetches PageSize+1
	Cursor   *KeysetCursor   // nil → first page
}

// BuildListQuery assembles the parameterized SQL and args for a keyset list.
// The returned SQL selects `*` (the caller scans with a resource Scan helper)
// and over-fetches by one row (LIMIT PageSize+1) so the caller can detect a
// further page. Errors originate only from the filter transpiler (bad user
// filter) — the caller maps them to InvalidArgument on "filter".
//
// Numbering discipline (the security contract): args are appended in emission
// order and each placeholder is `$len(args)` at the moment its value is
// appended, so base → filter → cursor → limit params stay aligned with the
// text. No value is ever formatted into the SQL string.
func BuildListQuery(q ListQuery) (string, []any, error) {
	if q.Resource == nil {
		return "", nil, fmt.Errorf("filter: ListQuery.Resource is required")
	}

	var (
		conditions []string
		args       []any
	)

	// Base scope — always first, so filter params never collide with it.
	for _, p := range q.Base {
		args = append(args, p.Arg)
		conditions = append(conditions, fmt.Sprintf(p.SQL, fmt.Sprintf("$%d", len(args))))
	}

	// AIP-160 filter, transpiled to a parameterized WHERE fragment. startIdx is
	// the next free placeholder index.
	if strings.TrimSpace(q.Filter) != "" {
		wc, err := Transpile(q.Resource, q.Filter, len(args)+1)
		if err != nil {
			return "", nil, err
		}
		if wc.SQL != "" {
			conditions = append(conditions, wc.SQL)
			args = append(args, wc.Args...)
		}
	}

	// Keyset cursor. For id-only ordering, a scalar `id <op> $n`. For a custom
	// order_by, a row-value comparison `(<col>, id) <op> ($v, $id)` — Postgres
	// evaluates the tuple lexicographically, which is exactly "the next page
	// after (col, id)" when <op> and the ORDER BY direction agree. ASC → `>`,
	// DESC → `<`.
	if q.Cursor != nil {
		op := ">"
		if q.Order.Descending {
			op = "<"
		}
		if q.Order.Field == "" {
			args = append(args, q.Cursor.ID)
			conditions = append(conditions, fmt.Sprintf("id %s $%d", op, len(args)))
		} else {
			args = append(args, q.Cursor.SortValue)
			sortPh := fmt.Sprintf("$%d", len(args))
			args = append(args, q.Cursor.ID)
			idPh := fmt.Sprintf("$%d", len(args))
			conditions = append(conditions, fmt.Sprintf("(%s, id) %s (%s, %s)", q.Order.Column, op, sortPh, idPh))
		}
	}

	// ORDER BY. The id tiebreaker takes the same direction as the sort column so
	// the row-value comparison above stays a valid keyset boundary.
	orderClause := "id"
	if q.Order.Field != "" {
		dir := "ASC"
		if q.Order.Descending {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf("%s %s, id %s", q.Order.Column, dir, dir)
	}

	// Page size + over-fetch.
	pageSize := q.PageSize
	if pageSize <= 0 {
		pageSize = 100
	}
	if pageSize > 1000 {
		pageSize = 1000
	}
	args = append(args, pageSize+1)
	limitPh := fmt.Sprintf("$%d", len(args))

	var sb strings.Builder
	sb.WriteString("SELECT * FROM ")
	sb.WriteString(q.Resource.Table)
	if len(conditions) > 0 {
		sb.WriteString(" WHERE ")
		sb.WriteString(strings.Join(conditions, " AND "))
	}
	sb.WriteString(" ORDER BY ")
	sb.WriteString(orderClause)
	sb.WriteString(" LIMIT ")
	sb.WriteString(limitPh)

	return sb.String(), args, nil
}
