package apierr

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	err := AlreadyExists("Space", "spaces/abc")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.AlreadyExists, st.Code())
	assert.Contains(t, st.Message(), "Space")
	assert.Contains(t, st.Message(), "spaces/abc")

	details := st.Details()
	require.NotEmpty(t, details)

	var foundResourceInfo bool
	for _, d := range details {
		if ri, ok := d.(*errdetails.ResourceInfo); ok {
			foundResourceInfo = true
			assert.Equal(t, "Space", ri.ResourceType)
			assert.Equal(t, "spaces/abc", ri.ResourceName)
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
	err := Internal(nil, "unexpected failure")
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

// TestInternal_CarriesCause pins the split that motivates the
// signature: the cause is recoverable from the error chain (so the
// logging interceptor can pull pg attrs off it) while the wire-facing
// status message stays the sanitized string — the cause never leaks
// to the client.
func TestInternal_CarriesCause(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:       "42P01",
		Message:    `relation "workflows" does not exist`,
		SchemaName: "public",
		TableName:  "workflows",
	}
	err := Internal(pgErr, "list workflows")
	require.Error(t, err)

	// Wire-facing status: sanitized message only, no cause leak.
	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "list workflows", st.Message())
	assert.NotContains(t, st.Message(), "workflows\" does not exist",
		"the pg cause must not leak into the client-facing status message")

	// Log-facing chain: the cause is recoverable via errors.As.
	var recovered *pgconn.PgError
	require.True(t, errors.As(err, &recovered),
		"the pg cause must be recoverable from the error chain")
	assert.Equal(t, "42P01", recovered.Code)

	// PgErrorLogAttrs (what the logging interceptor calls) surfaces it.
	m := attrsToMap(t, PgErrorLogAttrs(err))
	assert.Equal(t, "42P01", m["db_code"])
	assert.Equal(t, "workflows", m["db_table"])
}

// TestInternal_NilCause pins that a genuinely-causeless Internal is
// still a well-formed status error (wrapWithCause returns the status
// unchanged) and carries no recoverable pg cause.
func TestInternal_NilCause(t *testing.T) {
	err := Internal(nil, "invariant violated")
	require.Error(t, err)

	st := status.Convert(err)
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "invariant violated", st.Message())

	var pgErr *pgconn.PgError
	assert.False(t, errors.As(err, &pgErr), "nil cause → nothing to recover")
	assert.Nil(t, PgErrorLogAttrs(err))
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

// ---------------------------------------------------------------------------
// HandleResourceError
// ---------------------------------------------------------------------------

func TestHandleResourceError(t *testing.T) {
	uniqueViolation := &pgconn.PgError{Code: PgUniqueViolation, Message: "duplicate key"}
	fkViolation := &pgconn.PgError{Code: PgForeignKeyViolation, Message: "foreign key"}

	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
	}{
		{
			name:     "pgx.ErrNoRows maps to NotFound",
			err:      pgx.ErrNoRows,
			wantCode: codes.NotFound,
		},
		{
			name:     "pgconn unique-violation (23505) maps to AlreadyExists",
			err:      uniqueViolation,
			wantCode: codes.AlreadyExists,
		},
		{
			name:     "wrapped pgconn unique-violation maps to AlreadyExists",
			err:      fmt.Errorf("insert failed: %w", uniqueViolation),
			wantCode: codes.AlreadyExists,
		},
		{
			name:     "pgconn FK violation (23503) maps to NotFound when no constraint gating",
			err:      fkViolation,
			wantCode: codes.NotFound,
		},
		{
			name:     "wrapped pgconn FK violation maps to NotFound when no constraint gating",
			err:      fmt.Errorf("insert failed: %w", fkViolation),
			wantCode: codes.NotFound,
		},
		{
			name:     "string-shaped 'duplicate key' error does NOT match — must be a real PgError",
			err:      fmt.Errorf("ERROR: duplicate key value violates unique constraint"),
			wantCode: codes.Internal,
		},
		{
			name:     "generic error maps to Internal",
			err:      fmt.Errorf("something went wrong"),
			wantCode: codes.Internal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HandleResourceError(tt.err, "Asset", "assets/abc")
			require.Error(t, result)
			st := status.Convert(result)
			assert.Equal(t, tt.wantCode, st.Code())
		})
	}
}

