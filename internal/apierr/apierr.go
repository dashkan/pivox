package apierr

import (
	"errors"
	"fmt"
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
//   - everything else → Internal
//
// The unique-violation check uses `errors.As` against `pgconn.PgError`
// + the SQLSTATE constant, NOT a substring match on the error message.
// String-matching driver messages is fragile (driver upgrades, locale
// differences); structured codes are stable across pgx versions.
func HandleResourceError(err error, resourceType, resourceName string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return NotFound(resourceType, resourceName)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == PgUniqueViolation {
		return AlreadyExists(resourceType, resourceName)
	}
	return Internal("database error")
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
