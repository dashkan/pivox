package apierr

import (
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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

// HandleResourceError translates common database errors into gRPC status errors.
func HandleResourceError(err error, resourceType, resourceName string) error {
	if err == pgx.ErrNoRows {
		return NotFound(resourceType, resourceName)
	}
	if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
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
