package testutil

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Test rustfs lives in docker-compose.test.yml. `make test-up`
// starts it; tests connect to a fixed port. Per-test isolation is
// via per-test buckets.
//
// Why docker-compose instead of testcontainers-go: testcontainers
// starts a fresh container per package by default, which sums to
// ~10s × N packages of pure container plumbing. The compose stack
// is up-once-and-reused — every test connects to a running
// service in milliseconds.

const (
	composeS3Endpoint = "localhost:59000"
	composeS3User     = "testaccess"
	composeS3Password = "testsecret"
)

var (
	s3InitOnce sync.Once
	s3Client   *minio.Client
	s3InitErr  error
)

// SetupTestS3 returns a minio client + endpoint + freshly-created
// per-test bucket name and a cleanup that drops the bucket. The
// bucket name is derived from t.Name() so concurrent subtests
// don't collide.
func SetupTestS3(t *testing.T) (client *minio.Client, endpoint, bucketName string, cleanup func()) {
	t.Helper()
	s3InitOnce.Do(initSharedS3Client)
	if s3InitErr != nil {
		t.Fatalf("connect to docker-compose rustfs: %v\n\nIs `make test-up` running?", s3InitErr)
	}

	bucketName = bucketFromTestName(t)
	ctx := context.Background()
	if err := s3Client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("failed to create test bucket %q: %v", bucketName, err)
	}

	cleanup = func() {
		ctx := context.Background()
		objCh := s3Client.ListObjects(ctx, bucketName, minio.ListObjectsOptions{Recursive: true})
		for obj := range objCh {
			if obj.Err != nil {
				continue
			}
			_ = s3Client.RemoveObject(ctx, bucketName, obj.Key, minio.RemoveObjectOptions{})
		}
		_ = s3Client.RemoveBucket(ctx, bucketName)
	}

	return s3Client, composeS3Endpoint, bucketName, cleanup
}

// initSharedS3Client connects to the docker-compose rustfs once
// per test process. Any health check (is rustfs actually up?) is
// the caller's responsibility — typically `make test-up` waits
// for the compose healthcheck to pass before returning.
func initSharedS3Client() {
	c, err := minio.New(composeS3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(composeS3User, composeS3Password, ""),
		Secure: false,
	})
	if err != nil {
		s3InitErr = fmt.Errorf("minio client: %w", err)
		return
	}
	s3Client = c
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
