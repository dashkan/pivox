package config

// Config holds all server configuration. Populated from cobra flags
// in cmd/pivox-server/main.go, with env var fallbacks.
type Config struct {
	DatabaseURL string
	GRPCPort    string
	RESTPort    string
	DebugPort   string
	LogLevel    string
	GoogleCloud GoogleCloudConfig
	SyncAuth    SyncAuthConfig
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
