package aichat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/structpb"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// TestMarshalChunk asserts that every ServerEvent variant serializes
// to a JSON object conforming to the Vercel AI SDK UIMessageChunk
// wire format. Expected JSONs are taken verbatim from the Vercel
// spec (`uiMessageChunkSchema` in
// node_modules/ai/src/ui-message-stream/ui-message-chunks.ts) and
// the documentation examples in
// node_modules/ai/docs/04-ai-sdk-ui/50-stream-protocol.mdx.
//
// Assertions are JSON-deep-equal (assert.JSONEq), not byte-equal:
// `protojson` does not guarantee field-ordering byte-stability and
// the wire consumer (Vercel useChat via `parseJsonEventStream`)
// parses JSON, so semantic equality is the test that matches reality.
//
// This is the conformance test. If it stays green, the SSE wire
// produced by Pivox is exactly what `uiMessageChunkSchema.parse(...)`
// on the client accepts. If a chunk shape changes, the proto change
// MUST keep this test green or the change is wire-incompatible.
func TestMarshalChunk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ev   *aiv1.ServerEvent
		want string
	}{
		// ─── Lifecycle ──────────────────────────────────────────

		{
			name: "start",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Start{Start: &aiv1.Start{
				MessageId: "msg_1",
			}}},
			want: `{"type":"start","messageId":"msg_1"}`,
		},
		{
			name: "start_with_metadata",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Start{Start: &aiv1.Start{
				MessageId:       "msg_1",
				MessageMetadata: mustStruct(t, map[string]any{"createdAt": "2026-05-28T00:00:00Z"}),
			}}},
			want: `{"type":"start","messageId":"msg_1","messageMetadata":{"createdAt":"2026-05-28T00:00:00Z"}}`,
		},
		{
			name: "start_step",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_StartStep{
				StartStep: &aiv1.StartStep{},
			}},
			want: `{"type":"start-step"}`,
		},
		{
			name: "finish_step",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_FinishStep{
				FinishStep: &aiv1.FinishStep{},
			}},
			want: `{"type":"finish-step"}`,
		},
		{
			name: "finish_bare",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Finish{
				Finish: &aiv1.Finish{},
			}},
			want: `{"type":"finish"}`,
		},
		{
			name: "finish_with_reason_and_metadata",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Finish{Finish: &aiv1.Finish{
				FinishReason:    "stop",
				MessageMetadata: mustStruct(t, map[string]any{"inputTokens": float64(42), "outputTokens": float64(128)}),
			}}},
			want: `{"type":"finish","finishReason":"stop","messageMetadata":{"inputTokens":42,"outputTokens":128}}`,
		},
		{
			name: "abort_bare",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Abort{
				Abort: &aiv1.Abort{},
			}},
			want: `{"type":"abort"}`,
		},
		{
			name: "abort_with_reason",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Abort{Abort: &aiv1.Abort{
				Reason: "user cancelled",
			}}},
			want: `{"type":"abort","reason":"user cancelled"}`,
		},
		{
			name: "error",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Error{Error: &aiv1.Error{
				ErrorText: "error message",
			}}},
			want: `{"type":"error","errorText":"error message"}`,
		},
		{
			name: "message_metadata",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_MessageMetadata{
				MessageMetadata: &aiv1.MessageMetadata{
					MessageMetadata: mustStruct(t, map[string]any{
						"model":        "claude-sonnet-4-5",
						"inputTokens":  float64(100),
						"outputTokens": float64(250),
					}),
				},
			}},
			want: `{"type":"message-metadata","messageMetadata":{"model":"claude-sonnet-4-5","inputTokens":100,"outputTokens":250}}`,
		},

		// ─── Text ───────────────────────────────────────────────

		{
			name: "text_start",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextStart{TextStart: &aiv1.TextStart{
				Id: "msg_68679a454370819ca74c8eb3d04379630dd1afb72306ca5d",
			}}},
			want: `{"type":"text-start","id":"msg_68679a454370819ca74c8eb3d04379630dd1afb72306ca5d"}`,
		},
		{
			name: "text_delta",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{
				Id:    "msg_68679a454370819ca74c8eb3d04379630dd1afb72306ca5d",
				Delta: "Hello",
			}}},
			want: `{"type":"text-delta","id":"msg_68679a454370819ca74c8eb3d04379630dd1afb72306ca5d","delta":"Hello"}`,
		},
		{
			// providerMetadata is the single most likely field to be
			// silently dropped in a proto -> JSON translator. Exercise
			// it on at least one text chunk so a translator that
			// forgets to copy the field fails this test loudly.
			name: "text_delta_with_provider_metadata",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{
				Id:    "msg_1",
				Delta: "Hello",
				ProviderMetadata: mustStruct(t, map[string]any{
					"anthropic": map[string]any{"cacheReadInputTokens": float64(50)},
				}),
			}}},
			want: `{"type":"text-delta","id":"msg_1","delta":"Hello","providerMetadata":{"anthropic":{"cacheReadInputTokens":50}}}`,
		},
		{
			name: "text_end",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextEnd{TextEnd: &aiv1.TextEnd{
				Id: "msg_68679a454370819ca74c8eb3d04379630dd1afb72306ca5d",
			}}},
			want: `{"type":"text-end","id":"msg_68679a454370819ca74c8eb3d04379630dd1afb72306ca5d"}`,
		},

		// ─── Reasoning ──────────────────────────────────────────

		{
			name: "reasoning_start",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ReasoningStart{ReasoningStart: &aiv1.ReasoningStart{
				Id: "reasoning_123",
			}}},
			want: `{"type":"reasoning-start","id":"reasoning_123"}`,
		},
		{
			name: "reasoning_delta",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ReasoningDelta{ReasoningDelta: &aiv1.ReasoningDelta{
				Id:    "reasoning_123",
				Delta: "This is some reasoning",
			}}},
			want: `{"type":"reasoning-delta","id":"reasoning_123","delta":"This is some reasoning"}`,
		},
		{
			name: "reasoning_end",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ReasoningEnd{ReasoningEnd: &aiv1.ReasoningEnd{
				Id: "reasoning_123",
			}}},
			want: `{"type":"reasoning-end","id":"reasoning_123"}`,
		},

		// ─── Tool input ─────────────────────────────────────────

		{
			name: "tool_input_start",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputStart{ToolInputStart: &aiv1.ToolInputStart{
				ToolCallId: "call_fJdQDqnXeGxTmr4E3YPSR7Ar",
				ToolName:   "getWeatherInformation",
			}}},
			want: `{"type":"tool-input-start","toolCallId":"call_fJdQDqnXeGxTmr4E3YPSR7Ar","toolName":"getWeatherInformation"}`,
		},
		{
			name: "tool_input_start_with_flags",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputStart{ToolInputStart: &aiv1.ToolInputStart{
				ToolCallId:       "call_abc",
				ToolName:         "search",
				ProviderExecuted: true,
				Dynamic:          true,
				Title:            "Search the web",
			}}},
			want: `{"type":"tool-input-start","toolCallId":"call_abc","toolName":"search","providerExecuted":true,"dynamic":true,"title":"Search the web"}`,
		},
		{
			name: "tool_input_delta",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputDelta{ToolInputDelta: &aiv1.ToolInputDelta{
				ToolCallId:     "call_fJdQDqnXeGxTmr4E3YPSR7Ar",
				InputTextDelta: "San Francisco",
			}}},
			want: `{"type":"tool-input-delta","toolCallId":"call_fJdQDqnXeGxTmr4E3YPSR7Ar","inputTextDelta":"San Francisco"}`,
		},
		{
			name: "tool_input_available",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputAvailable{ToolInputAvailable: &aiv1.ToolInputAvailable{
				ToolCallId: "call_fJdQDqnXeGxTmr4E3YPSR7Ar",
				ToolName:   "getWeatherInformation",
				Input:      mustStruct(t, map[string]any{"city": "San Francisco"}),
			}}},
			want: `{"type":"tool-input-available","toolCallId":"call_fJdQDqnXeGxTmr4E3YPSR7Ar","toolName":"getWeatherInformation","input":{"city":"San Francisco"}}`,
		},
		{
			// Same providerMetadata propagation guard, on a different
			// chunk family. Two coverage points across families is
			// enough; expanding to every chunk would be padding.
			name: "tool_input_available_with_provider_metadata",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputAvailable{ToolInputAvailable: &aiv1.ToolInputAvailable{
				ToolCallId: "call_abc",
				ToolName:   "search",
				Input:      mustStruct(t, map[string]any{"q": "weather"}),
				ProviderMetadata: mustStruct(t, map[string]any{
					"anthropic": map[string]any{"cacheControl": "ephemeral"},
				}),
			}}},
			want: `{"type":"tool-input-available","toolCallId":"call_abc","toolName":"search","input":{"q":"weather"},"providerMetadata":{"anthropic":{"cacheControl":"ephemeral"}}}`,
		},
		{
			name: "tool_input_error",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputError{ToolInputError: &aiv1.ToolInputError{
				ToolCallId: "call_abc",
				ToolName:   "getWeatherInformation",
				Input:      mustStruct(t, map[string]any{"city": float64(42)}),
				ErrorText:  "expected string for city, got number",
			}}},
			want: `{"type":"tool-input-error","toolCallId":"call_abc","toolName":"getWeatherInformation","input":{"city":42},"errorText":"expected string for city, got number"}`,
		},
		{
			name: "tool_approval_request",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolApprovalRequest{ToolApprovalRequest: &aiv1.ToolApprovalRequest{
				ApprovalId: "appr_1",
				ToolCallId: "call_abc",
			}}},
			want: `{"type":"tool-approval-request","approvalId":"appr_1","toolCallId":"call_abc"}`,
		},

		// ─── Tool output ────────────────────────────────────────

		{
			name: "tool_output_available",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolOutputAvailable{ToolOutputAvailable: &aiv1.ToolOutputAvailable{
				ToolCallId: "call_fJdQDqnXeGxTmr4E3YPSR7Ar",
				Output:     mustStruct(t, map[string]any{"city": "San Francisco", "weather": "sunny"}),
			}}},
			want: `{"type":"tool-output-available","toolCallId":"call_fJdQDqnXeGxTmr4E3YPSR7Ar","output":{"city":"San Francisco","weather":"sunny"}}`,
		},
		{
			name: "tool_output_available_preliminary",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolOutputAvailable{ToolOutputAvailable: &aiv1.ToolOutputAvailable{
				ToolCallId:  "call_abc",
				Output:      mustStruct(t, map[string]any{"partial": true}),
				Preliminary: true,
			}}},
			want: `{"type":"tool-output-available","toolCallId":"call_abc","output":{"partial":true},"preliminary":true}`,
		},
		{
			name: "tool_output_error",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolOutputError{ToolOutputError: &aiv1.ToolOutputError{
				ToolCallId: "call_abc",
				ErrorText:  "tool timed out after 30s",
			}}},
			want: `{"type":"tool-output-error","toolCallId":"call_abc","errorText":"tool timed out after 30s"}`,
		},
		{
			name: "tool_output_denied",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolOutputDenied{ToolOutputDenied: &aiv1.ToolOutputDenied{
				ToolCallId: "call_abc",
			}}},
			want: `{"type":"tool-output-denied","toolCallId":"call_abc"}`,
		},

		// ─── Sources & files ────────────────────────────────────

		{
			name: "source_url",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_SourceUrl{SourceUrl: &aiv1.SourceUrl{
				SourceId: "https://example.com",
				Url:      "https://example.com",
			}}},
			want: `{"type":"source-url","sourceId":"https://example.com","url":"https://example.com"}`,
		},
		{
			name: "source_url_with_title",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_SourceUrl{SourceUrl: &aiv1.SourceUrl{
				SourceId: "src_1",
				Url:      "https://example.com/article",
				Title:    "Example Article",
			}}},
			want: `{"type":"source-url","sourceId":"src_1","url":"https://example.com/article","title":"Example Article"}`,
		},
		{
			name: "source_document",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_SourceDocument{SourceDocument: &aiv1.SourceDocument{
				SourceId:  "https://example.com",
				MediaType: "file",
				Title:     "Title",
			}}},
			want: `{"type":"source-document","sourceId":"https://example.com","mediaType":"file","title":"Title"}`,
		},
		{
			name: "file",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_File{File: &aiv1.File{
				Url:       "https://example.com/file.png",
				MediaType: "image/png",
			}}},
			want: `{"type":"file","url":"https://example.com/file.png","mediaType":"image/png"}`,
		},

		// ─── Data parts (data-<name>) ───────────────────────────

		{
			name: "data_weather",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Data{Data: &aiv1.DataPart{
				Name: "weather",
				Data: mustStruct(t, map[string]any{"location": "SF", "temperature": float64(100)}),
			}}},
			want: `{"type":"data-weather","data":{"location":"SF","temperature":100}}`,
		},
		{
			name: "data_artifact_streaming_snapshot",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Data{Data: &aiv1.DataPart{
				Name: "artifact",
				Id:   "art_1",
				Data: mustStruct(t, map[string]any{
					"phase":   "streaming",
					"kind":    "code",
					"title":   "main.go",
					"content": "package main\n",
				}),
			}}},
			want: `{"type":"data-artifact","id":"art_1","data":{"phase":"streaming","kind":"code","title":"main.go","content":"package main\n"}}`,
		},
		{
			name: "data_artifact_complete_snapshot",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Data{Data: &aiv1.DataPart{
				Name: "artifact",
				Id:   "art_1",
				Data: mustStruct(t, map[string]any{
					"phase":           "complete",
					"kind":            "code",
					"title":           "main.go",
					"content":         "package main\n\nfunc main() {}\n",
					"mimeType":        "text/x-go",
					"sizeBytes":       float64(32),
					"artifactVersion": "organizations/acme/users/alice/conversations/c1/artifacts/a1/versions/v1",
				}),
			}}},
			want: `{"type":"data-artifact","id":"art_1","data":{"phase":"complete","kind":"code","title":"main.go","content":"package main\n\nfunc main() {}\n","mimeType":"text/x-go","sizeBytes":32,"artifactVersion":"organizations/acme/users/alice/conversations/c1/artifacts/a1/versions/v1"}}`,
		},
		{
			name: "data_progress_transient",
			ev: &aiv1.ServerEvent{Event: &aiv1.ServerEvent_Data{Data: &aiv1.DataPart{
				Name:      "progress",
				Data:      mustStruct(t, map[string]any{"phase": "embedding", "pct": float64(42)}),
				Transient: true,
			}}},
			want: `{"type":"data-progress","data":{"phase":"embedding","pct":42},"transient":true}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := marshalChunk(tt.ev)
			require.NoError(t, err, "marshalChunk returned error")
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}

// TestMarshalChunk_UnsetVariant asserts that a ServerEvent whose
// oneof variant is not set is a programmer error surfaced as an
// error — never a silent empty emission. A nil oneof in the wild
// would mean the streaming pipeline produced a malformed event;
// the caller (SSE handler) must decide whether to log and skip or
// terminate the stream.
func TestMarshalChunk_UnsetVariant(t *testing.T) {
	t.Parallel()

	got, err := marshalChunk(&aiv1.ServerEvent{})
	require.Error(t, err)
	assert.Nil(t, got)
}

// mustStruct constructs a structpb.Struct from a Go map. The test
// fails (not the production code) if the map can't be converted —
// avoids polluting the table with err returns for fixture data.
//
// Numeric literals in the input map should be float64 (the JSON
// number type) to round-trip cleanly. Using int would coerce
// through structpb's number wrapping and the resulting JSON would
// still be a number, but explicit float64 keeps the table honest
// about what's on the wire.
func mustStruct(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	require.NoError(t, err)
	return s
}
