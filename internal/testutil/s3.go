package testutil

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// One rustfs container per test process, shared across every test
// that calls SetupTestS3. Each test gets its own bucket for
// isolation. Boots lazily on first SetupTestS3 call; never
// explicitly terminated — testcontainers' Ryuk reaper cleans up
// when the test process exits.
//
// Why shared: the container takes ~10s to boot and pass its
// healthcheck. Spinning a fresh one per test multiplied that by
// the number of S3 tests; the package was 75s of mostly-idle
// container plumbing. One shared container collapses it to a
// single 10s boot + per-test bucket creation (~ms each).
var (
	sharedS3InitOnce sync.Once
	sharedS3Client   *minio.Client
	sharedS3Endpoint string
	sharedS3InitErr  error
)

// SetupTestS3 returns a minio client + endpoint URL pointing at
// the package-shared rustfs container, plus a freshly-created
// per-test bucket name and a cleanup that drops the bucket.
//
// The bucket name is derived from t.Name() so concurrent subtests
// don't collide; cleanup drains any objects then deletes the
// bucket. Caller responsibilities collapse to "use the returned
// bucket name" — no MakeBucket boilerplate at the call site.
func SetupTestS3(t *testing.T) (client *minio.Client, endpoint, bucketName string, cleanup func()) {
	t.Helper()
	sharedS3InitOnce.Do(initSharedS3)
	if sharedS3InitErr != nil {
		t.Fatalf("failed to start shared RustFS container: %v", sharedS3InitErr)
	}

	bucketName = bucketFromTestName(t)
	ctx := context.Background()
	if err := sharedS3Client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("failed to create test bucket %q: %v", bucketName, err)
	}

	cleanup = func() {
		// Drain any objects so RemoveBucket succeeds, then drop
		// the bucket. Errors here are best-effort — the next test
		// run uses a different bucket name, and Ryuk will reap
		// the whole container at process exit anyway.
		ctx := context.Background()
		objCh := sharedS3Client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{Recursive: true})
		for obj := range objCh {
			if obj.Err != nil {
				continue
			}
			_ = sharedS3Client.RemoveObject(ctx, bucketName, obj.Key, minio.RemoveObjectOptions{})
		}
		_ = sharedS3Client.RemoveBucket(ctx, bucketName)
	}

	return sharedS3Client, sharedS3Endpoint, bucketName, cleanup
}

// initSharedS3 starts the one rustfs container the package
// shares. Called exactly once via sync.Once.
func initSharedS3() {
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
		sharedS3InitErr = fmt.Errorf("start rustfs container: %w", err)
		return
	}

	host, err := container.Host(ctx)
	if err != nil {
		sharedS3InitErr = fmt.Errorf("container host: %w", err)
		return
	}
	mappedPort, err := container.MappedPort(ctx, "9000")
	if err != nil {
		sharedS3InitErr = fmt.Errorf("container port: %w", err)
		return
	}
	sharedS3Endpoint = fmt.Sprintf("%s:%s", host, mappedPort.Port())

	sharedS3Client, err = minio.New(sharedS3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4("testaccess", "testsecret", ""),
		Secure: false,
	})
	if err != nil {
		sharedS3InitErr = fmt.Errorf("minio client: %w", err)
		return
	}
}

// bucketFromTestName derives a valid S3 bucket name from t.Name().
// Buckets are lowercase, no slashes/underscores, max 63 chars.
func bucketFromTestName(t *testing.T) string {
	name := "test-" + t.Name()
	name = strings.ToLower(name)
	name = strings.NewReplacer("/", "-", "_", "-").Replace(name)
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}
