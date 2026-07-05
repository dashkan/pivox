package storage

import (
	"errors"
	"fmt"
)

// BackendConfig selects and configures a storage Backend. Provider picks
// the implementation; the matching per-provider config field must be set.
//
// Credentials arrive already decrypted — the service layer owns
// decryption (internal/crypto), keeping this package free of key
// material handling and crypto imports.
type BackendConfig struct {
	// Provider selects the backend implementation. Required.
	Provider ProviderType
	// S3 configures an AWS_S3_COMPATIBLE backend. Required when Provider
	// is ProviderAWSS3Compatible; ignored otherwise.
	S3 *S3Config
}

// NewBackend constructs the Backend implementation for cfg.Provider.
//
// It returns the Backend interface (not a concrete type) because it
// dispatches across provider implementations — the one place in this
// package where returning an interface is correct.
func NewBackend(cfg BackendConfig) (Backend, error) {
	switch cfg.Provider {
	case "":
		return nil, errors.New("storage: BackendConfig.Provider is required")
	case ProviderAWSS3Compatible:
		if cfg.S3 == nil {
			return nil, fmt.Errorf("storage: provider %s requires an S3 config", cfg.Provider)
		}
		return newS3Backend(*cfg.S3)
	default:
		// Azure/GCS/filesystem are defined but not yet implemented.
		return nil, fmt.Errorf("storage: unsupported provider %q", cfg.Provider)
	}
}
