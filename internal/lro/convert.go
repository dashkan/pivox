package lro

import (
	"encoding/json"
	"fmt"

	"cloud.google.com/go/longrunning/autogen/longrunningpb"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	db "github.com/dashkan/pivox/internal/db/generated"
)

func marshalAny(msg proto.Message) (json.RawMessage, error) {
	a, err := anypb.New(msg)
	if err != nil {
		return nil, err
	}
	b, err := protojson.Marshal(a)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func unmarshalAny(data []byte) (*anypb.Any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	a := &anypb.Any{}
	if err := protojson.Unmarshal(data, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DoneOperation creates an already-completed Operation proto wrapping the
// given response. Use this for mutations that complete synchronously but
// whose proto return type is google.longrunning.Operation.
func DoneOperation(response proto.Message) (*longrunningpb.Operation, error) {
	a, err := anypb.New(response)
	if err != nil {
		return nil, fmt.Errorf("marshal response to Any: %w", err)
	}
	return &longrunningpb.Operation{
		Name: fmt.Sprintf("operations/%s", response.ProtoReflect().Descriptor().FullName()),
		Done: true,
		Result: &longrunningpb.Operation_Response{
			Response: a,
		},
	}, nil
}

func dbToProto(op db.Operation) (*longrunningpb.Operation, error) {
	// Construct operation name: "operations/{prefix}/{uuid}"
	name := fmt.Sprintf("operations/%s/%s", op.Prefix, op.ID.String())

	pbOp := &longrunningpb.Operation{
		Name: name,
		Done: op.Done,
	}

	if len(op.Metadata) > 0 {
		meta, err := unmarshalAny(op.Metadata)
		if err != nil {
			return nil, err
		}
		pbOp.Metadata = meta
	}

	if op.Done {
		if op.ErrorCode.Valid && op.ErrorCode.Int32 != 0 {
			msg := ""
			if op.ErrorMessage.Valid {
				msg = op.ErrorMessage.String
			}
			pbOp.Result = &longrunningpb.Operation_Error{
				Error: &spb.Status{
					Code:    op.ErrorCode.Int32,
					Message: msg,
				},
			}
		} else if len(op.Result) > 0 {
			result, err := unmarshalAny(op.Result)
			if err != nil {
				return nil, err
			}
			pbOp.Result = &longrunningpb.Operation_Response{
				Response: result,
			}
		}
	}

	return pbOp, nil
}
