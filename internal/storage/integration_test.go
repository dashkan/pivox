package storage_test

import (
	"bytes"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/storage"
	"github.com/dashkan/pivox/internal/testutil"
)

// TestS3Backend_SignUpload_Integration is a conformance round-trip: it
// mints a presigned PUT via the backend, uploads with a plain HTTP client
// (exactly as a browser would — no S3 SDK on the upload side), and then
// verifies the object landed byte-for-byte. Runs against the compose
// rustfs (`make test-up`).
func TestS3Backend_SignUpload_Integration(t *testing.T) {
	t.Parallel()
	client, endpoint, bucket := testutil.SetupTestS3(t)

	be, err := storage.NewBackend(storage.BackendConfig{
		Provider: storage.ProviderAWSS3Compatible,
		S3: &storage.S3Config{
			EndpointURI:     "http://" + endpoint,
			Bucket:          bucket,
			Region:          "us-east-1",
			AccessKeyID:     testutil.S3TestAccessKey,
			SecretAccessKey: testutil.S3TestSecretKey,
		},
	})
	require.NoError(t, err)

	const key = "orgs/acme/spaces/dev/assets/it123/v1"
	body := []byte("hello broadcast")

	instr, err := be.SignUpload(t.Context(), storage.SignUploadRequest{
		Key:         key,
		ContentType: "text/plain",
		Expiry:      5 * time.Minute,
	})
	require.NoError(t, err)
	require.Equal(t, storage.ProviderAWSS3Compatible, instr.Provider)

	// Upload directly to the presigned URL, like a browser.
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPut, instr.URL, bytes.NewReader(body))
	require.NoError(t, err)
	for k, v := range instr.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "presigned PUT should succeed; body: %s", readAll(resp.Body))

	// The object landed with the exact bytes.
	obj, err := client.GetObject(t.Context(), bucket, key, minio.GetObjectOptions{})
	require.NoError(t, err)
	defer func() { _ = obj.Close() }()
	got, err := io.ReadAll(obj)
	require.NoError(t, err)
	assert.Equal(t, body, got)
}

func readAll(r io.Reader) string {
	b, _ := io.ReadAll(r)
	return string(b)
}
