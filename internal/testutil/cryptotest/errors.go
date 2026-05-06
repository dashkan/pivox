package cryptotest

import "errors"

// ErrNotEncrypted is returned by Encryptor.Decrypt when the input
// lacks the envelope prefix. Tests that want to assert "the handler
// did not encrypt this value" can branch on errors.Is; the typical
// failure mode is a handler that stored a raw value where
// ciphertext was expected.
var ErrNotEncrypted = errors.New("cryptotest: input is not encrypted (missing envelope prefix)")
