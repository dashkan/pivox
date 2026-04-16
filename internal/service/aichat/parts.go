package aichat

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

var pjOpts = protojson.MarshalOptions{EmitUnpopulated: false}

// marshalParts serializes a list of MessagePart protos to a JSON array
// suitable for storage in a JSONB column.
func marshalParts(parts []*aiv1.MessagePart) (json.RawMessage, error) {
	arr := make([]json.RawMessage, 0, len(parts))
	for _, p := range parts {
		b, err := pjOpts.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("marshal part: %w", err)
		}
		arr = append(arr, b)
	}
	return json.Marshal(arr)
}

// unmarshalParts deserializes a JSON array from a JSONB column back into
// MessagePart protos.
func unmarshalParts(data json.RawMessage) ([]*aiv1.MessagePart, error) {
	if len(data) == 0 || string(data) == "[]" {
		return nil, nil
	}

	var rawParts []json.RawMessage
	if err := json.Unmarshal(data, &rawParts); err != nil {
		return nil, fmt.Errorf("unmarshal parts array: %w", err)
	}

	parts := make([]*aiv1.MessagePart, 0, len(rawParts))
	for _, raw := range rawParts {
		p := &aiv1.MessagePart{}
		if err := protojson.Unmarshal(raw, p); err != nil {
			return nil, fmt.Errorf("unmarshal part: %w", err)
		}
		parts = append(parts, p)
	}
	return parts, nil
}
