package server

import (
	"context"
	"errors"
	"strings"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// fieldMaskFullName is the protobuf full name for google.protobuf.FieldMask.
const fieldMaskFullName protoreflect.FullName = "google.protobuf.FieldMask"

// FieldMaskAwareValidationInterceptor returns a gRPC unary server interceptor
// that validates requests using protovalidate with field-mask awareness.
//
// For requests containing a FieldMask field with non-empty paths (i.e., Update
// RPCs following AIP-134), it clones the request and clears the nested resource
// message's sub-fields before validation. This prevents protovalidate from
// rejecting Update requests that omit IMMUTABLE/REQUIRED fields not being
// updated — matching standard Google API PATCH semantics where clients only
// send the fields they want to change.
//
// For all other requests, validation runs normally on the full message.
func FieldMaskAwareValidationInterceptor(validator protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		msg, ok := req.(proto.Message)
		if !ok {
			return nil, status.Errorf(codes.Internal, "unsupported message type: %T", req)
		}

		if err := validateWithFieldMaskAwareness(msg, validator); err != nil {
			return nil, err
		}

		return handler(ctx, req)
	}
}

// validateWithFieldMaskAwareness validates the message using protovalidate. For
// AIP-134 Update requests with a non-empty FieldMask, it uses protovalidate's
// WithFilter to validate only the resource fields listed in the mask — skipping
// IMMUTABLE/REQUIRED fields the client isn't updating. For all other requests,
// validation runs on the full message.
func validateWithFieldMaskAwareness(msg proto.Message, validator protovalidate.Validator) error {
	maskFD, resourceFD := findFieldMaskAndResource(msg.ProtoReflect().Descriptor())
	if maskFD == nil || resourceFD == nil {
		return validateMsg(msg, validator)
	}

	// Check if the mask has paths (partial update).
	maskVal := msg.ProtoReflect().Get(maskFD)
	maskMsg := maskVal.Message()
	pathsFD := maskMsg.Descriptor().Fields().ByName("paths")
	if pathsFD == nil || maskMsg.Get(pathsFD).List().Len() == 0 {
		// Empty/nil mask — validate normally (handler treats as full update).
		return validateMsg(msg, validator)
	}

	// Build a set of top-level field names from the mask paths.
	pathsList := maskMsg.Get(pathsFD).List()
	maskedFields := make(map[protoreflect.Name]bool, pathsList.Len())
	for i := 0; i < pathsList.Len(); i++ {
		path := pathsList.Get(i).String()
		// Extract top-level field name (e.g., "restrictions.api_targets" → "restrictions").
		if idx := strings.IndexByte(path, '.'); idx >= 0 {
			path = path[:idx]
		}
		maskedFields[protoreflect.Name(path)] = true
	}

	// Validate with a filter that only evaluates resource fields in the mask.
	resourceFullName := resourceFD.Message().FullName()
	filter := protovalidate.FilterFunc(func(m protoreflect.Message, d protoreflect.Descriptor) bool {
		fd, ok := d.(protoreflect.FieldDescriptor)
		if !ok {
			return true // always validate message-level and oneof-level rules
		}
		// Only filter fields inside the resource message, not the request wrapper.
		if m.Descriptor().FullName() != resourceFullName {
			return true
		}
		return maskedFields[fd.Name()]
	})

	return validateMsg(msg, validator, protovalidate.WithFilter(filter))
}

// findFieldMaskAndResource inspects a message descriptor for the AIP-134
// Update pattern: one google.protobuf.FieldMask field and one other
// message-kind field (the resource). Returns (nil, nil) if the pattern
// doesn't match.
func findFieldMaskAndResource(desc protoreflect.MessageDescriptor) (
	maskFD, resourceFD protoreflect.FieldDescriptor,
) {
	fields := desc.Fields()
	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)
		if fd.Kind() != protoreflect.MessageKind {
			continue
		}
		if fd.Message().FullName() == fieldMaskFullName {
			maskFD = fd
		} else if resourceFD == nil {
			resourceFD = fd
		}
	}
	return maskFD, resourceFD
}

// validateMsg runs protovalidate and converts errors to gRPC status codes.
// Matches the error handling from grpc-ecosystem/go-grpc-middleware's
// protovalidate interceptor.
func validateMsg(msg proto.Message, validator protovalidate.Validator, opts ...protovalidate.ValidationOption) error {
	err := validator.Validate(msg, opts...)
	if err == nil {
		return nil
	}
	var valErr *protovalidate.ValidationError
	if errors.As(err, &valErr) {
		st := status.New(codes.InvalidArgument, err.Error())
		ds, detErr := st.WithDetails(valErr.ToProto())
		if detErr != nil {
			return st.Err()
		}
		return ds.Err()
	}
	return status.Error(codes.Internal, err.Error())
}
