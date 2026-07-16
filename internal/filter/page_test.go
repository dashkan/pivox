package filter

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPaginate exercises the trim-and-encode contract. The crux case is
// len(rows) == pageSize (an exact page fill): it MUST NOT emit a token, because
// there is no further page — emitting one here is precisely the off-by-one the
// helper exists to make unrepresentable. Where a token IS emitted, the row
// handed to encode must be the LAST RETURNED row (rows[pageSize-1]), never the
// first un-returned row.
func TestPaginate(t *testing.T) {
	t.Parallel()

	const token = "next-token"

	tests := []struct {
		name       string
		rows       []int
		pageSize   int
		wantPage   []int
		wantToken  string
		wantEncode bool // encode expected to be invoked
		wantLast   int  // the row encode should have received (when wantEncode)
	}{
		{
			name:      "empty slice yields no token",
			rows:      []int{},
			pageSize:  10,
			wantPage:  []int{},
			wantToken: "",
		},
		{
			name:      "fewer rows than page size yields no token",
			rows:      []int{1, 2, 3},
			pageSize:  10,
			wantPage:  []int{1, 2, 3},
			wantToken: "",
		},
		{
			name:      "exactly page size yields no token (the boundary bug)",
			rows:      []int{1, 2, 3},
			pageSize:  3,
			wantPage:  []int{1, 2, 3},
			wantToken: "",
		},
		{
			name:       "one over page size encodes the last returned row",
			rows:       []int{1, 2, 3, 4},
			pageSize:   3,
			wantPage:   []int{1, 2, 3},
			wantToken:  token,
			wantEncode: true,
			wantLast:   3, // rows[pageSize-1], NOT rows[pageSize] (== 4)
		},
		{
			name:       "page size one encodes the single returned row",
			rows:       []int{7, 8},
			pageSize:   1,
			wantPage:   []int{7},
			wantToken:  token,
			wantEncode: true,
			wantLast:   7,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var (
				gotLast    int
				encodeSeen bool
			)
			page, next, err := Paginate(tt.rows, tt.pageSize, func(last int) (string, error) {
				encodeSeen = true
				gotLast = last
				return token, nil
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantPage, page)
			assert.Equal(t, tt.wantToken, next)
			assert.Equal(t, tt.wantEncode, encodeSeen, "encode invocation")
			if tt.wantEncode {
				assert.Equal(t, tt.wantLast, gotLast, "encode must receive the last RETURNED row (rows[pageSize-1])")
			}
		})
	}
}

// TestPaginate_EncodeError pins that an encode failure propagates the RAW error
// (callers wrap it) and returns a nil page + empty token, so a partial page is
// never surfaced alongside a token-encoding failure.
func TestPaginate_EncodeError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")
	page, next, err := Paginate([]int{1, 2, 3, 4}, 3, func(int) (string, error) {
		return "", sentinel
	})

	require.ErrorIs(t, err, sentinel, "the raw encode error must propagate unwrapped")
	assert.Nil(t, page)
	assert.Empty(t, next)
}

// TestPaginate_NonPositivePageSize pins the golang-safety guard: a pageSize < 1
// must return the rows untrimmed with no token and, critically, MUST NOT panic
// (a naive rows[:pageSize][pageSize-1] would index out of range).
func TestPaginate_NonPositivePageSize(t *testing.T) {
	t.Parallel()

	for _, ps := range []int{0, -1, -100} {
		rows := []int{1, 2, 3}
		var encodeSeen bool
		page, next, err := Paginate(rows, ps, func(int) (string, error) {
			encodeSeen = true
			return "tok", nil
		})
		require.NoError(t, err)
		assert.Equal(t, rows, page, "pageSize=%d returns rows untrimmed", ps)
		assert.Empty(t, next)
		assert.False(t, encodeSeen, "encode must not run for pageSize=%d", ps)
	}
}