// TestHandleResourceError_PreservesCauseOnInternal asserts that when
// HandleResourceError falls through to the Internal-error response
// (no typed mapping for the SQLSTATE), the original *pgconn.PgError
// stays recoverable via errors.As. Without this the gRPC logging
// interceptor sees only "database error" and a debug session has
// nothing to start from. Verified by the pgvector NULL panic
// surfacing as `error: "database error"` with zero diagnosable
// detail until the session that motivated this commit.
func TestHandleResourceError_PreservesCauseOnInternal(t *testing.T) {
	pgErr := &pgconn.PgError{
		Code:           "42P01",
		Message:        `relation "dashboards" does not exist`,
		SchemaName:     "public",
		TableName:      "dashboards",
		ColumnName:     "",
		ConstraintName: "",
	}

	out := HandleResourceError(pgErr, "Dashboard", "dashboards/test")
	require.Error(t, out)

	// Response side: sanitized to Internal/"database error".
	st, ok := status.FromError(out)
	require.True(t, ok, "expected gRPC status error")
	assert.Equal(t, codes.Internal, st.Code())
	assert.Equal(t, "database error", st.Message())

	// Log side: the original pgErr is recoverable via errors.As
	// so the logging interceptor can decode it for structured
	// fields.
	var recovered *pgconn.PgError
	require.True(t, errors.As(out, &recovered),
		"original *pgconn.PgError should be recoverable via errors.As; "+
			"if this fails the cause chain was dropped")
	assert.Equal(t, "42P01", recovered.Code)
	assert.Equal(t, "dashboards", recovered.TableName)
}

// TestHandleResourceError_PreservesCauseThroughWrap asserts the
// cause stays recoverable when the original error is fmt.Errorf-
// wrapped before reaching HandleResourceError (a common pattern
// when handlers add context like "load asset: %w").
func TestHandleResourceError_PreservesCauseThroughWrap(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "42703", Message: "column foo does not exist", TableName: "assets", ColumnName: "foo"}
	wrapped := fmt.Errorf("load asset: %w", pgErr)

	out := HandleResourceError(wrapped, "Asset", "assets/x")
	require.Error(t, out)

	var recovered *pgconn.PgError
	require.True(t, errors.As(out, &recovered))
	assert.Equal(t, "42703", recovered.Code)
	assert.Equal(t, "foo", recovered.ColumnName)
}

// TestHandleResourceError_TypedCasesDontWrap documents that the
// well-known SQLSTATE cases (NoRows, UniqueViolation,
// FKViolation) DO NOT preserve the cause today — they map to
// descriptive client-facing errors and the cost/benefit of
// adding `db_constraint` to AlreadyExists logs is a separate
// scope decision. If a future change wraps these too, the
// assertion flips and that's deliberate; the test exists so the
// scope boundary is recorded, not buried in commit history.
func TestHandleResourceError_TypedCasesDontWrap(t *testing.T) {
	uniqueViolation := &pgconn.PgError{
		Code:           PgUniqueViolation,
		Message:        "duplicate key value violates unique constraint",
		ConstraintName: "users_email_key",
	}
	out := HandleResourceError(uniqueViolation, "User", "users/x")

	var recovered *pgconn.PgError
	assert.False(t, errors.As(out, &recovered),
		"typed AlreadyExists case is intentionally not wrapped today; "+
			"if you flipped this, update the comment above + "+
			"PgErrorLogAttrs's docstring + the logging interceptor's "+
			"Warn branch")
}

