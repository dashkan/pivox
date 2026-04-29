package organizations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
)

const (
	// domainLifecyclePrefix tags every CreateDomain LRO so the
	// CancelDomainOpsForDomain query can find them.
	domainLifecyclePrefix = "domains"

	// domainVerificationTokenBytes is the entropy budget for the
	// per-domain TXT-record value the admin must publish. 32 bytes
	// of CSPRNG → 43 chars of unpadded base64url; collision-
	// resistant well past v1 scale.
	domainVerificationTokenBytes = 32

	// domainVerificationGrace is how long the CreateDomain LRO
	// waits for DNS to propagate before transitioning to EXPIRED.
	// AIP-164 calls for "reasonable but not unbounded" — 7 days
	// matches the verification windows of SaaS peers.
	domainVerificationGrace = 7 * 24 * time.Hour

	// domainPollInterval is how often the LRO work fn checks the
	// domain row for state transitions. Short enough that a
	// successful verify is observed quickly; long enough that the
	// goroutine doesn't burn CPU. Tests override via the test-only
	// constructor field if/when an integration suite drives this.
	domainPollInterval = 30 * time.Second
)

// pollIntervalForTest lets unit tests override domainPollInterval
// without exposing it as a public field. Production reads
// domainPollInterval directly. Set via SetDomainPollIntervalForTest
// from a TestMain or per-test setup.
var pollIntervalForTest time.Duration

// SetDomainPollIntervalForTest is the test-only override. Pass 0 to
// reset to production behavior.
func SetDomainPollIntervalForTest(d time.Duration) { pollIntervalForTest = d }

func effectiveDomainPollInterval() time.Duration {
	if pollIntervalForTest > 0 {
		return pollIntervalForTest
	}
	return domainPollInterval
}

// CreateDomain claims a DNS domain on behalf of the organization.
// Issues a verification token (random 32-byte value, unpadded
// base64url-encoded), inserts the domains row in PENDING state,
// and dispatches an LRO whose work fn long-polls the row until
// it transitions to VERIFIED (the verify-domain worker drives the
// transition out-of-band) or the 7-day grace window elapses.
//
// AIP-133: returns ALREADY_EXISTS for globally-claimed domains
// WITHOUT disclosing the holding org. The pgconn unique-violation
// is mapped here, not surfaced raw.
//
// Permission: domains.create on the parent org.
func (s *OrganizationsServer) CreateDomain(ctx context.Context, req *apiv1.CreateDomainRequest) (*longrunningpb.Operation, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	domainStr := strings.ToLower(strings.TrimSpace(req.GetDomain().GetDomain()))
	if domainStr == "" {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("domain.domain", "must not be empty"))
	}
	if id := req.GetDomainId(); id != "" && strings.ToLower(id) != domainStr {
		return nil, apierr.InvalidArgument(apierr.FieldViolation("domain_id",
			"must equal lowercase(domain.domain) or be empty"))
	}

	caller, err := s.caller(ctx)
	if err != nil {
		return nil, err
	}
	token, err := generateVerificationToken()
	if err != nil {
		slog.ErrorContext(ctx, "create domain: token generation failed", "error", err)
		return nil, apierr.Internal("generate verification token")
	}

	domain, err := s.queries.CreateDomain(ctx, db.CreateDomainParams{
		OrgID:             resolvedOrg.ID,
		Domain:            domainStr,
		VerificationToken: token,
		CreatedBy:         caller.String(),
	})
	if err != nil {
		// Translate the global UNIQUE(domain) violation into
		// ALREADY_EXISTS. We deliberately don't say which org
		// holds the claim — that's an information leak.
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == apierr.PgUniqueViolation {
			return nil, apierr.AlreadyExists("Domain", domainStr)
		}
		slog.ErrorContext(ctx, "create domain: insert failed", "domain", domainStr, "error", err)
		return nil, apierr.Internal("create domain")
	}

	domainResource := "organizations/" + resolvedOrg.Slug + "/domains/" + domain.Domain
	initialMeta := &apiv1.CreateDomainMetadata{
		Phase:             apiv1.CreateDomainMetadata_AWAITING_DNS,
		VerificationToken: token,
		Domain:            domainResource,
	}
	deadline := time.Now().Add(domainVerificationGrace)
	domainID := domain.ID
	orgID := resolvedOrg.ID
	orgSlug := resolvedOrg.Slug

	return s.lroManager.CreateAndRunForOrg(ctx, domainLifecyclePrefix, orgID, initialMeta,
		func(workCtx context.Context, progress lro.Progress) (proto.Message, error) {
			return s.runVerifyDomain(workCtx, progress, domainID, orgID, orgSlug, domainResource, deadline)
		})
}

