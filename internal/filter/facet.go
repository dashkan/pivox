package filter

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.einride.tech/aip/filtering"
	expr "google.golang.org/genproto/googleapis/api/expr/v1alpha1"
)

// defaultTermsSize is the top-N applied when a facet spec leaves size unset.
const defaultTermsSize = 50

// ErrUnknownFacetField is returned by ComputeFacets when a requested facet
// field is not in the resource's Facetable allowlist. Callers map it to
// InvalidArgument (the field is request input), distinguishing it from the
// Internal errors a failed query produces.
var ErrUnknownFacetField = errors.New("filter: facet field is not facetable")

// FacetSpec is one resolved terms-facet request. The List tier supports only
// terms aggregation; Field is validated against the resource's Facetable
// allowlist, Column is filled from that allowlist by ComputeFacets (never from
// the request), Name keys the result, Size is the top-N, and SelfExcluding
// drops the facet's own filter so its sibling values stay selectable.
type FacetSpec struct {
	Field         string
	Name          string
	SelfExcluding bool
	Size          int32
	// Column is the whitelisted SQL column/expression for Field, resolved from
	// Facetable. Populated by ComputeFacets; ignored on input.
	Column string
}

// FacetBucket is one terms bucket: a distinct column value and its row count.
type FacetBucket struct {
	Key   string
	Count int64
}

// RowQuerier is the minimal pgx surface ComputeFacets needs — a `Query` that
// returns rows. *pgxpool.Pool, pgx.Tx, and db.RWPool all satisfy it, so the
// handler passes its existing pool without a new dependency.
type RowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ComputeFacets runs the exact total-count and terms-facet queries for q over
// the SAME base scope + AIP filter as BuildListQuery (never narrowed by
// pagination or cursor), honoring per-facet self-exclusion, and returns the row
// total plus the per-facet buckets keyed by FacetSpec.Name (count-desc, top-N).
//
// Query plan (admin cardinality — a handful of cheap reads, correctness over
// cleverness):
//
//   - Facets whose field is NOT actively filtered share ONE GROUPING SETS scan
//     over the base+filter WHERE. Its empty grouping set () yields the grand
//     total for free, so total_count costs nothing extra.
//   - Each self-excluding facet whose field IS actively filtered needs its own
//     WHERE (the full filter minus that field's own predicate), so it runs as a
//     separate GROUP BY scan. If none exist and there are no shared facets, a
//     bare COUNT(*) supplies the total.
//
// Field names come only from q.Resource.Facetable — never request input — so
// every column emitted into GROUP BY / GROUPING SETS is server-controlled, and
// every filter operand stays a bound $N. An empty specs is a no-op (zero cost).
func ComputeFacets(ctx context.Context, querier RowQuerier, q ListQuery, specs []FacetSpec) (int64, map[string][]FacetBucket, error) {
	if q.Resource == nil {
		return 0, nil, fmt.Errorf("filter: ComputeFacets requires ListQuery.Resource")
	}
	if len(specs) == 0 {
		return 0, nil, nil
	}

	// Resolve + validate every spec's column against the Facetable allowlist
	// BEFORE issuing any query, so an unknown field fails fast as
	// ErrUnknownFacetField (InvalidArgument) rather than a DB error.
	resolved := make([]FacetSpec, len(specs))
	for i, s := range specs {
		ff, ok := q.Resource.Facetable[s.Field]
		if !ok {
			return 0, nil, fmt.Errorf("%w: %q", ErrUnknownFacetField, s.Field)
		}
		s.Column = ff.Column
		if s.Name == "" {
			s.Name = s.Field
		}
		if s.Size <= 0 {
			s.Size = defaultTermsSize
		}
		resolved[i] = s
	}

	// Which facetable fields does the request filter actively constrain? A
	// self-excluding facet on such a field must drop its own predicate.
	active, err := activeFilterFields(q.Filter)
	if err != nil {
		// A malformed filter is the caller's; surface it (the list query will
		// have reported the same). Wrap so it is not mistaken for a facet-field
		// error.
		return 0, nil, fmt.Errorf("filter: %w", err)
	}

	var shared, selfExcluding []FacetSpec
	for _, s := range resolved {
		if _, isActive := active[s.Field]; s.SelfExcluding && isActive {
			selfExcluding = append(selfExcluding, s)
		} else {
			shared = append(shared, s)
		}
	}

	results := make(map[string][]FacetBucket, len(resolved))

	// Shared facets + grand total in one scan over the full base+filter WHERE.
	sharedConds, sharedArgs, err := buildScopeConditions(q, "")
	if err != nil {
		return 0, nil, err
	}
	total, err := runSharedFacets(ctx, querier, q.Resource, sharedConds, sharedArgs, shared, results)
	if err != nil {
		return 0, nil, err
	}

	// Self-excluding facets: each over the filter minus its own field.
	for _, s := range selfExcluding {
		conds, args, err := buildScopeConditions(q, s.Field)
		if err != nil {
			return 0, nil, err
		}
		buckets, err := runTermsFacet(ctx, querier, q.Resource, conds, args, s)
		if err != nil {
			return 0, nil, err
		}
		results[s.Name] = buckets
	}

	return total, results, nil
}

