package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/dashkan/pivox/internal/apierr"
)

// captureLogger returns a JSON slog.Logger wired to the supplied
// buffer + a parsed-record helper. Used by tests asserting on
// individual log records emitted by the interceptor.
func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// decodeOnly parses a single newline-terminated JSON log record
// from the buffer. Asserts exactly one record was emitted —
// multi-record cases would mean the interceptor double-logged or
// a sibling code path leaked.
func decodeOnly(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 1, "expected exactly one log record, got %d:\n%s", len(lines), buf.String())
	var rec map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &rec))
	return rec
}

func TestLoggingUnaryInterceptor_AppendsPgErrorLogAttrs(t *testing.T) {
	// Fake an Internal-fallthrough scenario: handler hits an
	// unmapped pg SQLSTATE (relation does not exist) → apierr
	// returns the wrapped Internal status with the PgError on
	// the cause chain → interceptor must surface db_code,
	// db_table, etc. in the slog record.
	pgErr := &pgconn.PgError{
		Code:           "42P01",
		Message:        `relation "dashboards" does not exist`,
		SchemaName:     "public",
		TableName:      "dashboards",
		ColumnName:     "",
		ConstraintName: "",
	}
	handlerErr := apierr.HandleResourceError(pgErr, "Dashboard", "dashboards/x")

	var buf bytes.Buffer
	icp := LoggingUnaryInterceptor(captureLogger(&buf))
	_, err := icp(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req any) (any, error) { return nil, handlerErr },
	)
	require.Error(t, err, "interceptor must propagate the handler's error unchanged")

	rec := decodeOnly(t, &buf)
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "rpc", rec["msg"])
	assert.Equal(t, "/test.Service/Method", rec["method"])
	assert.Equal(t, "Internal", rec["code"])
	assert.Equal(t, "database error", rec["error"])
	// The new attrs from PgErrorLogAttrs:
	assert.Equal(t, "42P01", rec["db_code"], "SQLSTATE must surface so debug starts with the error class")
	assert.Equal(t, `relation "dashboards" does not exist`, rec["db_message"])
	assert.Equal(t, "public", rec["db_schema"])
	assert.Equal(t, "dashboards", rec["db_table"])
}

func TestLoggingUnaryInterceptor_NoPgFieldsForGenericInternal(t *testing.T) {
	// Handler returns a status error without any pg cause —
	// PgErrorLogAttrs returns nil and the record carries no
	// db_* keys (don't pollute logs with empty fields).
	handlerErr := apierr.Internal("something internal exploded")

	var buf bytes.Buffer
	icp := LoggingUnaryInterceptor(captureLogger(&buf))
	_, err := icp(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req any) (any, error) { return nil, handlerErr },
	)
	require.Error(t, err)

	rec := decodeOnly(t, &buf)
	assert.Equal(t, "ERROR", rec["level"])
	assert.Equal(t, "Internal", rec["code"])
	assert.Equal(t, "something internal exploded", rec["error"])

	for _, k := range []string{"db_code", "db_message", "db_schema", "db_table", "db_column", "db_constraint"} {
		_, present := rec[k]
		assert.False(t, present, "no pg cause → no db_* keys; got %q", k)
	}
}

func TestLoggingUnaryInterceptor_TypedClientErrorStaysWarn(t *testing.T) {
	// AlreadyExists (UniqueViolation typed-case) is a Warn,
	// not Error, and intentionally does not carry pg fields
	// today (apierr doesn't wrap typed cases — see
	// TestHandleResourceError_TypedCasesDontWrap). This test
	// pins both behaviors so a future change that flips either
	// has to update the assertion deliberately.
	pgErr := &pgconn.PgError{Code: apierr.PgUniqueViolation, Message: "duplicate key", ConstraintName: "users_email_key"}
	handlerErr := apierr.HandleResourceError(pgErr, "User", "users/abc")

	var buf bytes.Buffer
	icp := LoggingUnaryInterceptor(captureLogger(&buf))
	_, err := icp(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"},
		func(ctx context.Context, req any) (any, error) { return nil, handlerErr },
	)
	require.Error(t, err)

	rec := decodeOnly(t, &buf)
	assert.Equal(t, "WARN", rec["level"])
	assert.Equal(t, "AlreadyExists", rec["code"])
	_, hasDBCode := rec["db_code"]
	assert.False(t, hasDBCode, "typed cases don't preserve cause today; "+
		"if you flipped this update apierr_test.go::TestHandleResourceError_TypedCasesDontWrap "+
		"and the Warn branch in logRPC")
}