// runVerifyDomain is the LRO work fn. It long-polls the domains
// row for state transitions; the verify-domain worker drives the
// PENDING → VERIFIED transition independently. The work fn
// completes the LRO when the worker has done its job, or marks the
// row FAILED if the grace window elapses.
//
// Cancellation: ctx.Done() is observed from DeleteDomain (which
// flips the operation to cancelled) and from server shutdown.
func (s *OrganizationsServer) runVerifyDomain(
	ctx context.Context,
	progress lro.Progress,
	domainID, orgID uuid.UUID,
	orgSlug, domainResource string,
	deadline time.Time,
) (proto.Message, error) {
	t := time.NewTicker(effectiveDomainPollInterval())
	defer t.Stop()

	attempts := int32(0)
	check := func() (proto.Message, bool, error) {
		attempts++
		d, err := s.queries.GetDomainByID(ctx, db.GetDomainByIDParams{ID: domainID, OrgID: orgID})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// DeleteDomain ran while we were polling. Treat the
				// row's disappearance as a normal cancellation.
				return nil, true, apierr.FailedPrecondition("domain row was deleted; verification cancelled")
			}
			return nil, true, apierr.Internal("poll domain state")
		}
		switch d.State {
		case db.DomainStateVERIFIED:
			progress.Update(ctx, &apiv1.CreateDomainMetadata{
				Phase:         apiv1.CreateDomainMetadata_VERIFIED,
				Domain:        domainResource,
				LastCheckTime: timestamppb.Now(),
				AttemptCount:  attempts,
			})
			return convert.DomainToProto(d, orgSlug), true, nil
		case db.DomainStateFAILED:
			progress.Update(ctx, &apiv1.CreateDomainMetadata{
				Phase:         apiv1.CreateDomainMetadata_FAILED,
				Domain:        domainResource,
				LastCheckTime: timestamppb.Now(),
				AttemptCount:  attempts,
			})
			return nil, true, apierr.FailedPrecondition(fmt.Sprintf("domain %q verification failed", d.Domain))
		}
		// Still PENDING. Check for grace expiry; if elapsed, mark
		// the row FAILED and complete the LRO with EXPIRED phase.
		if time.Now().After(deadline) {
			if _, err := s.queries.MarkDomainFailed(ctx, domainID); err != nil {
				slog.ErrorContext(ctx, "create domain: mark failed on expiry", "id", domainID, "error", err)
			}
			progress.Update(ctx, &apiv1.CreateDomainMetadata{
				Phase:         apiv1.CreateDomainMetadata_EXPIRED,
				Domain:        domainResource,
				LastCheckTime: timestamppb.Now(),
				AttemptCount:  attempts,
			})
			return nil, true, apierr.FailedPrecondition("domain verification window elapsed without successful DNS check")
		}
		// Update progress so polling clients can observe attempts.
		progress.Update(ctx, &apiv1.CreateDomainMetadata{
			Phase:         apiv1.CreateDomainMetadata_AWAITING_DNS,
			Domain:        domainResource,
			LastCheckTime: timestamppb.Now(),
			AttemptCount:  attempts,
		})
		return nil, false, nil
	}

	// First check fires immediately so a freshly-deployed worker
	// that already verified the row sees the LRO complete on creation.
	if result, done, err := check(); done || err != nil {
		return result, err
	}
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
			if result, done, err := check(); done || err != nil {
				return result, err
			}
		}
	}
}

// GetDomain fetches a domain by resource name. Permission:
// domains.read on the parent org (interceptor-gated).
func (s *OrganizationsServer) GetDomain(ctx context.Context, req *apiv1.GetDomainRequest) (*apiv1.Domain, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	domainStr, err := parseDomainSegment(req.GetName(), resolvedOrg.Slug)
	if err != nil {
		return nil, err
	}
	row, err := s.queries.GetDomainByName(ctx, db.GetDomainByNameParams{
		Domain: domainStr, OrgID: resolvedOrg.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("Domain", req.GetName())
		}
		slog.ErrorContext(ctx, "get domain: lookup failed", "name", req.GetName(), "error", err)
		return nil, apierr.Internal("lookup domain")
	}
	return convert.DomainToProto(row, resolvedOrg.Slug), nil
}

// ListDomains returns all domains in the parent org. Pagination
// is accepted but ignored — orgs typically claim a handful of
// domains; the 100-row cap in the query is a defensive backstop.
//
// Permission: domains.read on the parent org (interceptor-gated).
func (s *OrganizationsServer) ListDomains(ctx context.Context, req *apiv1.ListDomainsRequest) (*apiv1.ListDomainsResponse, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	rows, err := s.queries.ListDomainsByOrg(ctx, resolvedOrg.ID)
	if err != nil {
		slog.ErrorContext(ctx, "list domains: query failed", "org_id", resolvedOrg.ID, "error", err)
		return nil, apierr.Internal("list domains")
	}
	out := make([]*apiv1.Domain, len(rows))
	for i, r := range rows {
		out[i] = convert.DomainToProto(r, resolvedOrg.Slug)
	}
	return &apiv1.ListDomainsResponse{Domains: out}, nil
}

