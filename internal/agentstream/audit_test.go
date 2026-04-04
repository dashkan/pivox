package agentstream

import (
	"encoding/json"
	"strings"
	"testing"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalAndRedact_NoSecrets(t *testing.T) {
	msg := &agentv1.ConfigUpdate{
		Endpoints: []*agentv1.EndpointConfig{
			{
				Name: "my-endpoint",
				Configuration: &agentv1.EndpointConfig_Filesystem{
					Filesystem: &agentv1.FileSystemEndpointConfig{
						Path: "/mnt/data",
					},
				},
			},
		},
		DeniedPatterns: []string{"*.tmp"},
	}

	data, err := MarshalAndRedact(msg)
	require.NoError(t, err)

	// Should be valid JSON.
	assert.True(t, json.Valid(data), "output should be valid JSON")

	// Should contain the endpoint name.
	assert.Contains(t, string(data), "my-endpoint")

	// Should contain the filesystem path.
	assert.Contains(t, string(data), "/mnt/data")

	// Should not contain any redaction markers (no secrets present).
	assert.NotContains(t, string(data), `"***"`)
}

func TestMarshalAndRedact_WithSecrets(t *testing.T) {
	msg := &agentv1.ConfigUpdate{
		Endpoints: []*agentv1.EndpointConfig{
			{
				Name: "s3-endpoint",
				Configuration: &agentv1.EndpointConfig_S3{
					S3: &agentv1.S3EndpointConfig{
						EndpointUri:     "https://s3.us-east-1.amazonaws.com",
						Bucket:          "my-bucket",
						Region:          "us-east-1",
						AccessKeyId:     "AKIAIOSFODNN7EXAMPLE",
						SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					},
				},
			},
		},
	}

	data, err := MarshalAndRedact(msg)
	require.NoError(t, err)

	// Should be valid JSON.
	assert.True(t, json.Valid(data), "output should be valid JSON")

	s := string(data)

	// The secret access key value must be redacted.
	assert.NotContains(t, s, "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"secret access key should not appear in redacted output")

	// The field name should still be present, but its value should be "***".
	assert.Contains(t, s, `"secretAccessKey"`)
	assert.Contains(t, s, `"***"`)

	// Non-secret fields should survive intact.
	assert.Contains(t, s, "AKIAIOSFODNN7EXAMPLE")
	assert.Contains(t, s, "my-bucket")
	assert.Contains(t, s, "us-east-1")
}

func TestMarshalAndRedact_MultipleEndpoints(t *testing.T) {
	msg := &agentv1.ConfigUpdate{
		Endpoints: []*agentv1.EndpointConfig{
			{
				Name: "ep-1",
				Configuration: &agentv1.EndpointConfig_S3{
					S3: &agentv1.S3EndpointConfig{
						Bucket:          "bucket-1",
						SecretAccessKey: "secret-one",
					},
				},
			},
			{
				Name: "ep-2",
				Configuration: &agentv1.EndpointConfig_S3{
					S3: &agentv1.S3EndpointConfig{
						Bucket:          "bucket-2",
						SecretAccessKey: "secret-two",
					},
				},
			},
		},
	}

	data, err := MarshalAndRedact(msg)
	require.NoError(t, err)

	s := string(data)

	// Both secrets must be redacted.
	assert.NotContains(t, s, "secret-one")
	assert.NotContains(t, s, "secret-two")

	// Both occurrences should be replaced with "***".
	assert.Equal(t, 2, strings.Count(s, `"***"`),
		"expected 2 redacted secrets for 2 endpoints")
}

func TestRedactSecretKeyPattern(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple secret",
			input: `{"secretAccessKey":"real-secret"}`,
			want:  `{"secretAccessKey":"***"}`,
		},
		{
			name:  "with spaces around colon",
			input: `{"secretAccessKey" : "real-secret"}`,
			want:  `{"secretAccessKey" : "***"}`,
		},
		{
			name:  "empty value",
			input: `{"secretAccessKey":""}`,
			want:  `{"secretAccessKey":"***"}`,
		},
		{
			name:  "no match leaves input unchanged",
			input: `{"accessKeyId":"AKIA1234"}`,
			want:  `{"accessKeyId":"AKIA1234"}`,
		},
		{
			name:  "multiple occurrences",
			input: `[{"secretAccessKey":"aaa"},{"secretAccessKey":"bbb"}]`,
			want:  `[{"secretAccessKey":"***"},{"secretAccessKey":"***"}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactSecretKeyPattern.ReplaceAll([]byte(tt.input), []byte(`$1"***"`))
			assert.Equal(t, tt.want, string(got))
		})
	}
}
