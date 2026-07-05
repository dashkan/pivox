// Package cryptotest provides a deterministic crypto.Encryptor
// implementation for tests.
//
// Why a dedicated test type instead of NoOp passthrough:
//
//   - Plaintext and ciphertext are distinguishable. A handler that
//     accidentally stores plaintext where ciphertext belongs (forgot
//     to call Encrypt) shows up in any test that asserts on the
//     stored bytes; a passthrough hides that bug.
//   - Round-trip semantics work: Decrypt(Encrypt(x)) == x. Tests
//     that exercise the full encrypt-then-decrypt path don't need
//     paired mock expectations — Encryptor handles it without
//     ceremony.
//   - Optional call recording. Tests that want to assert "we
//     encrypted exactly this plaintext on insert" can read
//     Encryptor.EncryptedPlaintexts; tests that don't care can
//     ignore it.
//
// Construct via cryptotest.New(); the harness uses one of these by
// default. Production code uses crypto.NewEncryptor() (KMS) and is
// not exercised here.
package cryptotest

import (
	"bytes"
	"sync"

	"github.com/dashkan/pivox/internal/crypto"
)

// envelopePrefix is prepended to plaintext on Encrypt and stripped
// on Decrypt. The prefix is intentionally non-empty so a handler
// that stores plaintext where ciphertext belongs produces bytes
// that don't round-trip — a hard signal in tests that assert on
// stored DB rows.
var envelopePrefix = []byte("ENC:")

// Encryptor is a deterministic, recording crypto.Encryptor.
// Safe for concurrent use.
type Encryptor struct {
	mu sync.Mutex
	// EncryptedPlaintexts holds, in call order, copies of every
	// plaintext passed to Encrypt. Tests that want to assert call
	// shape read this slice; tests that don't care ignore it.
	EncryptedPlaintexts [][]byte
}

// New returns a fresh Encryptor.
func New() *Encryptor { return &Encryptor{} }

// Encrypt records a copy of plaintext and returns ciphertext that
// will round-trip back through Decrypt.
// aad is ignored by this test double — AAD binding semantics (decrypt
// fails on mismatch) are covered against the real crypto encryptors
// (NewLocalEncryptor / NewGCPKMSEncryptor), not here. This type only needs
// to round-trip and keep plaintext distinguishable from ciphertext for
// handler tests.
func (e *Encryptor) Encrypt(plaintext, _ []byte) ([]byte, error) {
	e.mu.Lock()
	e.EncryptedPlaintexts = append(e.EncryptedPlaintexts, append([]byte(nil), plaintext...))
	e.mu.Unlock()

	out := make([]byte, 0, len(envelopePrefix)+len(plaintext))
	out = append(out, envelopePrefix...)
	out = append(out, plaintext...)
	return out, nil
}

// Decrypt strips the envelope prefix. Returns ErrNotEncrypted if
// the input lacks the prefix — a deliberate signal that the caller
// stored plaintext where ciphertext belongs.
func (e *Encryptor) Decrypt(ciphertext, _ []byte) ([]byte, error) {
	if !bytes.HasPrefix(ciphertext, envelopePrefix) {
		return nil, ErrNotEncrypted
	}
	return bytes.Clone(ciphertext[len(envelopePrefix):]), nil
}

// Compile-time check: Encryptor must satisfy the production
// interface so the harness can wire it transparently.
var _ crypto.Encryptor = (*Encryptor)(nil)
