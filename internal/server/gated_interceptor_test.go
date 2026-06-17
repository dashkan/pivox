package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestMethodPredicates(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		pivox      bool
		pivoxOrLRO bool
	}{
		{"pivox service", "/pivox.api.v1.Organizations/ListOrganizations", true, true},
		{"pivox ai stream", "/pivox.ai.v1.AiChat/StreamGenerateContent", true, true},
		{"LRO", "/google.longrunning.Operations/GetOperation", false, true},
		{"reflection v1", "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", false, false},
		{"reflection v1alpha", "/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo", false, false},
		{"health", "/grpc.health.v1.Health/Check", false, false},
		{"other google (not LRO)", "/google.iam.v1.IAMPolicy/GetIamPolicy", false, false},
		{"empty", "", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.pivox, IsPivox(tc.method), "IsPivox")
			assert.Equal(t, tc.pivoxOrLRO, IsPivoxOrLRO(tc.method), "IsPivoxOrLRO")
		})
	}
}

func TestGatedUnaryInterceptor_SkipsInnerWhenPredicateFalse(t *testing.T) {
	innerRan := false
	inner := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, _ grpc.UnaryHandler) (any, error) {
		innerRan = true
		return nil, errors.New("inner should not run")
	}
	handlerRan := false
	handler := func(_ context.Context, _ any) (any, error) {
		handlerRan = true
		return "ok", nil
	}

	wrapped := GatedUnaryInterceptor(func(string) bool { return false }, inner)
	resp, err := wrapped(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	}, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.False(t, innerRan, "inner interceptor must be skipped")
	assert.True(t, handlerRan, "handler must run directly")
}

func TestGatedUnaryInterceptor_RunsInnerWhenPredicateTrue(t *testing.T) {
	innerRan := false
	inner := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		innerRan = true
		return handler(ctx, req)
	}
	handler := func(_ context.Context, _ any) (any, error) { return "ok", nil }

	wrapped := GatedUnaryInterceptor(func(string) bool { return true }, inner)
	resp, err := wrapped(context.Background(), nil, &grpc.UnaryServerInfo{
		FullMethod: "/pivox.api.v1.Spaces/GetSpace",
	}, handler)

	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, innerRan, "inner interceptor must run for a matched method")
}

func TestGatedStreamInterceptor_SkipsInnerWhenPredicateFalse(t *testing.T) {
	innerRan := false
	inner := func(_ any, _ grpc.ServerStream, _ *grpc.StreamServerInfo, _ grpc.StreamHandler) error {
		innerRan = true
		return errors.New("inner should not run")
	}
	handlerRan := false
	handler := func(_ any, _ grpc.ServerStream) error {
		handlerRan = true
		return nil
	}

	wrapped := GatedStreamInterceptor(func(string) bool { return false }, inner)
	err := wrapped(nil, &mockServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{
		FullMethod: "/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
	}, handler)

	require.NoError(t, err)
	assert.False(t, innerRan, "inner interceptor must be skipped")
	assert.True(t, handlerRan, "handler must run directly")
}

func TestGatedStreamInterceptor_RunsInnerWhenPredicateTrue(t *testing.T) {
	innerRan := false
	inner := func(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		innerRan = true
		return handler(srv, ss)
	}
	handler := func(_ any, _ grpc.ServerStream) error { return nil }

	wrapped := GatedStreamInterceptor(func(string) bool { return true }, inner)
	err := wrapped(nil, &mockServerStream{ctx: context.Background()}, &grpc.StreamServerInfo{
		FullMethod: "/pivox.ai.v1.AiChat/StreamGenerateContent",
	}, handler)

	require.NoError(t, err)
	assert.True(t, innerRan, "inner interceptor must run for a matched method")
}