func TestPgErrorLogAttrs(t *testing.T) {
	t.Run("decodes pg error from cause chain", func(t *testing.T) {
		pgErr := &pgconn.PgError{
			Code:           "42P01",
			Message:        `relation "x" does not exist`,
			SchemaName:     "public",
			TableName:      "x",
			ColumnName:     "",
			ConstraintName: "",
		}
		// Wrap to mirror what HandleResourceError produces — the
		// cause sits underneath a status error.
		wrapped := HandleResourceError(pgErr, "X", "x/y")

		attrs := PgErrorLogAttrs(wrapped)
		require.NotNil(t, attrs, "PgErrorLogAttrs should decode the wrapped pgErr")

		m := attrsToMap(t, attrs)
		assert.Equal(t, "42P01", m["db_code"])
		assert.Equal(t, `relation "x" does not exist`, m["db_message"])
		assert.Equal(t, "public", m["db_schema"])
		assert.Equal(t, "x", m["db_table"])
		assert.Equal(t, "", m["db_column"])
		assert.Equal(t, "", m["db_constraint"])
	})

	t.Run("returns nil for non-pg error", func(t *testing.T) {
		assert.Nil(t, PgErrorLogAttrs(fmt.Errorf("plain error")))
	})

	t.Run("returns nil for nil error", func(t *testing.T) {
		assert.Nil(t, PgErrorLogAttrs(nil))
	})

	t.Run("sanitizes db_message for log injection", func(t *testing.T) {
		pgErr := &pgconn.PgError{
			Code:    "XX000",
			Message: "internal\nfake-log-line\rinjection",
		}
		wrapped := HandleResourceError(pgErr, "X", "x/y")

		attrs := PgErrorLogAttrs(wrapped)
		m := attrsToMap(t, attrs)
		assert.Equal(t, `internal\nfake-log-line\rinjection`, m["db_message"],
			"newlines and CRs in db_message must be escaped to defang "+
				"log-injection in non-JSON downstream pipelines")
	})

	t.Run("does NOT surface Detail or Hint", func(t *testing.T) {
		// Detail and Hint frequently leak input values verbatim
		// (e.g., `Key (email)=(user@example.com) already exists.`).
		// PgErrorLogAttrs intentionally excludes them so the default
		// emission can't leak PII into prod logs.
		pgErr := &pgconn.PgError{
			Code:   "23505",
			Detail: "Key (email)=(user@example.com) already exists.",
			Hint:   "Use a different email",
		}
		// Use the Internal-fallthrough wrap directly; UniqueViolation
		// would short-circuit to AlreadyExists.
		wrapped := Internal(pgErr, "database error")

		attrs := PgErrorLogAttrs(wrapped)
		m := attrsToMap(t, attrs)
		_, hasDetail := m["db_detail"]
		_, hasHint := m["db_hint"]
		assert.False(t, hasDetail, "db_detail must not be surfaced (PII risk)")
		assert.False(t, hasHint, "db_hint must not be surfaced (PII risk)")
	})
}

func TestSanitizeLogString(t *testing.T) {
	t.Run("empty string passes through", func(t *testing.T) {
		assert.Equal(t, "", SanitizeLogString(""))
	})
	t.Run("plain ASCII passes through", func(t *testing.T) {
		assert.Equal(t, "hello world", SanitizeLogString("hello world"))
	})
	t.Run("LF escaped", func(t *testing.T) {
		assert.Equal(t, `a\nb`, SanitizeLogString("a\nb"))
	})
	t.Run("CR escaped", func(t *testing.T) {
		assert.Equal(t, `a\rb`, SanitizeLogString("a\rb"))
	})
	t.Run("CRLF escaped", func(t *testing.T) {
		assert.Equal(t, `line1\r\nline2`, SanitizeLogString("line1\r\nline2"))
	})
}

// attrsToMap turns a slog-variadic []any (key, value, key, value, ...)
// into a map for assertion. Fails the test if the slice has an odd
// length or a non-string key.
func attrsToMap(t *testing.T, attrs []any) map[string]any {
	t.Helper()
	require.Equal(t, 0, len(attrs)%2, "attrs slice must have even length (key/value pairs)")
	m := make(map[string]any, len(attrs)/2)
	for i := 0; i < len(attrs); i += 2 {
		key, ok := attrs[i].(string)
		require.True(t, ok, "attr key at index %d must be string, got %T", i, attrs[i])
		m[key] = attrs[i+1]
	}
	return m
}
