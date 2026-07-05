package storage

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBackend(t *testing.T) {
	t.Parallel()
	s3 := validS3Config()
	tests := []struct {
		name    string
		cfg     BackendConfig
		wantErr string
	}{
		{name: "s3 compatible", cfg: BackendConfig{Provider: ProviderAWSS3Compatible, S3: &s3}},
		{name: "missing provider", cfg: BackendConfig{}, wantErr: "Provider is required"},
		{name: "s3 without config", cfg: BackendConfig{Provider: ProviderAWSS3Compatible}, wantErr: "requires an S3 config"},
		{name: "unimplemented provider", cfg: BackendConfig{Provider: ProviderAzureBlobStorage}, wantErr: "unsupported provider"},
		{name: "unknown provider", cfg: BackendConfig{Provider: "NONSENSE"}, wantErr: "unsupported provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := NewBackend(tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Nil(t, b)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, b)
			// Smoke: the dispatched backend actually signs.
			got, err := b.SignUpload(context.Background(), SignUploadRequest{
				Key: "orgs/a/spaces/b/assets/c/v1",
			})
			require.NoError(t, err)
			assert.Equal(t, ProviderAWSS3Compatible, got.Provider)
		})
	}
}