// runSharedFacets executes the combined GROUPING SETS scan (or a bare COUNT(*)
// when there are no shared facets), filling results for each shared facet and
// returning the grand total row count.
func runSharedFacets(ctx context.Context, querier RowQuerier, rf *ResourceFilter, conds []string, args []any, shared []FacetSpec, results map[string][]FacetBucket) (int64, error) {
	sqlText := buildSharedFacetQuery(rf, conds, shared)
	rows, err := querier.Query(ctx, sqlText, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	// Accumulate per-facet buckets keyed by the facet's index in `shared`.
	buckets := make([][]FacetBucket, len(shared))
	var total int64
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return 0, err
		}
		// Column layout (see buildSharedFacetQuery): vals[0] = count, then for
		// each shared facet i a (grouping, value) pair at 1+2i and 2+2i.
		count, err := toInt64(vals[0])
		if err != nil {
			return 0, err
		}
		assigned := false
		for i := range shared {
			g, err := toInt64(vals[1+2*i])
			if err != nil {
				return 0, err
			}
			if g == 0 { // this row groups facet i
				buckets[i] = append(buckets[i], FacetBucket{Key: toStringKey(vals[2+2*i]), Count: count})
				assigned = true
				break
			}
		}
		if !assigned {
			// The empty grouping set () — every grouping flag is 1 — is the grand
			// total. With zero shared facets there are no grouping columns, so the
			// single COUNT(*) row lands here too.
			total = count
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for i, s := range shared {
		results[s.Name] = topN(buckets[i], s.Size)
	}
	return total, nil
}

// runTermsFacet executes one single-field GROUP BY scan for a self-excluding
// facet and returns its count-desc, top-N buckets.
func runTermsFacet(ctx context.Context, querier RowQuerier, rf *ResourceFilter, conds []string, args []any, s FacetSpec) ([]FacetBucket, error) {
	rows, err := querier.Query(ctx, buildTermsFacetQuery(rf, conds, s), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var buckets []FacetBucket
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		count, err := toInt64(vals[1])
		if err != nil {
			return nil, err
		}
		buckets = append(buckets, FacetBucket{Key: toStringKey(vals[0]), Count: count})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return topN(buckets, s.Size), nil
}

// buildSharedFacetQuery assembles the GROUPING SETS scan for the shared facets:
// count(*) plus, per facet, a grouping() flag and the value cast to text, over
// the base+filter WHERE. The grouping sets are one per facet column plus the
// empty set () for the grand total. With no shared facets it degrades to a bare
// COUNT(*) so the total is still computed. Every column is a whitelisted
// server-controlled identifier; the only bound values are the WHERE operands.
func buildSharedFacetQuery(rf *ResourceFilter, conds []string, shared []FacetSpec) string {
	var sb strings.Builder
	sb.WriteString("SELECT count(*) AS cnt")
	for i, s := range shared {
		fmt.Fprintf(&sb, ", grouping(%s) AS g%d, (%s)::text AS v%d", s.Column, i, s.Column, i)
	}
	sb.WriteString(" FROM ")
	sb.WriteString(rf.Table)
	writeWhere(&sb, conds)
	if len(shared) > 0 {
		sb.WriteString(" GROUP BY GROUPING SETS (")
		for i, s := range shared {
			if i > 0 {
				sb.WriteString(", ")
			}
			fmt.Fprintf(&sb, "(%s)", s.Column)
		}
		sb.WriteString(", ())")
	}
	return sb.String()
}

// buildTermsFacetQuery assembles a single-field GROUP BY scan for one facet.
func buildTermsFacetQuery(rf *ResourceFilter, conds []string, s FacetSpec) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "SELECT (%s)::text AS v, count(*) AS cnt FROM %s", s.Column, rf.Table)
	writeWhere(&sb, conds)
	fmt.Fprintf(&sb, " GROUP BY %s", s.Column)
	return sb.String()
}

