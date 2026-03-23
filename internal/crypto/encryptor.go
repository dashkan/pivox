package crypto

// Encryptor provides symmetric encryption for sensitive data at rest
// (registration tokens, storage endpoint credentials, etc.).
type Encryptor interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
}
