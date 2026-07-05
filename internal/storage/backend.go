// Package storage provides the pluggable storage-backend abstraction
// shared by the Cloud Controller (minting direct-upload instructions)
// and, over time, the Storage Agent (serving/reading objects). Each
// supported backend — S3-compatible, Azure Blob, Google Cloud Storage,
// filesystem — is one implementation of Backend.
//
// The abstraction is deliberately small. Today Backend exposes only
// SignUpload; serve/list/delete grow onto it as their consumers migrate
// behind the interface, not before.
package storage

import (
	"context"
	"time"
)

// ProviderType identifies a storage backend implementation. It doubles
// as the discriminator the UI switches on to pick the provider-optimized
// upload path (each provider maps 1:1 to one native upload mechanism).
type ProviderType string

const (
	// ProviderAWSS3Compatible is any backend speaking the AWS S3 API —
	// AWS S3, RustFS, MinIO, or GCS in interoperability mode. Upload
	// mechanism: presigned PUT (+ multipart for large objects).
	ProviderAWSS3Compatible ProviderType = "AWS_S3_COMPATIBLE"
	// ProviderAzureBlobStorage is native Azure Blob storage. Upload
	// mechanism: block blob + SAS.
	ProviderAzureBlobStorage ProviderType = "AZURE_BLOB_STORAGE"
	// ProviderGoogleCloudStorage is native Google Cloud Storage. Upload
	// mechanism: resumable upload.
	ProviderGoogleCloudStorage ProviderType = "GOOGLE_CLOUD_STORAGE"
	// ProviderFileSystem is an agent-mounted filesystem. Upload
	// mechanism: authenticated HTTP upload to the agent (no presigning —
	// only the agent can reach the mount).
	ProviderFileSystem ProviderType = "FILE_SYSTEM"
)

// SignUploadRequest is the input to Backend.SignUpload.
//
// Key MUST be constructed server-side (from the asset's identity/path),
// never taken from untrusted client input — it lands in the signed URL.
type SignUploadRequest struct {
	// Key is the object key within the backend (e.g.
	// "orgs/{org}/spaces/{space}/assets/{asset}/v1"). Server-constructed.
	Key string
	// ContentType is advertised to the client as an upload header.
	// Optional.
	ContentType string
	// Expiry bounds the lifetime of the minted upload URL. Zero means
	// the backend's default; the backend clamps to provider limits.
	Expiry time.Duration
}

// UploadPart is one part of a multipart upload. Empty set means a
// single-request upload.
type UploadPart struct {
	Number int32
	URL    string
	Offset int64
	Size   int64
}

// UploadInstructions tells a client how to upload an object directly to
// the backend, bypassing the control plane. Provider names the
// mechanism the client uses; the remaining fields parameterize it.
//
// This is a domain type — the service layer converts it to the
// assets.v1 UploadInfo proto so this package stays proto-free.
type UploadInstructions struct {
	Provider ProviderType
	URL      string
	Headers  map[string]string
	Parts    []UploadPart
}

// Backend is a storage provider capable of minting direct-upload
// instructions. Small by design — one method today.
type Backend interface {
	// SignUpload mints instructions for a client to upload an object
	// directly to this backend.
	SignUpload(ctx context.Context, req SignUploadRequest) (*UploadInstructions, error)
}
