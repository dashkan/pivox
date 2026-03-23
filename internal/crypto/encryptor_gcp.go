//go:build !dev

package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"os"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
)

// GoogleCloudKMSEncryptor uses Google Cloud KMS for envelope encryption.
//
// Encryption flow:
//  1. Generate a random 256-bit Data Encryption Key (DEK)
//  2. Encrypt plaintext with AES-256-GCM using the DEK
//  3. Wrap (encrypt) the DEK using Cloud KMS
//  4. Store: [wrapped DEK length (4 bytes)] [wrapped DEK] [nonce] [ciphertext]
//
// Decryption flow:
//  1. Parse the stored blob to extract wrapped DEK, nonce, ciphertext
//  2. Unwrap the DEK via Cloud KMS
//  3. Decrypt ciphertext with AES-256-GCM using the DEK
//
// KMS never sees the plaintext data — only the small DEK.
type GoogleCloudKMSEncryptor struct {
	client  *kms.KeyManagementClient
	keyName string // e.g. projects/{project}/locations/{location}/keyRings/{ring}/cryptoKeys/{key}
}

// NewEncryptor creates a GoogleCloudKMSEncryptor for production builds.
// Requires PIVOX_KMS_KEY_NAME environment variable.
func NewEncryptor() (Encryptor, error) {
	keyName := os.Getenv("PIVOX_KMS_KEY_NAME")
	if keyName == "" {
		return nil, fmt.Errorf("PIVOX_KMS_KEY_NAME environment variable is required")
	}

	ctx := context.Background()
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create KMS client: %w", err)
	}

	return &GoogleCloudKMSEncryptor{
		client:  client,
		keyName: keyName,
	}, nil
}

func (e *GoogleCloudKMSEncryptor) Encrypt(plaintext []byte) ([]byte, error) {
	// Generate random DEK.
	dek := make([]byte, 32) // AES-256
	if _, err := io.ReadFull(rand.Reader, dek); err != nil {
		return nil, fmt.Errorf("generate DEK: %w", err)
	}

	// Encrypt plaintext with DEK using AES-256-GCM.
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Wrap DEK with KMS.
	ctx := context.Background()
	wrapResp, err := e.client.Encrypt(ctx, &kmspb.EncryptRequest{
		Name:      e.keyName,
		Plaintext: dek,
	})
	if err != nil {
		return nil, fmt.Errorf("KMS encrypt DEK: %w", err)
	}
	wrappedDEK := wrapResp.Ciphertext

	// Encode: [wrappedDEK length (4 bytes)][wrappedDEK][nonce][ciphertext]
	buf := make([]byte, 4+len(wrappedDEK)+len(nonce)+len(ciphertext))
	binary.BigEndian.PutUint32(buf[0:4], uint32(len(wrappedDEK)))
	copy(buf[4:], wrappedDEK)
	copy(buf[4+len(wrappedDEK):], nonce)
	copy(buf[4+len(wrappedDEK)+len(nonce):], ciphertext)

	return buf, nil
}

func (e *GoogleCloudKMSEncryptor) Decrypt(data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("ciphertext too short")
	}

	// Parse envelope.
	wrappedDEKLen := int(binary.BigEndian.Uint32(data[0:4]))
	if len(data) < 4+wrappedDEKLen {
		return nil, fmt.Errorf("ciphertext too short for wrapped DEK")
	}
	wrappedDEK := data[4 : 4+wrappedDEKLen]
	rest := data[4+wrappedDEKLen:]

	// Unwrap DEK with KMS.
	ctx := context.Background()
	unwrapResp, err := e.client.Decrypt(ctx, &kmspb.DecryptRequest{
		Name:       e.keyName,
		Ciphertext: wrappedDEK,
	})
	if err != nil {
		return nil, fmt.Errorf("KMS decrypt DEK: %w", err)
	}
	dek := unwrapResp.Plaintext

	// Decrypt ciphertext with DEK.
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(rest) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short for nonce")
	}
	nonce := rest[:nonceSize]
	ciphertext := rest[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}
