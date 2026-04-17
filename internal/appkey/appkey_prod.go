//go:build !dev

package appkey

import "fmt"

// newFromEnvImpl (prod): an unset PIVOX_APP_KEY is a fatal misconfiguration.
// The key MUST be stable across instances and restarts — a per-process
// random key would silently break pagination under load balancing.
func newFromEnvImpl(keyHex string) (*Codec, error) {
	if keyHex == "" {
		return nil, fmt.Errorf("appkey: PIVOX_APP_KEY is required in production builds")
	}
	return NewFromHex(keyHex)
}
