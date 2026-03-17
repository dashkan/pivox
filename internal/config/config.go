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
