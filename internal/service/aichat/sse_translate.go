package aichat

import (
	"errors"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// errMarshalChunkNotImplemented is the sentinel returned by the
// stub `marshalChunk` until the real translator lands in a follow-
// up commit. Tests assert against this so the gap is greppable.
var errMarshalChunkNotImplemented = errors.New("aichat: marshalChunk not implemented")

// marshalChunk serializes a ServerEvent into a Vercel AI SDK
// UIMessageChunk JSON object — the JSON bytes ready to embed in an
// SSE `data: ...\n\n` line.
//
// Returns errMarshalChunkNotImplemented at this commit; the real
// implementation switches on the oneof variant, protojson-marshals
// the inner message, and splices in the `"type"` discriminator. See
// sse_translate_test.go for the per-variant wire-format spec.
func marshalChunk(ev *aiv1.ServerEvent) ([]byte, error) {
	if ev == nil || ev.GetEvent() == nil {
		return nil, errors.New("aichat: ServerEvent has no event variant set")
	}
	return nil, errMarshalChunkNotImplemented
}
