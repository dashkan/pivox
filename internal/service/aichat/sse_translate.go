package aichat

import (
	"encoding/json"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

// translateToSSE converts a ServerEvent proto to a Vercel AI SDK UI
// message stream JSON line.
func translateToSSE(ev *aiv1.ServerEvent) string {
	var out map[string]any

	switch e := ev.GetEvent().(type) {
	case *aiv1.ServerEvent_TextStart:
		out = map[string]any{"type": "text-start", "id": e.TextStart.GetMessageId()}

	case *aiv1.ServerEvent_TextDelta:
		out = map[string]any{"type": "text-delta", "delta": e.TextDelta.GetDelta()}

	case *aiv1.ServerEvent_TextEnd:
		out = map[string]any{"type": "text-end"}

	case *aiv1.ServerEvent_ReasoningStart:
		out = map[string]any{"type": "reasoning-start", "id": e.ReasoningStart.GetMessageId()}

	case *aiv1.ServerEvent_ReasoningDelta:
		out = map[string]any{"type": "reasoning-delta", "delta": e.ReasoningDelta.GetDelta()}

	case *aiv1.ServerEvent_ReasoningEnd:
		out = map[string]any{"type": "reasoning-end"}

	case *aiv1.ServerEvent_ToolCallStart:
		out = map[string]any{
			"type":         "tool-call-start",
			"tool_call_id": e.ToolCallStart.GetToolCallId(),
			"tool":         e.ToolCallStart.GetTool(),
		}

	case *aiv1.ServerEvent_ToolCallDelta:
		out = map[string]any{
			"type":         "tool-call-delta",
			"tool_call_id": e.ToolCallDelta.GetToolCallId(),
			"delta":        e.ToolCallDelta.GetDelta(),
		}

	case *aiv1.ServerEvent_ToolInputAvailable:
		out = map[string]any{
			"type":         "tool-input-available",
			"tool_call_id": e.ToolInputAvailable.GetToolCallId(),
			"tool":         e.ToolInputAvailable.GetTool(),
			"input_json":   e.ToolInputAvailable.GetInputJson(),
			"server_side":  e.ToolInputAvailable.GetServerSide(),
		}

	case *aiv1.ServerEvent_ToolOutputAvailable:
		out = map[string]any{
			"type":         "tool-output-available",
			"tool_call_id": e.ToolOutputAvailable.GetToolCallId(),
			"result_json":  e.ToolOutputAvailable.GetResultJson(),
		}

	case *aiv1.ServerEvent_ToolError:
		out = map[string]any{
			"type":         "tool-error",
			"tool_call_id": e.ToolError.GetToolCallId(),
			"error":        e.ToolError.GetErrorMessage(),
		}

	case *aiv1.ServerEvent_ToolApprovalRequested:
		out = map[string]any{
			"type":         "tool-approval-requested",
			"tool_call_id": e.ToolApprovalRequested.GetToolCallId(),
			"tool":         e.ToolApprovalRequested.GetTool(),
			"input_json":   e.ToolApprovalRequested.GetInputJson(),
		}

	case *aiv1.ServerEvent_ArtifactStart:
		out = map[string]any{
			"type": "data-artifact-start",
			"data": map[string]any{
				"id":    e.ArtifactStart.GetArtifactId(),
				"type":  e.ArtifactStart.GetType(),
				"title": e.ArtifactStart.GetTitle(),
			},
		}

	case *aiv1.ServerEvent_ArtifactDelta:
		out = map[string]any{
			"type": "data-artifact-delta",
			"data": map[string]any{
				"id":    e.ArtifactDelta.GetArtifactId(),
				"delta": e.ArtifactDelta.GetDelta(),
			},
		}

	case *aiv1.ServerEvent_ArtifactEnd:
		out = map[string]any{
			"type": "data-artifact-end",
			"data": map[string]any{
				"id":               e.ArtifactEnd.GetArtifactId(),
				"artifact_version": e.ArtifactEnd.GetArtifactVersion(),
				"mime_type":        e.ArtifactEnd.GetMimeType(),
				"size_bytes":       e.ArtifactEnd.GetSizeBytes(),
			},
		}

	case *aiv1.ServerEvent_ArtifactError:
		out = map[string]any{
			"type": "data-artifact-error",
			"data": map[string]any{
				"id":    e.ArtifactError.GetArtifactId(),
				"error": e.ArtifactError.GetStatus().GetMessage(),
			},
		}

	case *aiv1.ServerEvent_MessageMetadata:
		out = map[string]any{
			"type":          "message-metadata",
			"id":            e.MessageMetadata.GetMessageId(),
			"model":         e.MessageMetadata.GetModel(),
			"input_tokens":  e.MessageMetadata.GetInputTokens(),
			"output_tokens": e.MessageMetadata.GetOutputTokens(),
		}

	case *aiv1.ServerEvent_Done:
		out = map[string]any{"type": "finish"}

	case *aiv1.ServerEvent_StreamError:
		out = map[string]any{
			"type":  "error",
			"error": e.StreamError.GetStatus().GetMessage(),
		}

	case *aiv1.ServerEvent_DataPart:
		out = map[string]any{
			"type": e.DataPart.GetType(),
			"data": e.DataPart.GetDataJson(),
		}

	default:
		out = map[string]any{"type": "unknown"}
	}

	b, _ := json.Marshal(out)
	return string(b)
}
