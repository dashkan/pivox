package apierr

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const domain = "pivox.ai"

func NotFound(resourceType, resourceName string) error {
	st := status.New(codes.NotFound, fmt.Sprintf("%s %q not found", resourceType, resourceName))
	st, _ = st.WithDetails(
		&errdetails.ResourceInfo{
			ResourceType: resourceType,
			ResourceName: resourceName,
			Description:  fmt.Sprintf("The requested %s does not exist or has been deleted.", resourceType),
		},
		&errdetails.ErrorInfo{
			Reason: "RESOURCE_NOT_FOUND",
			Domain: domain,
			Metadata: map[string]string{
				"resource_type": resourceType,
				"resource_name": resourceName,
			},
		},
	)
	return st.Err()
}

func AlreadyExists(resourceType, resourceName string) error {
	st := status.New(codes.AlreadyExists, fmt.Sprintf("%s %q already exists", resourceType, resourceName))
	st, _ = st.WithDetails(
		&errdetails.ResourceInfo{
			ResourceType: resourceType,
			ResourceName: resourceName,
		},
	)
	return st.Err()
}

func InvalidArgument(violations ...*errdetails.BadRequest_FieldViolation) error {
	st := status.New(codes.InvalidArgument, "one or more fields have invalid values")
	st, _ = st.WithDetails(
		&errdetails.BadRequest{FieldViolations: violations},
	)
	return st.Err()
}

func FieldViolation(field, description string) *errdetails.BadRequest_FieldViolation {
	return &errdetails.BadRequest_FieldViolation{
		Field:       field,
		Description: description,
	}
}

func EtagMismatch(resourceName, expected, actual string) error {
	st := status.New(codes.FailedPrecondition, "etag mismatch")
	st, _ = st.WithDetails(
		&errdetails.PreconditionFailure{
			Violations: []*errdetails.PreconditionFailure_Violation{{
				Type:        "ETAG",
				Subject:     resourceName,
				Description: fmt.Sprintf("expected etag %q but resource has %q", expected, actual),
			}},
		},
	)
	return st.Err()
}

func FailedPrecondition(msg string) error {
	return status.Error(codes.FailedPrecondition, msg)
}

func Internal(msg string) error {
	st := status.New(codes.Internal, msg)
	st, _ = st.WithDetails(
		&errdetails.ErrorInfo{
			Reason: "INTERNAL_ERROR",
			Domain: domain,
		},
	)
	return st.Err()
}

// Unauthenticated returns codes.Unauthenticated with a canonical
// message. Use for "no caller identity established" — missing token,
// invalid token, no auth context. Not for "caller is identified but
// lacks permission" — that's PermissionDenied.
func Unauthenticated(msg string) error {
	return status.Error(codes.Unauthenticated, msg)
}

// PermissionDenied returns codes.PermissionDenied with a canonical
// message. Use for "caller is authenticated but not permitted" —
// no membership, role too low, IAM denial.
func PermissionDenied(msg string) error {
	return status.Error(codes.PermissionDenied, msg)
}

// Unimplemented returns codes.Unimplemented for RPCs declared in the
// proto but not yet served. Distinct from FailedPrecondition (the
// caller can fix it) and Internal (server bug).
func Unimplemented(msg string) error {
	return status.Error(codes.Unimplemented, msg)
}

// Unavailable returns codes.Unavailable for transient conditions that
// the caller may safely retry — most often "server is shutting down,
// not accepting new work." gRPC clients treat Unavailable as
// retryable by default, so reserve it for cases where retry is
// genuinely the right caller behavior.
func Unavailable(msg string) error {
	return status.Error(codes.Unavailable, msg)
}

// BadRequest returns codes.InvalidArgument with a free-form message,
// for cases where the failure isn't tied to a specific field (and
// thus a typed FieldViolation isn't a natural fit). For field-level
// validation errors prefer `InvalidArgument(FieldViolation(...))` —
// clients can switch on the typed details.
func BadRequest(msg string) error {
	return status.Error(codes.InvalidArgument, msg)
}

func QuotaExceeded(subject, description string, retryDelay time.Duration) error {
	st := status.New(codes.ResourceExhausted, "quota exceeded")
	st, _ = st.WithDetails(
		&errdetails.QuotaFailure{
			Violations: []*errdetails.QuotaFailure_Violation{{
				Subject:     subject,
				Description: description,
			}},
		},
		&errdetails.RetryInfo{
			RetryDelay: durationpb.New(retryDelay),
		},
	)
	return st.Err()
}

