package testutil

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// SetupTestS3 starts a RustFS (S3-compatible) container and returns a minio
// client + endpoint URL. Call cleanup() when done.
func SetupTestS3(t *testing.T) (client *minio.Client, endpoint string, cleanup func()) {
	t.Helper()
	ctx := context.Background()

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Image:        "rustfs/rustfs:latest",
			ExposedPorts: []string{"9000/tcp"},
			Env: map[string]string{
				"RUSTFS_ROOT_USER":     "testaccess",
				"RUSTFS_ROOT_PASSWORD": "testsecret",
			},
			Cmd: []string{"server", "/data"},
			WaitingFor: wait.ForHTTP("/minio/health/live").
				WithPort("9000").
				WithStatusCodeMatcher(func(status int) bool {
					return status == http.StatusOK || status == http.StatusForbidden
				}).
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	})
	if err != nil {
		t.Fatalf("failed to start RustFS container: %v", err)
	}

	host, err := container.Host(ctx)
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to get container host: %v", err)
	}

	mappedPort, err := container.MappedPort(ctx, "9000")
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to get mapped port: %v", err)
	}

	endpoint = fmt.Sprintf("%s:%s", host, mappedPort.Port())

	client, err = minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4("testaccess", "testsecret", ""),
		Secure: false,
	})
	if err != nil {
		_ = container.Terminate(ctx)
		t.Fatalf("failed to create minio client: %v", err)
	}

	cleanup = func() {
		_ = container.Terminate(context.Background())
	}
	return client, endpoint, cleanup
}
