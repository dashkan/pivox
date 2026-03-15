package apierr

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNotFound(t *testing.T) {
	err := NotFound("Folder", "folders/123")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Contains(t, st.Message(), "Folder")
	assert.Contains(t, st.Message(), "folders/123")

	details := st.Details()
	require.NotEmpty(t, details)

	var foundResourceInfo bool
	for _, d := range details {
		if ri, ok := d.(*errdetails.ResourceInfo); ok {
			foundResourceInfo = true
			assert.Equal(t, "Folder", ri.ResourceType)
			assert.Equal(t, "folders/123", ri.ResourceName)
		}
	}
	assert.True(t, foundResourceInfo, "expected ResourceInfo detail")
}

func TestAlreadyExists(t *testing.T) {
	err := AlreadyExists("Project", "projects/abc")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	assert.Contains(t, st.Message(), "Project")
	assert.Contains(t, st.Message(), "projects/abc")

	details := st.Details()
	require.NotEmpty(t, details)

	var foundResourceInfo bool
	for _, d := range details {
		if ri, ok := d.(*errdetails.ResourceInfo); ok {
			foundResourceInfo = true
			assert.Equal(t, "Project", ri.ResourceType)
			assert.Equal(t, "projects/abc", ri.ResourceName)
		}
	}
	assert.True(t, foundResourceInfo, "expected ResourceInfo detail")
}

func TestInvalidArgument(t *testing.T) {
	fv := FieldViolation("display_name", "must not be empty")
	assert.Equal(t, "display_name", fv.Field)
	assert.Equal(t, "must not be empty", fv.Description)

	err := InvalidArgument(fv)
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.InvalidArgument, st.Code())

	details := st.Details()
	require.NotEmpty(t, details)

	var foundBadRequest bool
	for _, d := range details {
		if br, ok := d.(*errdetails.BadRequest); ok {
			foundBadRequest = true
			require.Len(t, br.FieldViolations, 1)
			assert.Equal(t, "display_name", br.FieldViolations[0].Field)
			assert.Equal(t, "must not be empty", br.FieldViolations[0].Description)
		}
	}
	assert.True(t, foundBadRequest, "expected BadRequest detail")
}

func TestEtagMismatch(t *testing.T) {
	err := EtagMismatch("folders/123", "abc", "def")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())

	details := st.Details()
	require.NotEmpty(t, details)

	var foundPreconditionFailure bool
	for _, d := range details {
		if pf, ok := d.(*errdetails.PreconditionFailure); ok {
			foundPreconditionFailure = true
			require.NotEmpty(t, pf.Violations)
		}
	}
	assert.True(t, foundPreconditionFailure, "expected PreconditionFailure detail")
}

func TestFailedPrecondition(t *testing.T) {
	err := FailedPrecondition("resource is not ready")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.FailedPrecondition, st.Code())
	assert.Contains(t, st.Message(), "resource is not ready")
}

func TestInternal(t *testing.T) {
	err := Internal("unexpected failure")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Contains(t, st.Message(), "unexpected failure")

	details := st.Details()
	require.NotEmpty(t, details)

	var foundErrorInfo bool
	for _, d := range details {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			foundErrorInfo = true
			assert.Equal(t, "pivox.ai", ei.Domain)
		}
	}
	assert.True(t, foundErrorInfo, "expected ErrorInfo detail with domain pivox.ai")
}

func TestQuotaExceeded(t *testing.T) {
	err := QuotaExceeded("user/123", "rate limit exceeded", 30*time.Second)
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.ResourceExhausted, st.Code())

	details := st.Details()
	require.NotEmpty(t, details)

	var foundQuotaFailure, foundRetryInfo bool
	for _, d := range details {
		if qf, ok := d.(*errdetails.QuotaFailure); ok {
			foundQuotaFailure = true
			require.NotEmpty(t, qf.Violations)
			assert.Equal(t, "user/123", qf.Violations[0].Subject)
			assert.Equal(t, "rate limit exceeded", qf.Violations[0].Description)
		}
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			foundRetryInfo = true
			assert.Equal(t, int64(30), ri.RetryDelay.Seconds)
		}
	}
	assert.True(t, foundQuotaFailure, "expected QuotaFailure detail")
	assert.True(t, foundRetryInfo, "expected RetryInfo detail")
}

func TestAborted(t *testing.T) {
	err := Aborted("Folder", "folders/123", "CONCURRENT_UPDATE")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.Aborted, st.Code())

	details := st.Details()
	require.NotEmpty(t, details)

	var foundErrorInfo bool
	for _, d := range details {
		if ei, ok := d.(*errdetails.ErrorInfo); ok {
			foundErrorInfo = true
			assert.Equal(t, "CONCURRENT_UPDATE", ei.Reason)
		}
	}
	assert.True(t, foundErrorInfo, "expected ErrorInfo detail with reason")
}
