package config

import "time"

// Config holds all server configuration. Populated from cobra flags
// in cmd/pivox-cloud/main.go, with env var fallbacks.
type Config struct {
	DatabaseURL      string
	GRPCPort         string
	RESTPort         string
	DebugPort        string
	LogLevel         string
	RateLimitEnabled bool
	GoogleCloud      GoogleCloudConfig
	SyncAuth         SyncAuthConfig
	DelegatedAuth    DelegatedAuthConfig
}

// DelegatedAuthConfig controls the delegated auth session flow used by
// plugins (NRCS ActiveX, Adobe UXP, etc.) that cannot safely authenticate
// in-process and must hand off to the Pivox app (AUTHN-07).
type DelegatedAuthConfig struct {
	// SessionTTL bounds how long a plugin has between creating a session
	// and the app completing it before the code expires.
	SessionTTL time.Duration

	// PollInterval is returned to clients in the createDelegatedAuthSession
	// response so they poll at a rate the server is comfortable with.
	PollInterval time.Duration
}

// GoogleCloudConfig holds Google Cloud / Firebase configuration.
// Credential resolution order:
//  1. ServiceAccountKey (inline JSON) — useful for containers / CI
//  2. ServiceAccountFile (path to JSON key file) — local dev with explicit key
//  3. GOOGLE_APPLICATION_CREDENTIALS env var — standard ADC file-based auth
//  4. Application Default Credentials — metadata server, gcloud auth, workload identity
//
// ProjectID is always required for Firebase Auth token verification. It is
// auto-detected from a service account key if provided, but must be set
// explicitly when using ADC on environments where it cannot be inferred
// (e.g. local dev without gcloud project configured).
type GoogleCloudConfig struct {
	ProjectID          string
	ServiceAccountKey  string
	ServiceAccountFile string
}
