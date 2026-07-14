package health

import (
	"os"

	"github.com/spf13/pflag"
)

// DefaultAddr is the health listen address EVERY Pivox binary defaults to.
//
// Uniform on purpose: in production each process has its own container, so one
// port means one probe config, one scrape config, one dashboard panel — no
// per-service special-casing. In dev they share a host and would collide, so the
// AppHost overrides PIVOX_DEBUG_PORT per process (api :9090, worker :9091,
// agent :9092).
const DefaultAddr = ":9090"

const (
	flagName  = "debug-port"
	envName   = "PIVOX_DEBUG_PORT"
	flagUsage = "Debug/health listen address, serving /healthz (liveness) and " +
		"/readyz (readiness). Defaults to " + DefaultAddr + " in every Pivox binary — " +
		"in production each has its own container, so a uniform port means one probe " +
		"config. Override (PIVOX_DEBUG_PORT) when several run on one host, as they do in dev."
)

// RegisterFlag registers --debug-port on f, defaulting to PIVOX_DEBUG_PORT and
// then to DefaultAddr.
//
// Shared rather than copy-pasted into each cmd: three identical flag
// declarations would drift, and the port is exactly the kind of thing that must
// not (a prober pointed at the wrong port reports a healthy service as down).
func RegisterFlag(f *pflag.FlagSet) {
	def := DefaultAddr
	if v := os.Getenv(envName); v != "" {
		def = v
	}
	f.String(flagName, def, flagUsage)
}

// Addr returns the resolved health listen address from a flag set that
// RegisterFlag was called on.
func Addr(f *pflag.FlagSet) string {
	// The error case is "the flag was never registered", i.e. a programmer error
	// that DefaultAddr keeps from silently yielding an empty listen address (which
	// would bind a random port and make the endpoint unfindable).
	v, err := f.GetString(flagName)
	if err != nil || v == "" {
		return DefaultAddr
	}
	return v
}