// HandleResourceError translates common database errors into gRPC
// status errors. Recognizes:
//   - `pgx.ErrNoRows` → NotFound
//   - Postgres unique-violation (SQLSTATE 23505) → AlreadyExists
//   - Postgres FK-violation (SQLSTATE 23503) → NotFound
//   - everything else → Internal
//
// FK-violation → NotFound: post-issue-#7 the polymorphic principal_id
// columns split into typed user_id/group_id with real FKs. Handlers
// no longer need an application-level "does this principal exist?"
// pre-check before INSERT — the FK enforces it. If the principal
// gets hard-deleted between a caller's resolve and our INSERT,
// Postgres returns 23503; mapping that to NotFound surfaces the
// race cleanly to the client instead of an opaque Internal.
//
// Caller passes `resourceType`/`resourceName` we attribute the
// NotFound to. The mapping is correct in practice because every
// well-formed handler pre-validates its non-target FKs (caller via
// AuthInterceptor, role/org via the membership/permission
// interceptor, etc.) — so a 23503 reaching this function is on the
// FK the caller named.
//
// CREATE handlers where there's NO meaningful FK→NotFound mapping
// (every FK is pre-validated and any 23503 is a transient race
// that should surface as Internal) should bypass this function for
// the FK case and use `IsUniqueViolation` directly.
//
// The SQLSTATE checks use `errors.As` against `pgconn.PgError`
// + structured code constants, NOT substring matches on the error
// message. String-matching driver messages is fragile (driver
// upgrades, locale differences); structured codes are stable across
// pgx versions.
func HandleResourceError(err error, resourceType, resourceName string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return NotFound(resourceType, resourceName)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case PgUniqueViolation:
			return AlreadyExists(resourceType, resourceName)
		case PgForeignKeyViolation:
			return NotFound(resourceType, resourceName)
		}
	}
	// Unknown DB error → sanitize the response to "database error"
	// (don't leak schema state to the gRPC client) but preserve the
	// original cause on the error chain so the logging interceptor
	// can recover the *pgconn.PgError via errors.As and emit code,
	// table, column, constraint as structured slog attrs. Without
	// this wrap the interceptor sees only the sanitized "database
	// error" string and a debug session has nothing to start from
	// (e.g., the pgvector NULL panic that ate ~30 minutes today
	// surfaced as `error: "database error"` with zero diagnosable
	// detail).
	return wrapWithCause(Internal("database error"), err)
}

// statusErrorWithCause carries a gRPC status (sanitized response)
// alongside the original error chain (rich detail for logs). It
// satisfies grpc-go's GRPCStatus() interface so the framework
// extracts the wrapped status when serializing the response, AND
// implements Unwrap() so errors.Is/As walk through to the cause.
//
// Today the only consumer of the cause chain is
// internal/server/logging_interceptor.go via PgErrorLogAttrs.
// Future consumers (a /debug endpoint surfacing internal errors
// to operators, structured error reporting to Sentry, etc.) get
// the cause chain for free.
type statusErrorWithCause struct {
	st    *status.Status
	cause error
}

func (e *statusErrorWithCause) Error() string              { return e.st.Err().Error() }
func (e *statusErrorWithCause) GRPCStatus() *status.Status { return e.st }
func (e *statusErrorWithCause) Unwrap() error              { return e.cause }

// wrapWithCause attaches `cause` to the chain underneath an
// existing gRPC status error. Panics if `statusErr` is not a
// status error (programmer bug — the caller passed an unwrapped
// error). Returns `statusErr` unchanged if `cause` is nil so
// callers don't have to nil-check at every site.
func wrapWithCause(statusErr error, cause error) error {
	if cause == nil {
		return statusErr
	}
	st, ok := status.FromError(statusErr)
	if !ok {
		// Construction-time bug: the apierr builders all return
		// status errors, so this can only fire if a future
		// builder forgets. Loud failure beats silent loss of
		// status code.
		panic(fmt.Sprintf("apierr.wrapWithCause: not a status error: %v", statusErr))
	}
	return &statusErrorWithCause{st: st, cause: cause}
}

// PgErrorLogAttrs returns slog-style key/value pairs decoded from
// a *pgconn.PgError attached to the cause chain (typically by
// HandleResourceError's Internal-fallthrough wrap). Returns nil
// for non-DB causes or a nil error.
//
// Six fields surface: db_code, db_message, db_schema, db_table,
// db_column, db_constraint. Detail and Hint are deliberately
// excluded — they verbatim-leak input values
// (e.g., "Key (email)=(user@example.com) already exists.") and
// the Internal-fallthrough errors this targets (relation does
// not exist, column does not exist, pgvector internal panics)
// don't need them to diagnose. If a future debug case requires
// Detail/Hint, surface as a separate, deliberately-scoped change
// rather than expanding the default exposure.
//
// db_message is sanitized for log injection (newlines, CRs) via
// SanitizeLogString. The remaining fields are SQLSTATE codes and
// schema identifiers — never input-derived, so safe verbatim.
func PgErrorLogAttrs(err error) []any {
	if err == nil {
		return nil
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	return []any{
		"db_code", pgErr.Code,
		"db_message", SanitizeLogString(pgErr.Message),
		"db_schema", pgErr.SchemaName,
		"db_table", pgErr.TableName,
		"db_column", pgErr.ColumnName,
		"db_constraint", pgErr.ConstraintName,
	}
}

// SanitizeLogString defangs newlines and carriage returns from
// caller-controlled error strings before they're logged. The
// default JSON slog handler escapes these on its own, but
// downstream log pipelines (text-handler dev runs, regex-based
// parsers in Loki / Datadog, raw stdout tailing) can be tricked
// into treating an embedded newline as a record boundary — a
// classic log-injection vector. Cheap defense-in-depth.
//
// Lives in apierr because both apierr.PgErrorLogAttrs (decoding
// a cause's pg-message field) and the gRPC logging interceptor
// (sanitizing the gRPC status message) call it. Putting it here
// keeps one implementation; previously a duplicate sat in
// internal/server/logging_interceptor.go.
func SanitizeLogString(s string) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer("\n", "\\n", "\r", "\\r")
	return r.Replace(s)
}

func Aborted(resourceType, resourceName, reason string) error {
	st := status.New(codes.Aborted, "conflict")
	st, _ = st.WithDetails(
		&errdetails.ErrorInfo{
			Reason: reason,
			Domain: domain,
		},
		&errdetails.ResourceInfo{
			ResourceType: resourceType,
			ResourceName: resourceName,
		},
	)
	return st.Err()
}
