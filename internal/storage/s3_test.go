package storage

import (
	"bytes"
	"context"
	"log/slog"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validS3Config returns a complete, valid config for the offline tests
// (region set so presigning never issues a GetBucketLocation round trip).
func validS3Config() S3Config {
	return S3Config{
		EndpointURI:     "https://s3.us-east-1.amazonaws.com",
		Bucket:          "pivox-assets",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAEXAMPLE",
		SecretAccessKey: "secretexamplekey",
	}
}

func TestNewS3Backend_Validation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cfg     S3Config
		wantErr string
	}{
		{
			name:    "missing endpoint",
			cfg:     S3Config{Bucket: "b", Region: "us-east-1"},
			wantErr: "EndpointURI is required",
		},
		{
			name:    "missing bucket",
			cfg:     S3Config{EndpointURI: "https://s3.us-east-1.amazonaws.com", Region: "us-east-1"},
			wantErr: "Bucket is required",
		},
		{
			name:    "missing region",
			cfg:     S3Config{EndpointURI: "https://s3.us-east-1.amazonaws.com", Bucket: "b"},
			wantErr: "Region is required",
		},
		{
			name: "valid",
			cfg:  validS3Config(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := newS3Backend(tt.cfg)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, b)
		})
	}
}

// TestS3Backend_SignUpload asserts SignUpload mints a well-formed SigV4
// presigned PUT URL fully offline (region is set, so minio signs locally
// with no GetBucketLocation round trip).
func TestS3Backend_SignUpload(t *testing.T) {
	t.Parallel()
	b, err := newS3Backend(validS3Config())
	require.NoError(t, err)

	const key = "orgs/acme/spaces/dev/assets/abc123/v1"
	got, err := b.SignUpload(context.Background(), SignUploadRequest{
		Key:         key,
		ContentType: "video/mp4",
		Expiry:      15 * time.Minute,
	})
	require.NoError(t, err)
	require.NotNil(t, got)

	assert.Equal(t, ProviderAWSS3Compatible, got.Provider)
	assert.Empty(t, got.Parts, "single-PUT upload has no parts")
	assert.Equal(t, "video/mp4", got.Headers["Content-Type"])

	u, err := url.Parse(got.URL)
	require.NoError(t, err)
	assert.Equal(t, "https", u.Scheme)
	assert.Contains(t, u.Host, "amazonaws.com")
	// Bucket + key appear in the URL (host- or path-style, so assert on
	// the whole URL rather than a specific segment).
	assert.Contains(t, got.URL, "pivox-assets")
	assert.Contains(t, u.Path, "abc123")

	q := u.Query()
	assert.Equal(t, "AWS4-HMAC-SHA256", q.Get("X-Amz-Algorithm"))
	assert.NotEmpty(t, q.Get("X-Amz-Signature"))
	assert.NotEmpty(t, q.Get("X-Amz-Credential"))
	assert.Equal(t, "900", q.Get("X-Amz-Expires"), "15m expiry in seconds")
}

func TestS3Backend_SignUpload_InvalidKey(t *testing.T) {
	t.Parallel()
	b, err := newS3Backend(validS3Config())
	require.NoError(t, err)

	tests := []struct {
		name    string
		key     string
		wantErr string
	}{
		{name: "empty", key: "", wantErr: "Key is required"},
		{name: "leading slash", key: "/orgs/acme/x", wantErr: "must not start with '/'"},
		{name: "dotdot segment", key: "orgs/acme/../evil/x", wantErr: "must not contain a '..' segment"},
		{name: "control char", key: "orgs/acme/x\x00y", wantErr: "control characters"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := b.SignUpload(context.Background(), SignUploadRequest{Key: tt.key})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestS3Backend_SignUpload_ClampsExpiry verifies zero/negative use the
// default and an over-long expiry clamps to the SigV4 ceiling, while the
// exact ceiling passes through unchanged.
func TestS3Backend_SignUpload_ClampsExpiry(t *testing.T) {
	t.Parallel()
	b, err := newS3Backend(validS3Config())
	require.NoError(t, err)

	tests := []struct {
		name        string
		expiry      time.Duration
		wantExpires string
	}{
		{name: "zero uses default 15m", expiry: 0, wantExpires: "900"},
		{name: "negative uses default 15m", expiry: -1, wantExpires: "900"},
		{name: "at 7d boundary not clamped", expiry: 7 * 24 * time.Hour, wantExpires: "604800"},
		{name: "over 7d clamps", expiry: 30 * 24 * time.Hour, wantExpires: "604800"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := b.SignUpload(context.Background(), SignUploadRequest{
				Key:    "orgs/acme/spaces/dev/assets/abc123/v1",
				Expiry: tt.expiry,
			})
			require.NoError(t, err)
			u, err := url.Parse(got.URL)
			require.NoError(t, err)
			assert.Equal(t, tt.wantExpires, u.Query().Get("X-Amz-Expires"))
		})
	}
}

// TestS3Config_LogValue_RedactsSecrets ensures logging an S3Config never
// emits the access key or secret.
func TestS3Config_LogValue_RedactsSecrets(t *testing.T) {
	t.Parallel()
	cfg := validS3Config()
	cfg.SecretAccessKey = "supersecretvalue"

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("s3 config", "cfg", cfg)
	out := buf.String()

	assert.NotContains(t, out, "supersecretvalue")
	assert.NotContains(t, out, cfg.AccessKeyID)
	assert.Contains(t, out, "REDACTED")
	assert.Contains(t, out, "pivox-assets", "non-secret fields still logged")
}
