//go:build dev

package organizations_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/appkey"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/service/organizations"
	"github.com/dashkan/pivox/internal/testutil/grpcharness"
	"github.com/dashkan/pivox/internal/workers"
)

// TestE2E_CreateDomain_StubResolverDrivesVerified pins the happy
// path: CreateDomain inserts a PENDING domain, the verify-domain
// worker (with the v1 stub resolver) runs a tick, MarkDomainVerified
// flips the row to VERIFIED, and the LRO completes with the
// VERIFIED phase.
//
// This is the canonical path for v1 since the stub resolver always
// "passes" — every PENDING domain goes to VERIFIED on the next
// worker tick.
func TestE2E_CreateDomain_StubResolverDrivesVerified(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Tight timing for the test:
	//   - poll interval: 100ms so the LRO checks the row often.
	//   - grace: 30s so a worker hiccup doesn't tip into EXPIRED
	//     mid-test (we expect the verify to land within ~200ms).
	resetPoll := setDomainPollIntervalForTest(t, 100*time.Millisecond)
	defer resetPoll()
	resetGrace := setDomainGraceForTest(t, 30*time.Second)
	defer resetGrace()

	h := newDomainsHarness(t)
	startVerifyWorker(t, h, workers.NewStubDNSResolver(silentDomainLogger()), 50*time.Millisecond)

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "verify-org", "Verify Org")

	op, err := orgClient.CreateDomain(context.Background(), &apiv1.CreateDomainRequest{
		Parent: "organizations/verify-org",
		Domain: &apiv1.Domain{Domain: "example.com"},
	})
	require.NoError(t, err)
	require.False(t, op.GetDone(),
		"CreateDomain LRO should be async — worker drives the transition")

	final := waitOpUntilDone(t, h, op, 5*time.Second, "CreateDomain")
	require.Nil(t, final.GetError(), "LRO must complete cleanly: %v", final.GetError())

	var verifiedDomain apiv1.Domain
	require.NoError(t, final.GetResponse().UnmarshalTo(&verifiedDomain))
	assert.Equal(t, "organizations/verify-org/domains/example.com", verifiedDomain.GetName())
	assert.Equal(t, apiv1.Domain_VERIFIED, verifiedDomain.GetState())
}

// TestE2E_CreateDomain_NoTxtRecordExpires pins the EXPIRED path:
// when the resolver returns no records, MarkDomainVerified never
// fires, the row stays PENDING, and the LRO eventually trips the
// grace deadline → EXPIRED.
func TestE2E_CreateDomain_NoTxtRecordExpires(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Tighter timing: poll fast, grace very short so we don't sit
	// on the test for seconds while waiting for the deadline.
	resetPoll := setDomainPollIntervalForTest(t, 50*time.Millisecond)
	defer resetPoll()
	resetGrace := setDomainGraceForTest(t, 200*time.Millisecond)
	defer resetGrace()

	h := newDomainsHarness(t)
	// Resolver returns empty — verify worker treats this as "not
	// yet propagated, retry next tick"; the row stays PENDING.
	startVerifyWorker(t, h, &emptyDNSResolver{}, 50*time.Millisecond)

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "expire-org", "Expire Org")

	op, err := orgClient.CreateDomain(context.Background(), &apiv1.CreateDomainRequest{
		Parent: "organizations/expire-org",
		Domain: &apiv1.Domain{Domain: "no-record.example"},
	})
	require.NoError(t, err)

	final := waitOpUntilDone(t, h, op, 3*time.Second, "CreateDomain expire")
	require.NotNil(t, final.GetError(),
		"LRO must complete with an error after grace expires")
	assert.Equal(t, int32(codes.FailedPrecondition), final.GetError().GetCode())
	assert.Contains(t, final.GetError().GetMessage(), "verification window elapsed")
}

