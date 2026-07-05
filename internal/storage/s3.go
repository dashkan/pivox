package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// defaultUploadExpiry is used when SignUploadRequest.Expiry is zero.
const defaultUploadExpiry = 15 * time.Minute

// maxUploadExpiry is the ceiling AWS SigV4 presigned URLs allow (7 days).
// Requests above it are clamped, not rejected.
const maxUploadExpiry = 7 * 24 * time.Hour

// S3Config configures an AWS_S3_COMPATIBLE backend. Credentials are
// static access keys; other auth methods (STS, instance profile) are
// added behind this same constructor as needed.
type S3Config struct {
	// EndpointURI is the S3 endpoint, e.g. "https://s3.us-east-1.amazonaws.com"
	// or "http://minio.internal:9000". Required.
	EndpointURI string
	// Bucket is the bucket objects are stored in. Required.
	Bucket string
	// Region is the backend region. Required for offline presigning —
	// without it the client performs a GetBucketLocation round trip.
	Region string
	// AccessKeyID / SecretAccessKey are static credentials. Required.
	AccessKeyID     string
	SecretAccessKey string
}

// s3Backend is an AWS_S3_COMPATIBLE Backend backed by a minio client.
type s3Backend struct {
	client *minio.Client
	bucket string
}

var _ Backend = (*s3Backend)(nil)

// newS3Backend constructs an s3Backend. It builds the minio client but
// does NOT probe the bucket — minting an upload URL must not depend on a
// live round trip to the backend (and keeps construction offline-testable).
func newS3Backend(cfg S3Config) (*s3Backend, error) {
	if cfg.EndpointURI == "" {
		return nil, errors.New("storage: S3Config.EndpointURI is required")
	}
	if cfg.Bucket == "" {
		return nil, errors.New("storage: S3Config.Bucket is required")
	}
	if cfg.Region == "" {
		// Required: with an explicit region minio signs presigned URLs
		// locally; without it every SignUpload issues a GetBucketLocation
		// round trip, defeating the offline-minting invariant.
		return nil, errors.New("storage: S3Config.Region is required")
	}

	u, err := url.Parse(cfg.EndpointURI)
	if err != nil {
		return nil, fmt.Errorf("storage: parse endpoint URI: %w", err)
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: u.Scheme == "https",
		Region: cfg.Region,
	}

	client, err := minio.New(u.Host, opts)
	if err != nil {
		return nil, fmt.Errorf("storage: create s3 client: %w", err)
	}
	return &s3Backend{client: client, bucket: cfg.Bucket}, nil
}

func (b *s3Backend) SignUpload(ctx context.Context, req SignUploadRequest) (*UploadInstructions, error) {
	if err := validateObjectKey(req.Key); err != nil {
		return nil, err
	}

	expiry := req.Expiry
	switch {
	case expiry <= 0:
		expiry = defaultUploadExpiry
	case expiry > maxUploadExpiry:
		expiry = maxUploadExpiry
	}

	// PresignedPutObject signs the URL locally (no round trip) because the
	// client has an explicit region. The secret key never leaves this
	// process — only the SigV4 signature appears in the URL.
	u, err := b.client.PresignedPutObject(ctx, b.bucket, req.Key, expiry)
	if err != nil {
		return nil, fmt.Errorf("storage: presign put %q: %w", req.Key, err)
	}

	// Content-Type is advertised to the client as an upload header but is
	// NOT bound into the SigV4 signature — the client may send any value.
	// Bind it via PresignHeader if content-type integrity ever matters
	// (e.g. objects later served using their stored type).
	var headers map[string]string
	if req.ContentType != "" {
		headers = map[string]string{"Content-Type": req.ContentType}
	}

	return &UploadInstructions{
		Provider: ProviderAWSS3Compatible,
		URL:      u.String(),
		Headers:  headers,
	}, nil
}

// validateObjectKey is a defense-in-depth backstop on the object key.
// The service layer is the authority for tenant-prefix scoping; this only
// rejects structurally dangerous keys before they land in a signed URL
// (leading slash, ".." segment, control chars). It also guards the future
// FILE_SYSTEM backend, which maps keys onto a mounted path.
func validateObjectKey(key string) error {
	if key == "" {
		return errors.New("storage: SignUploadRequest.Key is required")
	}
	if strings.HasPrefix(key, "/") {
		return errors.New("storage: object key must not start with '/'")
	}
	if slices.Contains(strings.Split(key, "/"), "..") {
		return errors.New("storage: object key must not contain a '..' segment")
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return errors.New("storage: object key must not contain control characters")
		}
	}
	return nil
}

// LogValue redacts credentials so an S3Config logged via slog never leaks
// the access key or secret.
func (c S3Config) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("endpoint_uri", c.EndpointURI),
		slog.String("bucket", c.Bucket),
		slog.String("region", c.Region),
		slog.String("access_key_id", "REDACTED"),
		slog.String("secret_access_key", "REDACTED"),
	)
}