func writeWhere(sb *strings.Builder, conds []string) {
	if len(conds) == 0 {
		return
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(strings.Join(conds, " AND "))
}

// activeFilterFields returns the set of field ids the AIP-160 filter directly
// compares against (the LHS of =, !=, <, <=, >, >=, or :). These are the fields
// a self-excluding facet may need to drop. Bare literals (default-field search)
// and value-position idents are not field references and are excluded.
func activeFilterFields(filterStr string) (map[string]struct{}, error) {
	if strings.TrimSpace(filterStr) == "" {
		return nil, nil
	}
	var parser filtering.Parser
	parser.Init(filterStr)
	parsed, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}
	fields := make(map[string]struct{})
	collectFilterFields(parsed.GetExpr(), fields)
	if len(fields) == 0 {
		return nil, nil
	}
	return fields, nil
}

func collectFilterFields(e *expr.Expr, out map[string]struct{}) {
	call, ok := e.GetExprKind().(*expr.Expr_CallExpr)
	if !ok {
		return
	}
	c := call.CallExpr
	switch c.GetFunction() {
	case filtering.FunctionAnd, filtering.FunctionFuzzyAnd, filtering.FunctionOr, filtering.FunctionNot:
		for _, a := range c.GetArgs() {
			collectFilterFields(a, out)
		}
	case filtering.FunctionEquals, filtering.FunctionNotEquals,
		filtering.FunctionLessThan, filtering.FunctionLessEquals,
		filtering.FunctionGreaterThan, filtering.FunctionGreaterEquals,
		filtering.FunctionHas:
		if args := c.GetArgs(); len(args) == 2 {
			if id, ok := args[0].GetExprKind().(*expr.Expr_IdentExpr); ok {
				out[id.IdentExpr.GetName()] = struct{}{}
			}
		}
	}
}

// topN sorts buckets count-desc (key asc as a stable tiebreak) and truncates to
// size. It sorts in place; callers pass a per-facet slice they own.
func topN(buckets []FacetBucket, size int32) []FacetBucket {
	sort.SliceStable(buckets, func(i, j int) bool {
		if buckets[i].Count != buckets[j].Count {
			return buckets[i].Count > buckets[j].Count
		}
		return buckets[i].Key < buckets[j].Key
	})
	if size > 0 && len(buckets) > int(size) {
		buckets = buckets[:size]
	}
	return buckets
}

// toInt64 coerces a pgx scalar (count / grouping flag) to int64. Postgres
// returns int8 for count(*) and int4 for grouping(), which pgx yields as int64
// and int32 respectively.
func toInt64(v any) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int32:
		return int64(n), nil
	case int:
		return int64(n), nil
	default:
		return 0, fmt.Errorf("filter: facet expected integer column, got %T", v)
	}
}

// toStringKey renders a bucket's ::text value. A NULL group value (nil) becomes
// the empty string — a terms facet key is a string, and the column value is
// already cast to text in SQL.
func toStringKey(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