// DeleteDomain releases a domain claim. Three preconditions, in
// order:
//
//  1. If a CreateDomain LRO is still running for this domain, cancel
//     it. The work fn observes its operation transitioning to
//     cancelled (via the existing LRO Manager flow) and exits.
//  2. If removing this domain would leave an enabled SSO config
//     without any verified domain, refuse with FAILED_PRECONDITION.
//     Admins must disable SSO or add another verified domain first.
//  3. DELETE the row.
//
// Returns the deleted Domain (AIP-135) — clients see the final
// state of the row before deletion.
//
// Permission: domains.delete on the parent org (interceptor-gated).
func (s *OrganizationsServer) DeleteDomain(ctx context.Context, req *apiv1.DeleteDomainRequest) (*apiv1.Domain, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	domainStr, err := parseDomainSegment(req.GetName(), resolvedOrg.Slug)
	if err != nil {
		return nil, err
	}
	row, err := s.queries.GetDomainByName(ctx, db.GetDomainByNameParams{
		Domain: domainStr, OrgID: resolvedOrg.ID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apierr.NotFound("Domain", req.GetName())
		}
		slog.ErrorContext(ctx, "delete domain: lookup failed", "name", req.GetName(), "error", err)
		return nil, apierr.Internal("lookup domain")
	}
	if req.GetEtag() != "" && req.GetEtag() != row.Etag {
		return nil, apierr.FailedPrecondition("etag mismatch; refresh the domain and retry")
	}

	// Last-VERIFIED-domain-on-enabled-SSO check: only relevant when
	// removing a row that's currently VERIFIED. PENDING/FAILED
	// removals never affect SSO posture.
	if row.State == db.DomainStateVERIFIED {
		if err := s.guardLastVerifiedDomain(ctx, resolvedOrg.ID); err != nil {
			return nil, err
		}
	}

	// Cancel any in-flight CreateDomain LRO. Best-effort: if no
	// LRO is running, this is a no-op (UPDATE 0 rows). DB errors
	// here surface as Internal — we don't want to delete the row
	// while a verification goroutine still runs.
	if err := s.queries.CancelDomainOpsForDomain(ctx, db.CancelDomainOpsForDomainParams{
		OrgID:      pgtype.UUID{Bytes: resolvedOrg.ID, Valid: true},
		DomainName: "organizations/" + resolvedOrg.Slug + "/domains/" + domainStr,
	}); err != nil {
		slog.ErrorContext(ctx, "delete domain: cancel in-flight LROs failed", "domain", domainStr, "error", err)
		return nil, apierr.Internal("cancel in-flight verification operations")
	}

	if err := s.queries.DeleteDomain(ctx, db.DeleteDomainParams{ID: row.ID, OrgID: resolvedOrg.ID}); err != nil {
		slog.ErrorContext(ctx, "delete domain: delete failed", "id", row.ID, "error", err)
		return nil, apierr.Internal("delete domain")
	}
	return convert.DomainToProto(row, resolvedOrg.Slug), nil
}

// guardLastVerifiedDomain returns FAILED_PRECONDITION when
// deleting a verified domain would leave an enabled SSO config
// without any verified domain. If the org has no SSO config row
// at all, or the row exists but is enabled=false, the precondition
// is satisfied.
func (s *OrganizationsServer) guardLastVerifiedDomain(ctx context.Context, orgID uuid.UUID) error {
	sso, err := s.queries.GetSsoConfigByOrgID(ctx, orgID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // no SSO config — nothing to guard
		}
		slog.ErrorContext(ctx, "delete domain: lookup sso config failed", "org_id", orgID, "error", err)
		return apierr.Internal("lookup SSO config")
	}
	if !sso.Enabled {
		return nil
	}
	count, err := s.queries.CountVerifiedDomainsByOrg(ctx, orgID)
	if err != nil {
		slog.ErrorContext(ctx, "delete domain: count verified domains failed", "org_id", orgID, "error", err)
		return apierr.Internal("count verified domains")
	}
	if count <= 1 {
		return apierr.FailedPrecondition(
			"cannot delete the last verified domain on an enabled SSO config; disable SSO or add another verified domain first")
	}
	return nil
}

// parseDomainSegment extracts the trailing `{domain}` segment from
// `organizations/{org}/domains/{domain}` and verifies the org slug
// matches the interceptor-resolved scope.
func parseDomainSegment(name, expectedOrg string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "organizations" || parts[2] != "domains" || parts[1] == "" || parts[3] == "" {
		return "", apierr.InvalidArgument(apierr.FieldViolation("name",
			"expected organizations/{org}/domains/{domain}"))
	}
	if parts[1] != expectedOrg {
		return "", apierr.InvalidArgument(apierr.FieldViolation("name",
			"org slug in path does not match resolved scope"))
	}
	return strings.ToLower(parts[3]), nil
}

// generateVerificationToken returns a 43-char unpadded base64url
// CSPRNG token. The TXT record the admin publishes contains this
// value so DNS verification can match it.
func generateVerificationToken() (string, error) {
	buf := make([]byte, domainVerificationTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
