package crypto

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
)

// Encryptor provides authenticated symmetric encryption for sensitive
// data at rest (SSO client secrets, storage endpoint credentials, etc.).
//
// aad is Additional Authenticated Data: authenticated but NOT encrypted,
// and it MUST match on Decrypt or decryption fails. Callers bind it to the
// owning resource (e.g. org_id) so a ciphertext can't be moved to a
// different row/tenant. Pass nil when there is no stable binding context.
// Bind aad to the owning resource; encrypt and decrypt must always agree
// on the value.
type Encryptor interface {
	Encrypt(plaintext, aad []byte) ([]byte, error)
	Decrypt(ciphertext, aad []byte) ([]byte, error)
}

// Provider selects the encryption backend — specifically the key-encryption
// key (KEK) source. All backends are Tink-backed; only the KEK differs.
type Provider string

const (
	// ProviderLocal uses a local cleartext Tink keyset (dev / on-prem, no
	// cloud KMS). The keyset material is the master key.
	ProviderLocal Provider = "local"
	// ProviderGCP wraps a per-value DEK with a Google Cloud KMS key (Tink
	// envelope encryption).
	ProviderGCP Provider = "gcp"
	// ProviderAWS / ProviderAzure are reserved; add the backend + KEK
	// wiring when needed.
)

// EncryptorConfig configures NewEncryptor. It is populated from the
// PIVOX_ENCRYPTION_* flags in the cmd layer — this package reads no
// environment itself.
type EncryptorConfig struct {
	// Provider selects the backend. Empty defaults to ProviderLocal.
	Provider Provider
	// LocalKeysetB64 is the base64-encoded cleartext Tink keyset used when
	// Provider is local. It IS the master key — treat as a secret.
	LocalKeysetB64 string
	// GCPKMSKeyName is the Cloud KMS key resource name used when Provider is
	// gcp, e.g. "projects/p/locations/l/keyRings/r/cryptoKeys/k". The
	// "gcp-kms://" scheme is added if absent.
	GCPKMSKeyName string
}

// NewEncryptor builds the Encryptor for cfg.Provider.
func NewEncryptor(cfg EncryptorConfig) (Encryptor, error) {
	switch cfg.Provider {
	case ProviderLocal, "":
		if cfg.LocalKeysetB64 == "" {
			return nil, errors.New("crypto: local provider requires PIVOX_ENCRYPTION_LOCAL_KEYSET")
		}
		ks, err := base64.StdEncoding.DecodeString(cfg.LocalKeysetB64)
		if err != nil {
			return nil, fmt.Errorf("crypto: decode local keyset: %w", err)
		}
		return NewLocalEncryptor(ks)
	case ProviderGCP:
		if cfg.GCPKMSKeyName == "" {
			return nil, errors.New("crypto: gcp provider requires PIVOX_ENCRYPTION_GCP_KMS_KEY_NAME")
		}
		return NewGCPKMSEncryptor(context.Background(), cfg.GCPKMSKeyName)
	default:
		return nil, fmt.Errorf("crypto: unknown encryption provider %q", cfg.Provider)
	}
}
