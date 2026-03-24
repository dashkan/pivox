package operations

import (
	"context"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/dashkan/pivox/internal/lro"
)

// OperationsServer implements longrunningpb.OperationsServer by delegating to
// an LRO Manager.
type OperationsServer struct {
	longrunningpb.UnimplementedOperationsServer
	lro *lro.Manager
}

// NewOperationsServer returns a new OperationsServer backed by the given LRO
// manager.
func NewOperationsServer(lro *lro.Manager) *OperationsServer {
	return &OperationsServer{lro: lro}
}

// GetOperation returns the latest state of a long-running operation.
func (s *OperationsServer) GetOperation(ctx context.Context, req *longrunningpb.GetOperationRequest) (*longrunningpb.Operation, error) {
	if req.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "name is required")
	}
	return s.lro.GetOperation(ctx, req.GetName())
}

// ListOperations lists operations that match the specified filter in the
// request. The server's name field is used as the operation prefix.
func (s *OperationsServer) ListOperations(ctx context.Context, req *longrunningpb.ListOperationsRequest) (*longrunningpb.ListOperationsResponse, error) {
	pageSize := req.GetPageSize()
	if pageSize <= 0 {
		pageSize = 100
	}

	ops, err := s.lro.ListOperations(ctx, req.GetName(), pageSize)
	if err != nil {
		return nil, err
	}

	return &longrunningpb.ListOperationsResponse{
		Operations: ops,
	}, nil
}

// WaitOperation waits until the specified long-running operation is done or
// reaches at most the given timeout, returning the latest state.
func (s *OperationsServer) WaitOperation(ctx context.Context, req *longrunningpb.WaitOperationRequest) (*longrunningpb.Operation, error) {
	if req.GetTimeout() != nil {
		timeout := req.GetTimeout().AsDuration()
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return s.lro.WaitOperation(ctx, req.GetName())
}

// DeleteOperation deletes a long-running operation. This method indicates that
// the client is no longer interested in the operation result.
func (s *OperationsServer) DeleteOperation(ctx context.Context, req *longrunningpb.DeleteOperationRequest) (*emptypb.Empty, error) {
	if req.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "name is required")
	}
	if err := s.lro.DeleteOperation(ctx, req.GetName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

// CancelOperation starts asynchronous cancellation on a long-running
// operation.
func (s *OperationsServer) CancelOperation(ctx context.Context, req *longrunningpb.CancelOperationRequest) (*emptypb.Empty, error) {
	if req.GetName() == "" {
		return nil, status.Errorf(codes.InvalidArgument, "name is required")
	}
	if err := s.lro.CancelOperation(ctx, req.GetName()); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
