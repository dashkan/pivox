package config

import (
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL  string
	GRPCPort     string
	RESTPort     string
	DebugPort    string
	WorkerCount  int
	LogLevel     string
	SharedSecret string
	GoogleCloud  GoogleCloudConfig
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

func Load() *Config {
	return &Config{
		DatabaseURL:  getEnv("DATABASE_URL", "postgres://localhost:5432/pivox?sslmode=disable"),
		GRPCPort:     getEnv("GRPC_PORT", ":50051"),
		RESTPort:     getEnv("REST_PORT", ":8080"),
		DebugPort:    getEnv("DEBUG_PORT", ":9090"),
		WorkerCount:  getEnvInt("WORKER_COUNT", 5),
		LogLevel:     getEnv("LOG_LEVEL", "info"),
		SharedSecret: getEnv("SHARED_SECRET", "dev-secret"),
		GoogleCloud: GoogleCloudConfig{
			ProjectID:          getEnv("GOOGLE_CLOUD_PROJECT_ID", ""),
			ServiceAccountKey:  getEnv("GOOGLE_CLOUD_SERVICE_ACCOUNT_KEY", ""),
			ServiceAccountFile: getEnv("GOOGLE_CLOUD_SERVICE_ACCOUNT_FILE", ""),
		},
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}
