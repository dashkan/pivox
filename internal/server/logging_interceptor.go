package server

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LoggingUnaryInterceptor logs the outcome of every unary RPC: method,
// status code, latency, and authenticated UID when present. On
// failure, also logs the underlying error message. Log level is
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
	if uid, ok := AuthenticatedUID(ctx); ok && uid != "" {
		attrs = append(attrs, "uid", uid)
	}

	switch code {
	case codes.OK:
		logger.Info("rpc", attrs...)
	case codes.Canceled, codes.DeadlineExceeded:
		// Usually a client disconnect or timed-out poll — noisy and
		// rarely actionable. Demote to Debug.
		logger.Debug("rpc", attrs...)
	case codes.Internal, codes.Unknown, codes.Unavailable, codes.DataLoss:
		// Server-side bugs. Always include the error detail.
		logger.Error("rpc", append(attrs, "error", sanitizeLogString(st.Message()))...)
	default:
		// Client errors (InvalidArgument, NotFound, PermissionDenied,
		// Unauthenticated, AlreadyExists, FailedPrecondition,
		// OutOfRange, ResourceExhausted, Aborted) — warn so they
		// surface in dashboards but don't drown out real errors.
		logger.Warn("rpc", append(attrs, "error", sanitizeLogString(st.Message()))...)
	}
}

// sanitizeLogString defangs newlines and carriage returns from
// caller-controlled error strings before they're logged. The default
// JSON slog handler escapes these on its own, but downstream log
// pipelines (text-handler dev runs, regex-based parsers in Loki /
// Datadog, raw stdout tailing) can be tricked into treating an
// embedded newline as a record boundary — a classic log-injection
// vector. Cheap defense-in-depth.
func sanitizeLogString(s string) string {
	if s == "" {
		return s
	}
	r := strings.NewReplacer("\n", "\\n", "\r", "\\r")
	return r.Replace(s)
}
