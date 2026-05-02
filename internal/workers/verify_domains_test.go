package workers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// stubTestResolver is a deterministic in-process DNSResolver for tests.
// Returns whatever the test sets; LookupTXT errors when err is set.
type stubTestResolver struct {
	records map[string][]string
	err     error
}

func (f *stubTestResolver) LookupTXT(_ context.Context, host string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.records[host], nil
}

func TestVerifyDomainsWorker_Work_VerifiesPendingDomains(t *testing.T) {
	q := new(mocks.MockQuerier)
	d1 := db.Domain{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Domain: "example.com"}
	d2 := db.Domain{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Domain: "acme.test"}
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{d1, d2}, nil)
	q.On("MarkDomainVerified", mock.Anything, d1.ID).Return(d1, nil)
	q.On("MarkDomainVerified", mock.Anything, d2.ID).Return(d2, nil)

	resolver := &stubTestResolver{records: map[string][]string{
		"_pivox-verify.example.com": {"pivox-site-verification=token1"},
		"_pivox-verify.acme.test":   {"pivox-site-verification=token2"},
	}}
	w := &VerifyDomainsWorker{Queries: q, Resolver: resolver, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[VerifyDomainsArgs]{Args: VerifyDomainsArgs{}}))
	q.AssertExpectations(t)
}

// TestVerifyDomainsWorker_Work_LookupFailureLeavesPending preserves
// the pre-River semantics: a transient DNS error doesn't fail the
// row — the row stays PENDING and the next tick retries. Marking
// FAILED is a separate, explicit transition driven by the backoff
// schedule (not implemented here; out of scope for the migration).
func TestVerifyDomainsWorker_Work_LookupFailureLeavesPending(t *testing.T) {
	q := new(mocks.MockQuerier)
	d := db.Domain{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Domain: "example.com"}
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{d}, nil)

	resolver := &stubTestResolver{err: errors.New("dns timeout")}
	w := &VerifyDomainsWorker{Queries: q, Resolver: resolver, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[VerifyDomainsArgs]{Args: VerifyDomainsArgs{}}))
	q.AssertNotCalled(t, "MarkDomainVerified", mock.Anything, mock.Anything)
}

// TestVerifyDomainsWorker_Work_EmptyTXTLeavesPending: a successful
// lookup that returns no records is treated identically to a
// transient error — the row isn't ready yet. Same retry-on-next-tick
// semantics.
func TestVerifyDomainsWorker_Work_EmptyTXTLeavesPending(t *testing.T) {
	q := new(mocks.MockQuerier)
	d := db.Domain{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Domain: "example.com"}
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{d}, nil)

	resolver := &stubTestResolver{records: map[string][]string{}}
	w := &VerifyDomainsWorker{Queries: q, Resolver: resolver, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[VerifyDomainsArgs]{Args: VerifyDomainsArgs{}}))
	q.AssertNotCalled(t, "MarkDomainVerified", mock.Anything, mock.Anything)
}

func TestVerifyDomainsWorker_Work_NoopWhenEmpty(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{}, nil)

	w := &VerifyDomainsWorker{Queries: q, Resolver: &stubTestResolver{}, Logger: silentLogger()}
	require.NoError(t, w.Work(context.Background(), &river.Job[VerifyDomainsArgs]{Args: VerifyDomainsArgs{}}))
}

func TestVerifyDomainsWorker_Work_ListErrorReturned(t *testing.T) {
	q := new(mocks.MockQuerier)
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{}, errors.New("db down"))

	w := &VerifyDomainsWorker{Queries: q, Resolver: &stubTestResolver{}, Logger: silentLogger()}
	err := w.Work(context.Background(), &river.Job[VerifyDomainsArgs]{Args: VerifyDomainsArgs{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "db down")
}

func TestVerifyDomainsArgs_Kind(t *testing.T) {
	assert.Equal(t, "verify_domains", VerifyDomainsArgs{}.Kind())
}
