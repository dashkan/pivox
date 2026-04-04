package agentstream

import (
	"fmt"
	"regexp"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// RedactSecretKeyPattern matches "secretAccessKey":"<value>" in protojson output.
var RedactSecretKeyPattern = regexp.MustCompile(`("secretAccessKey"\s*:\s*)"[^"]*"`)

// MarshalAndRedact marshals a protobuf message to JSON using protojson and
// redacts secret_access_key values by replacing them with "***".
func MarshalAndRedact(msg proto.Message) ([]byte, error) {
	data, err := protojson.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("protojson marshal: %w", err)
	}
	redacted := RedactSecretKeyPattern.ReplaceAll(data, []byte(`$1"***"`))
	return redacted, nil
}
