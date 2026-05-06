package cryptotest

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptor_RoundTrip(t *testing.T) {
	e := New()
	plaintext := []byte("sensitive registration token")

	ciphertext, err := e.Encrypt(plaintext)
	require.NoError(t, err)

	got, err := e.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, plaintext, got)
}

func TestEncryptor_PlaintextDistinguishableFromCiphertext(t *testing.T) {
	e := New()
	plaintext := []byte("hello")

	ciphertext, err := e.Encrypt(plaintext)
	require.NoError(t, err)
	assert.NotEqual(t, plaintext, ciphertext, "ciphertext must differ from plaintext so accidental plaintext storage is detectable")
}

func TestEncryptor_DecryptRejectsRawPlaintext(t *testing.T) {
	e := New()

	// Pretend a handler stored plaintext where ciphertext belongs.
	_, err := e.Decrypt([]byte("not-encrypted"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotEncrypted))
}

func TestEncryptor_RecordsCalls(t *testing.T) {
	e := New()

	_, err := e.Encrypt([]byte("first"))
	require.NoError(t, err)
	_, err = e.Encrypt([]byte("second"))
	require.NoError(t, err)

	require.Len(t, e.EncryptedPlaintexts, 2)
	assert.Equal(t, []byte("first"), e.EncryptedPlaintexts[0])
	assert.Equal(t, []byte("second"), e.EncryptedPlaintexts[1])
}

func TestEncryptor_RecordedCopiesAreIndependent(t *testing.T) {
	// Caller mutating the input slice after Encrypt must not corrupt
	// the recorded plaintext — the buffer is copied on entry.
	e := New()
	buf := []byte("mutable")
	_, err := e.Encrypt(buf)
	require.NoError(t, err)

	buf[0] = 'X'
	require.Len(t, e.EncryptedPlaintexts, 1)
	assert.Equal(t, []byte("mutable"), e.EncryptedPlaintexts[0])
}

func TestEncryptor_DecryptedSliceIsIndependent(t *testing.T) {
	// Mutating the decrypted slice must not corrupt the ciphertext
	// the caller still holds.
	e := New()
	ciphertext, err := e.Encrypt([]byte("hello"))
	require.NoError(t, err)

	got, err := e.Decrypt(ciphertext)
	require.NoError(t, err)
	got[0] = 'X'

	again, err := e.Decrypt(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), again, "ciphertext re-decryption must be unaffected by caller mutation")
}

func TestEncryptor_ConcurrentEncrypts(t *testing.T) {
	// Encryptor is documented as safe for concurrent use; the
	// goroutine-collision shape catches a missing lock under -race.
	e := New()

	const goroutines = 16
	const perGoroutine = 32

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			payload := bytes.Repeat([]byte{byte(id)}, perGoroutine)
			for range perGoroutine {
				if _, err := e.Encrypt(payload); err != nil {
					t.Errorf("goroutine %d: %v", id, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	assert.Len(t, e.EncryptedPlaintexts, goroutines*perGoroutine)
}