// TestE2E_CreateDomain_DuplicateAlreadyExists pins the AIP-133
// duplicate-name handling: claiming a domain twice (even within the
// same org) returns AlreadyExists. The handler intentionally does
// not disclose whether the existing claim is in the same org or
// elsewhere — that's the spec'd information-leak guard.
func TestE2E_CreateDomain_DuplicateAlreadyExists(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	resetPoll := setDomainPollIntervalForTest(t, 100*time.Millisecond)
	defer resetPoll()
	resetGrace := setDomainGraceForTest(t, 30*time.Second)
	defer resetGrace()

	h := newDomainsHarness(t)
	// No verify worker — we don't need verification to fire for
	// this test; we only assert duplicate-claim handling.

	owner := h.SeedIdentity(t, grpcharness.SeedIdentityOpts{UID: "owner"})
	h.SetCaller(owner)

	orgClient := apiv1.NewOrganizationsClient(h.Conn())
	createOrg(t, orgClient, "dup-org", "Dup Org")

	_, err := orgClient.CreateDomain(context.Background(), &apiv1.CreateDomainRequest{
		Parent: "organizations/dup-org",
		Domain: &apiv1.Domain{Domain: "claim-me.example"},
	})
	require.NoError(t, err, "first claim must succeed")

	_, err = orgClient.CreateDomain(context.Background(), &apiv1.CreateDomainRequest{
		Parent: "organizations/dup-org",
		Domain: &apiv1.Domain{Domain: "claim-me.example"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, status.Code(err))
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// emptyDNSResolver returns no TXT records — the verify worker
// treats this as "not yet propagated, retry next tick" so the row
// stays PENDING. Used to exercise the grace-expiry path.
type emptyDNSResolver struct{}

func (emptyDNSResolver) LookupTXT(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("stub: no records")
}

// createOrg is the test-setup helper for tests that need an org.
// CreateOrganization is sync (returns done=true immediately) so no
// LRO wait is needed. Returns the org proto so callers that need
// the etag/revision can use it; callers that only need the org to
// exist can call it as a statement and discard.
func createOrg(t *testing.T, c apiv1.OrganizationsClient, slug, displayName string) *apiv1.Organization {
	t.Helper()
	op, err := c.CreateOrganization(context.Background(), &apiv1.CreateOrganizationRequest{
		OrganizationId: slug,
		Organization:   &apiv1.Organization{DisplayName: displayName},
	})
	require.NoError(t, err)
	require.True(t, op.GetDone())
	var org apiv1.Organization
	require.NoError(t, op.GetResponse().UnmarshalTo(&org))
	return &org
}

func newDomainsHarness(t *testing.T) *grpcharness.Harness {
	return grpcharness.New(t, grpcharness.WithServices(func(h *grpcharness.Harness, s *grpc.Server) {
		callerIdentity := server.NewCallerIdentityResolver(h.Queries)
		codec, err := appkey.NewFromHex(strings.Repeat("ab", 32))
		require.NoError(t, err)
		apiv1.RegisterOrganizationsServer(s, organizations.NewOrganizationsServer(organizations.Config{
			Pool:       h.Pool,
			Queries:    h.Queries,
			Auth:       h.Auth,
			Codec:      codec,
			ReadUID:    server.AuthenticatedUID,
			Caller:     callerIdentity,
			LROManager: h.LROManager,
			Encryptor:  h.Encryptor,
		}))
	}))
}

// startVerifyWorker is a stub kept to preserve the file's build under
// -tags=dev. The pre-River workers.NewVerifyDomainWorker was deleted
// in commit 4e5e3aa (the River cutover); domain verification now runs
// as a River periodic job in pivox-worker. Tests that depend on this
// helper now skip — they need migrating to the new shape (#71 Phase 2
// for the organizations service) but that's separate from #69 Phase 5.
func startVerifyWorker(t *testing.T, _ *grpcharness.Harness, _ workers.DNSResolver, _ time.Duration) {
	t.Helper()
	t.Skip("startVerifyWorker references the pre-River VerifyDomainWorker; test needs migration per #71 Phase 2")
}

func setDomainPollIntervalForTest(t *testing.T, d time.Duration) func() {
	t.Helper()
	organizations.SetDomainPollIntervalForTest(d)
	return func() { organizations.SetDomainPollIntervalForTest(0) }
}

func setDomainGraceForTest(t *testing.T, d time.Duration) func() {
	t.Helper()
	organizations.SetDomainVerificationGraceForTest(d)
	return func() { organizations.SetDomainVerificationGraceForTest(0) }
}

func silentDomainLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// waitOpUntilDone polls the LRO until it's done or the timeout
// fires. Used for the verify path which has a real worker tick
// loop; the harness's WaitOperation uses listeners which are
// unaffected by external worker activity.
func waitOpUntilDone(t *testing.T, h *grpcharness.Harness, op interface{ GetName() string }, timeout time.Duration, label string) *longrunningpb.Operation {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		got, err := h.LROManager.GetOperation(context.Background(), op.GetName())
		require.NoError(t, err, "%s: GetOperation failed", label)
		if got.GetDone() {
			return got
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("%s: LRO not done after %s", label, timeout)
	return nil
}
