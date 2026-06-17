package organizations

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"log/slog"
	"strings"
	"time"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/dashkan/pivox/internal/apierr"
	"github.com/dashkan/pivox/internal/convert"
	db "github.com/dashkan/pivox/internal/db/generated"
	"github.com/dashkan/pivox/internal/lro"
	apiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/api/v1"
	"github.com/dashkan/pivox/internal/server"
	"github.com/dashkan/pivox/internal/workers"
)

const (
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

// graceForTest mirrors pollIntervalForTest for the verification
// grace window. Production uses domainVerificationGrace (7 days);
// tests exercising the EXPIRED path set this to a sub-second value
// so the LRO ticks past the deadline without the test sleeping
// for a week.
var graceForTest time.Duration

// SetDomainVerificationGraceForTest is the test-only override for
// the verification grace window. Pass 0 to reset.
func SetDomainVerificationGraceForTest(d time.Duration) { graceForTest = d }

func effectiveDomainVerificationGrace() time.Duration {
	if graceForTest > 0 {
		return graceForTest
	}
	return domainVerificationGrace
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

	caller := server.MustPivoxUserID(ctx)
	token, err := generateVerificationToken()
	if err != nil {
		slog.ErrorContext(ctx, "create domain: token generation failed", "error", err)
		return nil, apierr.Internal("generate verification token")
	}

	domain, err := s.queries.CreateDomain(ctx, db.CreateDomainParams{
		OrgID:             resolvedOrg.ID,
		Domain:            domainStr,
		VerificationToken: token,
		CreatedBy:         convert.PgUUID(caller),
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
	deadline := time.Now().Add(effectiveDomainVerificationGrace())

	// River-backed: pivox-cloud enqueues the job + creates the
	// operations row in one tx; pivox-worker's VerifyDomainWorker
	// runs the long-poll loop. OrgID populated so
	// CancelRunningOpsForOrg picks it up if the org enters
	// DELETE_REQUESTED.
	opID := uuid.New()
	return s.lroManager.NewLro(ctx, domainResource, lro.NewLroOpts{
		OperationID: opID,
		OrgID:       pgtype.UUID{Bytes: resolvedOrg.ID, Valid: true},
		CreatedBy:   convert.PgUUID(caller),
		JobArgs: workers.VerifyDomainArgs{
			OperationID:  opID,
			DomainID:     domain.ID,
			OrgID:        resolvedOrg.ID,
			OrgSlug:      resolvedOrg.Slug,
			Resource:     domainResource,
			Deadline:     deadline,
			PollInterval: int64(effectiveDomainPollInterval()),
		},
		Metadata: initialMeta,
	})
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
	return convert.DomainToProto(row, resolvedOrg.Slug, nil), nil
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
		out[i] = convert.DomainToProto(r, resolvedOrg.Slug, nil)
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
// Tx scope: when removing a VERIFIED row, the precondition + cancel
// + delete run inside a single tx with the SSO config row locked
// FOR UPDATE. Without that lock, two concurrent DeleteDomain calls
// against sibling verified domains could both observe count >= 2
// (passing the precondition), then both delete, leaving the org
// with zero verified domains under enabled SSO. Locking the SSO
// config row serializes the precondition so the second tx sees the
// post-first-commit state and refuses. PENDING/FAILED removals
// don't touch SSO posture and skip the lock entirely.
//
// Permission: domains.delete on the parent org (interceptor-gated).
func (s *OrganizationsServer) DeleteDomain(ctx context.Context, req *apiv1.DeleteDomainRequest) (*apiv1.Domain, error) {
	resolvedOrg := server.MustResolvedOrgFromContext(ctx)
	domainStr, err := parseDomainSegment(req.GetName(), resolvedOrg.Slug)
	if err != nil {
		return nil, err
	}

	type result struct {
		row          db.Domain
		cancelledIDs []uuid.UUID
	}
	res, err := db.RunInTx(ctx, s.pool, func(qtx db.Querier) (result, error) {
		// FOR UPDATE: the verify-domain worker mutates domains.state
		// without taking an application-level lock, so reading without
		// the row lock would let MarkDomainVerified flip PENDING to
		// VERIFIED between our SELECT and our DELETE — bypassing the
		// VERIFIED-only branch of the precondition below.
		row, err := qtx.GetDomainByNameForUpdate(ctx, db.GetDomainByNameForUpdateParams{
			Domain: domainStr, OrgID: resolvedOrg.ID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return result{}, apierr.NotFound("Domain", req.GetName())
			}
			slog.ErrorContext(ctx, "delete domain: lookup failed", "name", req.GetName(), "error", err)
			return result{}, apierr.Internal("lookup domain")
		}
		if req.GetEtag() != "" && req.GetEtag() != row.Etag {
			return result{}, apierr.FailedPrecondition("etag mismatch; refresh the domain and retry")
		}

		// Last-VERIFIED-domain-on-enabled-SSO precondition. Only fires
		// for VERIFIED removals — PENDING/FAILED never affected SSO.
		// Lock the SSO config row FOR UPDATE so concurrent verified-
		// domain deletes serialize on the same lock and the count
		// reflects the truth at our delete-time, not a stale snapshot.
		if row.State == db.DomainStateVERIFIED {
			if err := guardLastVerifiedDomainTx(ctx, qtx, resolvedOrg.ID); err != nil {
				return result{}, err
			}
		}

		cancelledIDs, err := qtx.CancelDomainOpsForDomain(ctx,
			"organizations/"+resolvedOrg.Slug+"/domains/"+domainStr)
		if err != nil {
			slog.ErrorContext(ctx, "delete domain: cancel in-flight LROs failed", "domain", domainStr, "error", err)
			return result{}, apierr.Internal("cancel in-flight verification operations")
		}

		if err := qtx.DeleteDomain(ctx, db.DeleteDomainParams{ID: row.ID, OrgID: resolvedOrg.ID}); err != nil {
			slog.ErrorContext(ctx, "delete domain: delete failed", "id", row.ID, "error", err)
			return result{}, apierr.Internal("delete domain")
		}
		return result{row: row, cancelledIDs: cancelledIDs}, nil
	})
	if err != nil {
		return nil, err
	}

	return convert.DomainToProto(res.row, resolvedOrg.Slug, nil), nil
}

// guardLastVerifiedDomainTx is the tx-bound variant of the
// last-verified-domain precondition. The qtx-bound
// GetSsoConfigByOrgIDForUpdate locks the SSO config row so concurrent
// transactions queue on the same row; the count then reflects a
// consistent snapshot under that lock.
//
// Returns nil when the precondition is satisfied (no SSO config,
// disabled SSO, or count > 1). Returns FAILED_PRECONDITION when the
// delete would leave enabled SSO without any verified domain.
func guardLastVerifiedDomainTx(ctx context.Context, qtx db.Querier, orgID uuid.UUID) error {
	sso, err := qtx.GetSsoConfigByOrgIDForUpdate(ctx, orgID)
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
	count, err := qtx.CountVerifiedDomainsByOrg(ctx, orgID)
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
