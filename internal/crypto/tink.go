package crypto

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/tink-crypto/tink-go-gcpkms/v2/integration/gcpkms"
	"github.com/tink-crypto/tink-go/v2/aead"
	"github.com/tink-crypto/tink-go/v2/insecurecleartextkeyset"
	"github.com/tink-crypto/tink-go/v2/keyset"
)

// gcpKMSPrefix is Tink's scheme for Google Cloud KMS key URIs.
const gcpKMSPrefix = "gcp-kms://"

// The Encryptor backends are Google Tink AEADs returned directly. Tink's
// tink.AEAD has the same method set as Encryptor, so it satisfies the
// interface with no wrapper type — Tink stays contained to this package.

// NewLocalEncryptor builds an Encryptor from a serialized cleartext Tink
// keyset (binary form). For dev / on-prem without a cloud KMS. The keyset
// bytes ARE the master key — source them from a restricted env var or file,
// never a source literal. "Cleartext" is Tink's own term for that trust
// assumption.
func NewLocalEncryptor(serialized []byte) (Encryptor, error) {
	if len(serialized) == 0 {
		return nil, errors.New("crypto: empty keyset")
	}
	handle, err := insecurecleartextkeyset.Read(keyset.NewBinaryReader(bytes.NewReader(serialized)))
	if err != nil {
		return nil, fmt.Errorf("crypto: read keyset: %w", err)
	}
	a, err := aead.New(handle)
	if err != nil {
		return nil, fmt.Errorf("crypto: build aead: %w", err)
	}
	return a, nil
}

// NewGCPKMSEncryptor builds an Encryptor whose KEK is a Google Cloud KMS
// key (Tink envelope encryption: a fresh AES-256-GCM DEK per value, wrapped
// by the KMS key). keyName is the KMS resource name; the "gcp-kms://" scheme
// is added if absent. Requires ambient GCP credentials (ADC).
func NewGCPKMSEncryptor(ctx context.Context, keyName string) (Encryptor, error) {
	keyURI := keyName
	if !strings.HasPrefix(keyURI, gcpKMSPrefix) {
		keyURI = gcpKMSPrefix + keyName
	}
	client, err := gcpkms.NewClient(ctx, keyURI)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcp kms client: %w", err)
	}
	kek, err := client.GetAEAD(keyURI)
	if err != nil {
		return nil, fmt.Errorf("crypto: gcp kms aead: %w", err)
	}
	return aead.NewKMSEnvelopeAEAD2(aead.AES256GCMKeyTemplate(), kek), nil
}

// GenerateCleartextKeyset creates a fresh AES-256-GCM Tink keyset,
// serialized as cleartext binary. Used to bootstrap a dev / on-prem
// encryption key and by tests. Store the output somewhere restricted;
// anyone holding it can decrypt everything encrypted under it.
func GenerateCleartextKeyset() ([]byte, error) {
	handle, err := keyset.NewHandle(aead.AES256GCMKeyTemplate())
	if err != nil {
		return nil, fmt.Errorf("crypto: new keyset: %w", err)
	}
	var buf bytes.Buffer
	if err := insecurecleartextkeyset.Write(handle, keyset.NewBinaryWriter(&buf)); err != nil {
		return nil, fmt.Errorf("crypto: write keyset: %w", err)
	}
	return buf.Bytes(), nil
}
