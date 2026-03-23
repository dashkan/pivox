//go:build dev

package crypto

// NoOpEncryptor passes data through unchanged. Used for local development
// where encryption adds friction with zero benefit.
type NoOpEncryptor struct{}

func (NoOpEncryptor) Encrypt(plaintext []byte) ([]byte, error)  { return plaintext, nil }
func (NoOpEncryptor) Decrypt(ciphertext []byte) ([]byte, error) { return ciphertext, nil }

// NewEncryptor returns a NoOpEncryptor for dev builds.
func NewEncryptor() (Encryptor, error) {
	return NoOpEncryptor{}, nil
}
