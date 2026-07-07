package server

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/dashkan/pivox/internal/apierr"
)

// LoggingUnaryInterceptor logs the outcome of every unary RPC: method,
// status code, latency, and the caller's Pivox identity UUID when
// present (the stable internal `identities.id`, i.e. the Keycloak
// `sub`). On failure, also logs the underlying error message. Log
// level is
// chosen by status code so noisy expected client errors
// (`NotFound`, `InvalidArgument`, ...) don't drown out real bugs:
//
//   - `OK`                                                 → Info
//   - `InvalidArgument` / `NotFound` / `PermissionDenied`
//     / `Unauthenticated` / `AlreadyExists` / `FailedPrecondition`
//     / `OutOfRange` / `ResourceExhausted` / `Aborted`     → Warn
//   - `Internal` / `Unknown` / `Unavailable` / `DataLoss`  → Error
//   - `Canceled` / `DeadlineExceeded`                      → Debug
//
// Place this interceptor FIRST in the chain so it sees auth and
// validation rejections too. Handlers shouldn't need their own
// per-RPC logging boilerplate — surface failures here, not in each
// service.
func LoggingUnaryInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		logRPC(logger, info.FullMethod, start, ctx, err)
		return resp, err
	}
}

// LoggingStreamInterceptor is the streaming counterpart. Covers all
// three stream types (server-streaming, client-streaming, bidi) —
// gRPC routes all of them through `StreamServerInterceptor`. Latency
// is the lifetime of the stream end-to-end.
func LoggingStreamInterceptor(logger *slog.Logger) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		start := time.Now()
		err := handler(srv, ss)
		logRPC(logger, info.FullMethod, start, ss.Context(), err)
		return err
	}
}

func logRPC(logger *slog.Logger, method string, start time.Time, ctx context.Context, err error) {
	st, _ := status.FromError(err)
	code := st.Code()
	attrs := []any{
		"method", method,
		"code", code.String(),
		"latency_ms", time.Since(start).Milliseconds(),
	}
	if id, ok := UserID(ctx); ok {
		attrs = append(attrs, "user_id", id.String())
	}

	switch code {
	case codes.OK:
		logger.Info("rpc", attrs...)
	case codes.Canceled, codes.DeadlineExceeded:
		// Usually a client disconnect or timed-out poll — noisy and
		// rarely actionable. Demote to Debug.
		logger.Debug("rpc", attrs...)
	case codes.Internal, codes.Unknown, codes.Unavailable, codes.DataLoss:
		// Server-side bugs. Append the gRPC status message AND any
		// pg-error fields recovered from the cause chain — the
		// gRPC response is sanitized to "database error" by
		// apierr.HandleResourceError, but the underlying
		// *pgconn.PgError carries the schema/table/column/constraint
		// the next debug session needs to start from.
		errAttrs := append(attrs, "error", apierr.SanitizeLogString(st.Message()))
		if cause := apierr.PgErrorLogAttrs(err); cause != nil {
			errAttrs = append(errAttrs, cause...)
		}
		logger.Error("rpc", errAttrs...)
	default:
		// Client errors (InvalidArgument, NotFound, PermissionDenied,
		// Unauthenticated, AlreadyExists, FailedPrecondition,
		// OutOfRange, ResourceExhausted, Aborted) — warn so they
		// surface in dashboards but don't drown out real errors.
		logger.Warn("rpc", append(attrs, "error", apierr.SanitizeLogString(st.Message()))...)
	}
}
