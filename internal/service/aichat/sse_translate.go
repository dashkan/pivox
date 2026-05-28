package aichat

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// errUnsetVariant is the sentinel returned by marshalChunk when a
// ServerEvent's `event` oneof is not set. Greppable so callers can
// match against it without string comparison.
var errUnsetVariant = errors.New("aichat: ServerEvent has no event variant set")

// marshalChunk serializes a ServerEvent into a Vercel AI SDK
// UIMessageChunk JSON object — the JSON bytes ready to embed in an
// SSE `data: ...\n\n` line.
//
// The chunk shape produced by this function is exactly what
// `uiMessageChunkSchema.parse(...)` on the client accepts:
// camelCase field names (protojson default), with a `type`
// discriminator spliced in for each variant. The `data-<name>`
// variant is special-cased: the dynamic suffix lives in DataPart's
// proto `name` field, the wire `type` is built as `"data-" + name`,
// and the `name` field is NOT emitted (only `type`, `id`, `data`,
// `transient` cross the wire).
//
// See sse_translate_test.go for the per-variant spec, asserted
// JSON-equal against examples copied from the Vercel SDK.
func marshalChunk(ev *aiv1.ServerEvent) ([]byte, error) {
	if ev == nil || ev.GetEvent() == nil {
		return nil, errUnsetVariant
	}
	switch e := ev.GetEvent().(type) {
	// ─── Lifecycle ─────────────────────────────────────────
	case *aiv1.ServerEvent_Start:
		return marshalTyped("start", e.Start)
	case *aiv1.ServerEvent_StartStep:
		return marshalTyped("start-step", e.StartStep)
	case *aiv1.ServerEvent_FinishStep:
		return marshalTyped("finish-step", e.FinishStep)
	case *aiv1.ServerEvent_Finish:
		return marshalTyped("finish", e.Finish)
	case *aiv1.ServerEvent_Abort:
		return marshalTyped("abort", e.Abort)
	case *aiv1.ServerEvent_Error:
		return marshalTyped("error", e.Error)
	case *aiv1.ServerEvent_MessageMetadata:
		return marshalTyped("message-metadata", e.MessageMetadata)

	// ─── Text ──────────────────────────────────────────────
	case *aiv1.ServerEvent_TextStart:
		return marshalTyped("text-start", e.TextStart)
	case *aiv1.ServerEvent_TextDelta:
		return marshalTyped("text-delta", e.TextDelta)
	case *aiv1.ServerEvent_TextEnd:
		return marshalTyped("text-end", e.TextEnd)

	// ─── Reasoning ─────────────────────────────────────────
	case *aiv1.ServerEvent_ReasoningStart:
		return marshalTyped("reasoning-start", e.ReasoningStart)
	case *aiv1.ServerEvent_ReasoningDelta:
		return marshalTyped("reasoning-delta", e.ReasoningDelta)
	case *aiv1.ServerEvent_ReasoningEnd:
		return marshalTyped("reasoning-end", e.ReasoningEnd)

	// ─── Tool input ────────────────────────────────────────
	case *aiv1.ServerEvent_ToolInputStart:
		return marshalTyped("tool-input-start", e.ToolInputStart)
	case *aiv1.ServerEvent_ToolInputDelta:
		return marshalTyped("tool-input-delta", e.ToolInputDelta)
	case *aiv1.ServerEvent_ToolInputAvailable:
		return marshalTyped("tool-input-available", e.ToolInputAvailable)
	case *aiv1.ServerEvent_ToolInputError:
		return marshalTyped("tool-input-error", e.ToolInputError)
	case *aiv1.ServerEvent_ToolApprovalRequest:
		return marshalTyped("tool-approval-request", e.ToolApprovalRequest)

	// ─── Tool output ───────────────────────────────────────
	case *aiv1.ServerEvent_ToolOutputAvailable:
		return marshalTyped("tool-output-available", e.ToolOutputAvailable)
	case *aiv1.ServerEvent_ToolOutputError:
		return marshalTyped("tool-output-error", e.ToolOutputError)
	case *aiv1.ServerEvent_ToolOutputDenied:
		return marshalTyped("tool-output-denied", e.ToolOutputDenied)

	// ─── Sources & files ───────────────────────────────────
	case *aiv1.ServerEvent_SourceUrl:
		return marshalTyped("source-url", e.SourceUrl)
	case *aiv1.ServerEvent_SourceDocument:
		return marshalTyped("source-document", e.SourceDocument)
	case *aiv1.ServerEvent_File:
		return marshalTyped("file", e.File)

	// ─── Custom data parts ─────────────────────────────────
	case *aiv1.ServerEvent_Data:
		return marshalDataPart(e.Data)

	default:
		return nil, fmt.Errorf("aichat: unhandled ServerEvent variant %T", e)
	}
}

// marshalTyped marshals a proto message via protojson (lowerCamelCase
// field names, proto-field-number ordering) and splices a
// `"type":"<typ>"` field at the head of the resulting JSON object.
//
// Returns the JSON chunk ready for the SSE wire. The Vercel client
// (useChat via parseJsonEventStream + uiMessageChunkSchema) parses
// these chunks; field ordering is irrelevant to it.
func marshalTyped(typ string, msg proto.Message) ([]byte, error) {
	body, err := (protojson.MarshalOptions{}).Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("aichat: marshal %s: %w", typ, err)
	}
	return injectType(typ, body)
}

// injectType prepends a `"type":<typ>` field at the head of a JSON
// object. The input is assumed to be a well-formed object starting
// with `{` — protojson guarantees this for any proto.Message.
//
// `{}` becomes `{"type":"X"}`; `{"a":1}` becomes `{"type":"X","a":1}`.
func injectType(typ string, body []byte) ([]byte, error) {
	if len(body) == 0 || body[0] != '{' {
		return nil, fmt.Errorf("aichat: protojson output is not a JSON object: %q", body)
	}
	typeJSON, err := json.Marshal(typ)
	if err != nil {
		return nil, fmt.Errorf("aichat: marshal type %q: %w", typ, err)
	}
	// Bare `{}` — emit `{"type":<typ>}` with no trailing comma.
	if bytes.Equal(body, []byte("{}")) {
		out := make([]byte, 0, len(typeJSON)+10)
		out = append(out, []byte(`{"type":`)...)
		out = append(out, typeJSON...)
		out = append(out, '}')
		return out, nil
	}
	// Non-empty — splice `"type":<typ>,` right after the opening `{`.
	out := make([]byte, 0, len(body)+len(typeJSON)+10)
	out = append(out, []byte(`{"type":`)...)
	out = append(out, typeJSON...)
	out = append(out, ',')
	out = append(out, body[1:]...)
	return out, nil
}

// marshalDataPart serializes a DataPart into a data-<name> wire
// chunk. Special-cased vs marshalTyped because the wire `type`
// field carries a dynamic suffix from the proto `name` field, and
// the proto `name` itself is NOT emitted on the wire (it's just the
// source of the suffix).
//
// Output fields, in this order:
//   - type: "data-" + name   (always; non-empty enforced by proto
//     validator)
//   - id: stable replace-on-update key (omitted when empty)
//   - data: payload object (omitted when nil)
//   - transient: true to skip client persistence (omitted when false)
func marshalDataPart(d *aiv1.DataPart) ([]byte, error) {
	chunk := map[string]any{"type": "data-" + d.GetName()}
	if id := d.GetId(); id != "" {
		chunk["id"] = id
	}
	if data := d.GetData(); data != nil {
		chunk["data"] = data.AsMap()
	}
	if d.GetTransient() {
		chunk["transient"] = true
	}
	return json.Marshal(chunk)
}
