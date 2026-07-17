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
//   - "" (no client order_by) → the resource's declared default order,
//     parsed from rf.DefaultOrder (e.g. "id desc" → newest-first id-only
//     keyset; an unset rf.DefaultOrder keeps the historical id-ASC default,
//     i.e. the zero OrderByPlan). See planDefaultOrder.
//   - a non-empty client order_by: exactly one field, optionally followed by
//     "asc"/"desc". The field MUST be in rf.Sortable; the resulting Column
//     comes only from that whitelist entry, never from the request. "id" is
//     NOT a client-selectable field — it is the implicit tiebreaker.
//   - more than one comma-separated field → error (see OrderByPlan doc).
//
// Column/direction are structural, not values: the column is a fixed
// whitelisted identifier and the direction selects ASC/DESC — neither is ever a
// bound parameter, but neither can be attacker-chosen text either.
func PlanOrderBy(rf *ResourceFilter, orderBy string) (OrderByPlan, error) {
	orderBy = strings.TrimSpace(orderBy)
	if orderBy == "" {
		return planDefaultOrder(rf)
	}

	field, descending, err := parseOrderTerm(orderBy)
	if err != nil {
		return OrderByPlan{}, err
	}
	if field == "" {
		return planDefaultOrder(rf)
	}

	sf, ok := rf.Sortable[field]
	if !ok {
		return OrderByPlan{}, fmt.Errorf("invalid order_by field %q", field)
	}
	return OrderByPlan{Field: field, Column: sf.Column, Type: sf.Type, Descending: descending}, nil
}

// planDefaultOrder derives the keyset plan used when the client sends no
// order_by, from the resource's declared default order (rf.DefaultOrder). This
// is one of the engine's three per-resource default knobs (alongside
// DefaultPageSize and DefaultConditions); it is what lets a resource default to
// newest-first without every client having to pass order_by.
//
// The first token is the default sort field; "id" (or an empty/unset
// DefaultOrder) means the simple id-only keyset — Field stays "" so
// EncodeCursor/DecodeCursor emit the compact 16-byte id token — but the
// declared direction still flows through to the ORDER BY clause and the keyset
// comparison operator (DESC → "<", ORDER BY id DESC). Any other field must be
// registered Sortable, in which case the default uses the compound (col, id)
// cursor exactly as a client-supplied order_by would. rf.DefaultOrder is a
// server-controlled declaration, never request input, so a bad value is a
// startup-time programmer error surfaced as an error here rather than silently
// ignored.
func planDefaultOrder(rf *ResourceFilter) (OrderByPlan, error) {
	if rf == nil || strings.TrimSpace(rf.DefaultOrder) == "" {
		return OrderByPlan{}, nil
	}
	field, descending, err := parseOrderTerm(rf.DefaultOrder)
	if err != nil {
		return OrderByPlan{}, fmt.Errorf("resource %q has invalid DefaultOrder %q: %w", rf.Table, rf.DefaultOrder, err)
	}
	if field == "" || field == "id" {
		// id-only keyset, declared direction preserved.
		return OrderByPlan{Descending: descending}, nil
	}
	sf, ok := rf.Sortable[field]
	if !ok {
		return OrderByPlan{}, fmt.Errorf("resource %q DefaultOrder field %q is not registered Sortable", rf.Table, field)
	}
	return OrderByPlan{Field: field, Column: sf.Column, Type: sf.Type, Descending: descending}, nil
}

// parseOrderTerm splits a single AIP-132 order_by term into its field and
// direction. It accepts "", "<field>", or "<field> asc|desc"; a comma (multiple
// fields), more than two tokens, or an unrecognized direction is an error. It
// resolves neither the column nor the whitelist — callers do, because the
// client path and the default path differ on whether "id" is acceptable.
func parseOrderTerm(orderBy string) (field string, descending bool, err error) {
	orderBy = strings.TrimSpace(orderBy)
	if strings.Contains(orderBy, ",") {
		return "", false, fmt.Errorf("keyset order_by supports a single field, got %q", orderBy)
	}
	fields := strings.Fields(orderBy)
	if len(fields) == 0 {
		return "", false, nil
	}
	if len(fields) > 2 {
		return "", false, fmt.Errorf("invalid order_by term %q", orderBy)
	}
	field = fields[0]
	if len(fields) == 2 {
		switch strings.ToLower(fields[1]) {
		case "asc":
			descending = false
		case "desc":
			descending = true
		default:
			return "", false, fmt.Errorf("invalid order direction %q for field %q; must be \"asc\" or \"desc\"", fields[1], field)
		}
	}
	return field, descending, nil
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
	// ShowDeleted, when true, includes soft-deleted rows. Only meaningful for a
	// resource whose ResourceFilter has SoftDelete set; otherwise ignored. This
	// is the AIP `show_deleted` request flag, plumbed by the handler.
	ShowDeleted bool
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

	// Soft-delete filter — resource-level, mirroring filter.Query's buildQuery:
	// exclude soft-deleted rows unless the caller passes ShowDeleted. It is a
	// no-arg literal predicate, so it never consumes a $N placeholder and leaves
	// the base/filter/cursor numbering below untouched. Only fires for a resource
	// whose table actually has a delete_time column (SoftDelete set).
	if q.Resource.SoftDelete && !q.ShowDeleted {
		conditions = append(conditions, "delete_time IS NULL")
	}

	// Base scope — always first, so filter params never collide with it.
	for _, p := range q.Base {
		args = append(args, p.Arg)
		conditions = append(conditions, fmt.Sprintf(p.SQL, fmt.Sprintf("$%d", len(args))))
	}

	// Resource-level default conditions — server-declared predicates ALWAYS
	// applied, even when the client sends no filter (e.g. a resource that hides
	// archived rows by default). They compose with the base scope + soft-delete +
	// client filter as ANDed conditions; like the base scope, each carries
	// exactly one %s / one bound Arg, so numbering stays aligned. Inert (nil) for
	// a resource that declares none.
	for _, p := range q.Resource.DefaultConditions {
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
	// the row-value comparison above stays a valid keyset boundary. For the
	// id-only default the direction still applies (a DESC DefaultOrder yields
	// "ORDER BY id DESC", paired with the `id < $cursor` keyset op above); ASC
	// stays the bare "id" it has always been.
	orderClause := "id"
	switch {
	case q.Order.Field != "":
		dir := "ASC"
		if q.Order.Descending {
			dir = "DESC"
		}
		orderClause = fmt.Sprintf("%s %s, id %s", q.Order.Column, dir, dir)
	case q.Order.Descending:
		orderClause = "id DESC"
	}

	// Page size + over-fetch. The caller is expected to have clamped PageSize via
	// ClampPageSize (honoring the resource's DefaultPageSize/MaxPageSize); this is
	// a defensive backstop with the same universal bounds.
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
