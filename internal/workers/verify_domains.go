package workers

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// VerifyDomainsArgs is the (empty) arg struct for the
// domain-verification periodic job. Kind is the on-disk dispatch
// key — wire contract.
type VerifyDomainsArgs struct{}

// Kind implements river.JobArgs.
func (VerifyDomainsArgs) Kind() string { return "verify_domains" }

// VerifyDomainsWorker drives PENDING domain rows to VERIFIED via
// the DNSResolver seam. v1 wires StubDNSResolver which always
// "succeeds"; production swaps in a net.Resolver-backed impl
// without touching this worker. Replaces the pre-River
// VerifyDomainWorker (deleted in cutover) — same SQL, same
// resolver seam, periodic invocation driven by River.
//
// Lookup failures (DNS error or empty TXT records) are NOT marked
// FAILED here. The row stays PENDING; the next periodic tick
// retries. The dedicated PENDING→FAILED transition fires only
// after the full backoff schedule elapses (which the v1 stub
// resolver never triggers).
type VerifyDomainsWorker struct {
	river.WorkerDefaults[VerifyDomainsArgs]

	Queries  db.Querier
	Resolver DNSResolver
	Logger   *slog.Logger
}

// Work implements river.Worker[VerifyDomainsArgs]. List-time errors
// propagate; per-domain errors are logged and the row left PENDING.
func (w *VerifyDomainsWorker) Work(ctx context.Context, _ *river.Job[VerifyDomainsArgs]) error {
	domains, err := w.Queries.ListPendingDomains(ctx)
	if err != nil {
		return err
	}
	if len(domains) == 0 {
		return nil
	}
	w.Logger.InfoContext(ctx, "verify_domains: ticking pending domains", "count", len(domains))
	for _, d := range domains {
		records, lookupErr := w.Resolver.LookupTXT(ctx, "_pivox-verify."+d.Domain)
		if lookupErr != nil || len(records) == 0 {
			// Transient DNS errors are common and don't mean the
			// record is wrong; row stays PENDING for next tick.
			w.Logger.WarnContext(ctx, "verify_domains: lookup not satisfied; will retry next tick",
				"domain", d.Domain, "error", lookupErr)
			continue
		}
		updated, err := w.Queries.MarkDomainVerified(ctx, d.ID)
		if err != nil {
			w.Logger.ErrorContext(ctx, "verify_domains: MarkDomainVerified failed",
				"domain", d.Domain, "error", err)
			continue
		}
		w.Logger.InfoContext(ctx, "verify_domains: verified", "domain", updated.Domain)
	}
	return nil
}
