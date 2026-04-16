package aichat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aiv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/ai/v1"
)

func TestTranslateToSSE(t *testing.T) {
	tests := []struct {
		name     string
		event    *aiv1.ServerEvent
		wantType string
	}{
		{
			"text_start",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextStart{TextStart: &aiv1.TextStart{MessageId: "m1"}}},
			"text-start",
		},
		{
			"text_delta",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{Delta: "hello"}}},
			"text-delta",
		},
		{
			"text_end",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextEnd{TextEnd: &aiv1.TextEnd{}}},
			"text-end",
		},
		{
			"done",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_Done{Done: &aiv1.Done{}}},
			"finish",
		},
		{
			"tool_input_available",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_ToolInputAvailable{ToolInputAvailable: &aiv1.ToolInputAvailable{
				ToolCallId: "tc1", Tool: "search", InputJson: `{"q":"x"}`, ServerSide: false,
			}}},
			"tool-input-available",
		},
		{
			"artifact_start",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_ArtifactStart{ArtifactStart: &aiv1.ArtifactStart{
				ArtifactId: "a1", Type: "code", Title: "example.py",
			}}},
			"data-artifact-start",
		},
		{
			"artifact_end",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_ArtifactEnd{ArtifactEnd: &aiv1.ArtifactEnd{
				ArtifactId: "a1", ArtifactVersion: "orgs/acme/conv/c1/art/a1/ver/v1", MimeType: "text/x-python", SizeBytes: 100,
			}}},
			"data-artifact-end",
		},
		{
			"stream_error",
			&aiv1.ServerEvent{Event: &aiv1.ServerEvent_StreamError{StreamError: &aiv1.StreamError{}}},
			"error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line := translateToSSE(tt.event)
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(line), &parsed))
			assert.Equal(t, tt.wantType, parsed["type"])
		})
	}
}

func TestTranslateToSSE_TextDeltaContent(t *testing.T) {
	ev := &aiv1.ServerEvent{Event: &aiv1.ServerEvent_TextDelta{TextDelta: &aiv1.TextDelta{Delta: "Hello world"}}}
	line := translateToSSE(ev)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &parsed))
	assert.Equal(t, "text-delta", parsed["type"])
	assert.Equal(t, "Hello world", parsed["delta"])
}

func TestTranslateToSSE_ArtifactStartData(t *testing.T) {
	ev := &aiv1.ServerEvent{Event: &aiv1.ServerEvent_ArtifactStart{ArtifactStart: &aiv1.ArtifactStart{
		ArtifactId: "art1", Type: "code", Title: "main.go",
	}}}
	line := translateToSSE(ev)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(line), &parsed))
	data := parsed["data"].(map[string]any)
	assert.Equal(t, "art1", data["id"])
	assert.Equal(t, "code", data["type"])
	assert.Equal(t, "main.go", data["title"])
}
