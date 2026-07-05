package crypto_test

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dashkan/pivox/internal/crypto"
)

func newLocal(t *testing.T) crypto.Encryptor {
	t.Helper()
	ks, err := crypto.GenerateCleartextKeyset()
	require.NoError(t, err)
	enc, err := crypto.NewLocalEncryptor(ks)
	require.NoError(t, err)
	return enc
}

func TestLocalEncryptor_RoundTrip(t *testing.T) {
	t.Parallel()
	enc := newLocal(t)
	pt := []byte("acme-sso-client-secret")
	aad := []byte("sso-client-secret:org-acme")

	ct, err := enc.Encrypt(pt, aad)
	require.NoError(t, err)
	assert.NotEqual(t, pt, ct)

	got, err := enc.Decrypt(ct, aad)
	require.NoError(t, err)
	assert.Equal(t, pt, got)
}

func TestLocalEncryptor_NilAADRoundTrips(t *testing.T) {
	t.Parallel()
	enc := newLocal(t)
	pt := []byte("no-binding-context")

	ct, err := enc.Encrypt(pt, nil)
	require.NoError(t, err)
	got, err := enc.Decrypt(ct, nil)
	require.NoError(t, err)
	assert.Equal(t, pt, got)
}

func TestLocalEncryptor_AADMismatchFails(t *testing.T) {
	t.Parallel()
	enc := newLocal(t)
	ct, err := enc.Encrypt([]byte("bound"), []byte("org-A"))
	require.NoError(t, err)

	_, err = enc.Decrypt(ct, []byte("org-B"))
	require.Error(t, err, "wrong aad must fail to decrypt")

	_, err = enc.Decrypt(ct, nil)
	require.Error(t, err, "missing aad must fail when it was bound")
}

func TestLocalEncryptor_NonDeterministic(t *testing.T) {
	t.Parallel()
	enc := newLocal(t)
	a, err := enc.Encrypt([]byte("same"), nil)
	require.NoError(t, err)
	b, err := enc.Encrypt([]byte("same"), nil)
	require.NoError(t, err)
	assert.NotEqual(t, a, b, "same plaintext must not yield identical ciphertext")
}

func TestLocalEncryptor_CrossKeysetFails(t *testing.T) {
	t.Parallel()
	enc1, enc2 := newLocal(t), newLocal(t) // independent keysets
	ct, err := enc1.Encrypt([]byte("x"), nil)
	require.NoError(t, err)

	_, err = enc2.Decrypt(ct, nil)
	require.Error(t, err, "ciphertext from a different keyset must not decrypt")
}

func TestNewLocalEncryptor_BadKeyset(t *testing.T) {
	t.Parallel()
	_, err := crypto.NewLocalEncryptor(nil)
	require.Error(t, err)
	_, err = crypto.NewLocalEncryptor([]byte("garbage"))
	require.Error(t, err)
}

func TestNewEncryptor_LocalProvider(t *testing.T) {
	t.Parallel()
	ks, err := crypto.GenerateCleartextKeyset()
	require.NoError(t, err)

	enc, err := crypto.NewEncryptor(crypto.EncryptorConfig{
		Provider:       crypto.ProviderLocal,
		LocalKeysetB64: base64.StdEncoding.EncodeToString(ks),
	})
	require.NoError(t, err)

	ct, err := enc.Encrypt([]byte("hi"), []byte("aad"))
	require.NoError(t, err)
	got, err := enc.Decrypt(ct, []byte("aad"))
	require.NoError(t, err)
	assert.Equal(t, []byte("hi"), got)
}

func TestNewEncryptor_Errors(t *testing.T) {
	t.Parallel()
	_, err := crypto.NewEncryptor(crypto.EncryptorConfig{Provider: "nonsense"})
	require.Error(t, err, "unknown provider")

	_, err = crypto.NewEncryptor(crypto.EncryptorConfig{Provider: crypto.ProviderLocal})
	require.Error(t, err, "local provider without a keyset")

	_, err = crypto.NewEncryptor(crypto.EncryptorConfig{Provider: crypto.ProviderGCP})
	require.Error(t, err, "gcp provider without a key name")
}
