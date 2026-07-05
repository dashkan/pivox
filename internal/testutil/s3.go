package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// Test rustfs lives in docker-compose.test.yml. `make test-up` starts it;
// tests connect to a fixed port. Per-test isolation is via per-test
// buckets — bucketFromTestName generates a name that is unique per
// invocation by appending a random suffix.

const (
	composeS3Endpoint = "localhost:59000"

	// S3TestAccessKey and S3TestSecretKey are the docker-compose rustfs
	// root credentials (docker-compose.test.yml). Exported so storage
	// backend integration tests can construct their own S3 clients.
	S3TestAccessKey = "testaccess"
	S3TestSecretKey = "testsecret"

	// s3MaxBucketName is the S3 ceiling for a bucket label.
	s3MaxBucketName = 63
	// s3SuffixHexLen reserves room for a 4-byte random suffix
	// (8 hex chars + 1 dash separator) so the per-test name never
	// collides after truncation.
	s3SuffixHexLen = 8
)

var (
	s3InitOnce sync.Once
	s3Client   *minio.Client
	s3InitErr  error
)

// SetupTestS3 returns a minio client, the compose endpoint, and a
// freshly-created per-test bucket name. The bucket and its objects
// are torn down via t.Cleanup, so callers don't manage cleanup.
func SetupTestS3(t *testing.T) (client *minio.Client, endpoint, bucketName string) {
	t.Helper()
	s3InitOnce.Do(initSharedS3Client)
	if s3InitErr != nil {
		t.Fatalf("connect to docker-compose rustfs: %v\n\nIs `make test-up` running?", s3InitErr)
	}

	bucketName = bucketFromTestName(t)
	ctx := context.Background()
	if err := s3Client.MakeBucket(ctx, bucketName, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("create test bucket %q: %v", bucketName, err)
	}

	t.Cleanup(func() { teardownBucket(s3Client, bucketName) })

	return s3Client, composeS3Endpoint, bucketName
}

// teardownBucket removes every object in the bucket and then the
// bucket itself. Errors are intentionally swallowed — the test has
// already finished, so a cleanup failure can't affect its result;
// best-effort is the right shape here.
func teardownBucket(c *minio.Client, name string) {
	ctx := context.Background()
	for obj := range c.ListObjects(ctx, name, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			continue
		}
		_ = c.RemoveObject(ctx, name, obj.Key, minio.RemoveObjectOptions{})
	}
	_ = c.RemoveBucket(ctx, name)
}

// initSharedS3Client connects to the docker-compose rustfs once
// per test process. The compose healthcheck guarantees the service
// is up before tests run; this is purely a client construction.
func initSharedS3Client() {
	c, err := minio.New(composeS3Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(S3TestAccessKey, S3TestSecretKey, ""),
		Secure: false,
	})
	if err != nil {
		s3InitErr = fmt.Errorf("minio client: %w", err)
		return
	}
	s3Client = c
}

// bucketFromTestName derives a unique S3-valid bucket name from
// t.Name(). S3 bucket labels are lowercase, max 63 chars, and may
// not contain underscores or slashes. We sanitize the test name,
// truncate to leave room for a random suffix, then append the
// suffix so concurrent subtests with similar names cannot collide
// after truncation.
func bucketFromTestName(t *testing.T) string {
	t.Helper()

	const prefix = "test-"
	maxBase := s3MaxBucketName - len(prefix) - 1 - s3SuffixHexLen // -1 for the dash before the suffix

	base := strings.ToLower(t.Name())
	base = strings.NewReplacer("/", "-", "_", "-").Replace(base)
	if len(base) > maxBase {
		base = base[:maxBase]
	}

	var rnd [s3SuffixHexLen / 2]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		// crypto/rand failures are fatal; tests can't proceed.
		t.Fatalf("generate bucket suffix: %v", err)
	}
	return prefix + base + "-" + hex.EncodeToString(rnd[:])
}
