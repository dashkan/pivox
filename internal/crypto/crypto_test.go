//go:build dev

package crypto

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoOpEncryptor_RoundTrip(t *testing.T) {
	enc := NoOpEncryptor{}
	plaintext := []byte("sensitive registration token")

	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)

	assert.Equal(t, plaintext, decrypted)
	// NoOp should pass data through unchanged.
	assert.Equal(t, plaintext, ciphertext)
}

func TestNoOpEncryptor_EmptyInput(t *testing.T) {
	enc := NoOpEncryptor{}
	plaintext := []byte{}

	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)

	assert.Equal(t, plaintext, decrypted)
	assert.Empty(t, decrypted)
}

func TestNoOpEncryptor_LargeInput(t *testing.T) {
	enc := NoOpEncryptor{}
	// 1 MiB payload.
	plaintext := bytes.Repeat([]byte("A"), 1<<20)

	ciphertext, err := enc.Encrypt(plaintext)
	require.NoError(t, err)

	decrypted, err := enc.Decrypt(ciphertext)
	require.NoError(t, err)

	assert.Equal(t, plaintext, decrypted)
	assert.Len(t, decrypted, 1<<20)
}

func TestNoOpEncryptor_NilInput(t *testing.T) {
	enc := NoOpEncryptor{}

	ciphertext, err := enc.Encrypt(nil)
	require.NoError(t, err)
	assert.Nil(t, ciphertext)

	decrypted, err := enc.Decrypt(nil)
	require.NoError(t, err)
	assert.Nil(t, decrypted)
}

func TestNewEncryptor_Dev(t *testing.T) {
	enc, err := NewEncryptor()
	require.NoError(t, err)
	require.NotNil(t, enc)

	_, ok := enc.(NoOpEncryptor)
	assert.True(t, ok, "NewEncryptor in dev build should return NoOpEncryptor")
}

func TestEncryptorInterface(t *testing.T) {
	// Compile-time check: NoOpEncryptor must satisfy Encryptor.
	var _ Encryptor = NoOpEncryptor{}
	var _ Encryptor = (*NoOpEncryptor)(nil)
}
