package workers

import (
	"context"
	"log/slog"
)

// DNSResolver is the seam between VerifyDomainWorker and the
// underlying DNS lookup. v1 ships with StubDNSResolver wired up
// so end-to-end domain claiming works without real DNS in dev /
// pre-prod. The real net.Resolver-backed impl lands before any
// external admin uses SSO (sub-decision #10 in the IAM roadmap).
type DNSResolver interface {
	// LookupTXT returns the TXT records for the given hostname,
	// matching net.Resolver.LookupTXT. The verify worker formats
	// the verification token as `pivox-site-verification=<token>`
	// and looks for a matching record in the response.
	LookupTXT(ctx context.Context, host string) ([]string, error)
}

// StubDNSResolver is the v1 placeholder: it always returns a TXT
// record matching whatever verification token the caller is
// looking for. The worker's verify path treats any non-empty list
// as "matches". Real DNS will go through the same code path; only
// the resolver impl changes.
//
// Logging strategy: a single INFO line at construction announces
// the stub is wired (so production telemetry makes the fake DNS
// path obvious), then per-lookup logs drop to DEBUG. With ~minutes
// of tick interval and N PENDING domains, INFO-per-lookup would be
// ~30N lines/hour with zero diagnostic value.
type StubDNSResolver struct {
	logger *slog.Logger
}

// NewStubDNSResolver constructs a resolver that pretends every
// lookup succeeds. The constructor logs once at INFO so the
// "DNS is faked" signal is visible without spamming the per-lookup
// path.
func NewStubDNSResolver(logger *slog.Logger) *StubDNSResolver {
	logger.Info("stub-dns: domain verification is mocked — every TXT lookup will fake-pass")
	return &StubDNSResolver{logger: logger}
}

// LookupTXT returns a single fake-pass record. The exact string
// value is irrelevant; the worker's match check only cares that
// the slice is non-empty. DEBUG-level so the per-tick fan-out
// doesn't drown INFO.
func (s *StubDNSResolver) LookupTXT(ctx context.Context, host string) ([]string, error) {
	s.logger.Debug("stub-dns: TXT lookup fake-passed", "host", host)
	return []string{"stub-dns-verification-pass"}, nil
}
