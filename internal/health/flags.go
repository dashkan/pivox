package health

import (
	"os"

	"github.com/spf13/pflag"
)

// DefaultAddr is the health listen address every Pivox binary defaults to. In
// production each process has its own container, so one port means one probe
// config; in dev they share a host and the AppHost overrides PIVOX_DEBUG_PORT.
const DefaultAddr = ":9090"

const (
	flagName  = "debug-port"
	envName   = "PIVOX_DEBUG_PORT"
	flagUsage = "Debug/health listen address, serving /healthz (liveness) and " +
		"/readyz (readiness). Defaults to " + DefaultAddr + " in every Pivox binary — " +
		"in production each has its own container, so a uniform port means one probe " +
		"config. Override (PIVOX_DEBUG_PORT) when several run on one host, as they do in dev."
)

// RegisterFlag registers --debug-port on f, defaulting to PIVOX_DEBUG_PORT then
// DefaultAddr. Shared rather than copy-pasted into each cmd so the three cannot
// drift.
func RegisterFlag(f *pflag.FlagSet) {
	def := DefaultAddr
	if v := os.Getenv(envName); v != "" {
		def = v
	}
	f.String(flagName, def, flagUsage)
}

// Addr returns the resolved health listen address. Falls back to DefaultAddr
// rather than "" (which would bind a random, unfindable port).
func Addr(f *pflag.FlagSet) string {
	v, err := f.GetString(flagName)
	if err != nil || v == "" {
		return DefaultAddr
	}
	return v
}
