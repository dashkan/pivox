package server

import (
	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// ValidateStreamInterceptor returns a gRPC stream server interceptor that
// validates each client message via protovalidate before the handler sees it.
//
// Why this exists: gRPC server-streaming methods (e.g. AiChat.
// StreamGenerateContent) receive a single request message before streaming
// responses back. Without a streaming validator, every CEL rule on the
// request type — `string.in` enums, `string.min_len`, message-level CEL
// constraints — silently no-ops. The unary chain wires
// FieldMaskAwareValidationInterceptor for this; the stream chain needs the
// stream counterpart.
//
// Implementation wraps `RecvMsg` to validate after the framework deserializes
// the request. Reuses `validateWithFieldMaskAwareness` so field-mask aware
// validation works for any future streaming Update RPCs (none today; cheap
// to support).
//
// Bidirectional/client-streaming compatibility: every RecvMsg validates, so
// each client-side message in a stream gets checked. For server-streaming
// methods like StreamGenerateContent the call is single-shot; the cost is
// negligible.
func ValidateStreamInterceptor(validator protovalidate.Validator) grpc.StreamServerInterceptor {
	return func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		return handler(srv, &validatingServerStream{ServerStream: ss, validator: validator})
	}
}

// validatingServerStream wraps a grpc.ServerStream and validates each message
// received from the client. Outbound messages (server → client) are forwarded
// unchanged.
type validatingServerStream struct {
	grpc.ServerStream
	validator protovalidate.Validator
}

// RecvMsg validates the deserialized client message via protovalidate before
// returning it to the handler. Validation errors surface as
// codes.InvalidArgument with the same FieldViolation detail shape that the
// unary interceptor produces, so clients can switch on the typed error
// regardless of unary vs streaming RPC.
func (vss *validatingServerStream) RecvMsg(m any) error {
	if err := vss.ServerStream.RecvMsg(m); err != nil {
		return err
	}
	msg, ok := m.(proto.Message)
	if !ok {
		return status.Errorf(codes.Internal, "validate stream: unsupported message type: %T", m)
	}
	return validateWithFieldMaskAwareness(msg, vss.validator)
}
