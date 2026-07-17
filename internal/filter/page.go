package filter

// defaultPageSize / defaultMaxPageSize are the universal fallbacks applied by
// ClampPageSize when a resource declares neither DefaultPageSize nor
// MaxPageSize — preserving the 100/1000 policy every keyset handler hard-coded
// before the per-resource knobs existed.
const (
	defaultPageSize    int32 = 100
	defaultMaxPageSize int32 = 1000
)

// ClampPageSize applies the server page-size policy for a keyset list: a request
// page_size of <= 0 becomes the resource's DefaultPageSize (falling back to 100
// when unset), and any value above the resource's MaxPageSize (falling back to
// 1000 when unset) is capped. It replaces the per-handler clampPageSize copies
// that each hard-coded 100/1000, routing the policy through the resource's
// declared knobs instead.
func ClampPageSize(rf *ResourceFilter, n int32) int32 {
	defaultSize := defaultPageSize
	if rf != nil && rf.DefaultPageSize > 0 {
		defaultSize = rf.DefaultPageSize
	}
	maxSize := defaultMaxPageSize
	if rf != nil && rf.MaxPageSize > 0 {
		maxSize = rf.MaxPageSize
	}
	switch {
	case n <= 0:
		return defaultSize
	case n > maxSize:
		return maxSize
	default:
		return n
	}
}

// Paginate trims an over-fetched keyset result set (queried at LIMIT
// pageSize+1) to pageSize and derives the next-page token from the LAST
// RETURNED row.
//
// It exists to close, structurally, the off-by-one footgun that recurred
// across the List surface: a handler that over-fetched by one and then encoded
// its cursor from rows[pageSize] (the first UN-returned row) against a strict
// `>`/`<` resume predicate silently dropped one row at every page boundary. By
// owning BOTH the trim and the token, Paginate makes that mistake
// unrepresentable — the caller supplies HOW to encode a row (encode), never
// WHICH row. encode is invoked only when a further page exists
// (len(rows) > pageSize); otherwise nextToken is "".
//
// Contract:
//   - pageSize < 1 OR len(rows) <= pageSize → (rows, "", nil): no further page.
//     The pageSize < 1 guard is load-bearing — it prevents the negative-index
//     panic a naive rows[:pageSize][pageSize-1] would hit.
//   - otherwise → page = rows[:pageSize]; encode is called with the last
//     returned row (page[pageSize-1]). On an encode error the raw error is
//     returned with a nil page and empty token (callers wrap it, e.g.
//     apierr.Internal(err, "encode page token")); on success (page, tok, nil).
func Paginate[T any](rows []T, pageSize int, encode func(last T) (string, error)) (page []T, nextToken string, err error) {
	if pageSize < 1 || len(rows) <= pageSize {
		return rows, "", nil
	}
	page = rows[:pageSize]
	tok, err := encode(page[pageSize-1])
	if err != nil {
		return nil, "", err
	}
	return page, tok, nil
}
