package config

import "time"

// Config holds all server configuration. Populated from cobra flags
// in cmd/pivox-cloud/main.go, with env var fallbacks.
//
// Note: there is no GoogleCloudConfig — Firebase credentials and
// project ID resolve entirely through Google's standard Application
// Default Credentials chain (service-account JSON → metadata server →
// gcloud user identity). Operators do not configure GCP knobs in
// Pivox-named env vars.
type Config struct {
	DatabaseURL      string
	GRPCPort         string
	ServiceGRPCPort  string // service-to-service gRPC listener (AgentService et al.)
	RESTPort         string
	DebugPort        string
	LogLevel         string
	RateLimitEnabled bool
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
