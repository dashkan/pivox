package workers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/testutil/mocks"
)

// fakeResolver lets tests script LookupTXT without touching real
// DNS. It also lets us assert what hostnames the worker queries.
type fakeResolver struct {
	mock.Mock
}

func (f *fakeResolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	args := f.Called(ctx, host)
	return args.Get(0).([]string), args.Error(1)
}

func TestVerifyDomainWorker_TicksPendingToVerified(t *testing.T) {
	q := new(mocks.MockQuerier)
	resolver := &fakeResolver{}
	d1 := db.Domain{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Domain: "acme.com"}
	d2 := db.Domain{ID: uuid.MustParse("0192a000-bbbb-7000-8000-000000000002"), Domain: "beta.io"}
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{d1, d2}, nil)
	resolver.On("LookupTXT", mock.Anything, "_pivox-verify.acme.com").Return([]string{"ok"}, nil)
	resolver.On("LookupTXT", mock.Anything, "_pivox-verify.beta.io").Return([]string{"ok"}, nil)
	q.On("MarkDomainVerified", mock.Anything, d1.ID).Return(d1, nil)
	q.On("MarkDomainVerified", mock.Anything, d2.ID).Return(d2, nil)

	w := &VerifyDomainWorker{queries: q, resolver: resolver, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, w.processBatch(context.Background()))
	q.AssertExpectations(t)
	resolver.AssertExpectations(t)
}

func TestVerifyDomainWorker_LookupFailureKeepsDomainPending(t *testing.T) {
	// DNS lookup errors are treated as "retry next tick" — the row
	// stays PENDING. MarkDomainVerified must NOT fire.
	q := new(mocks.MockQuerier)
	resolver := &fakeResolver{}
	d := db.Domain{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Domain: "transient-fail.com"}
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{d}, nil)
	resolver.On("LookupTXT", mock.Anything, "_pivox-verify.transient-fail.com").
		Return([]string{}, errors.New("dns timeout"))

	w := &VerifyDomainWorker{queries: q, resolver: resolver, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, w.processBatch(context.Background()))
	q.AssertNotCalled(t, "MarkDomainVerified", mock.Anything, mock.Anything)
}

func TestVerifyDomainWorker_EmptyTXTRecordsKeepsPending(t *testing.T) {
	// Empty list is the "domain doesn't have the verification TXT
	// yet" signal — same outcome as a lookup error, retry next tick.
	q := new(mocks.MockQuerier)
	resolver := &fakeResolver{}
	d := db.Domain{ID: uuid.MustParse("0192a000-aaaa-7000-8000-000000000001"), Domain: "no-record.com"}
	q.On("ListPendingDomains", mock.Anything).Return([]db.Domain{d}, nil)
	resolver.On("LookupTXT", mock.Anything, "_pivox-verify.no-record.com").Return([]string{}, nil)

	w := &VerifyDomainWorker{queries: q, resolver: resolver, logger: silentLogger(), interval: time.Minute}
	require.NoError(t, w.processBatch(context.Background()))
	q.AssertNotCalled(t, "MarkDomainVerified", mock.Anything, mock.Anything)
}

// --- Stub resolver ---

func TestStubDNSResolver_AlwaysReturnsNonEmpty(t *testing.T) {
	// The stub is the v1 production resolver — it must always
	// pretend the lookup succeeded so PENDING domains tick to
	// VERIFIED. The exact string doesn't matter; the worker only
	// checks that the slice is non-empty.
	r := NewStubDNSResolver(silentLogger())
	got, err := r.LookupTXT(context.Background(), "anything.example")
	require.NoError(t, err)
	assert.NotEmpty(t, got)
}
