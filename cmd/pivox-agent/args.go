//go:build !dev

package main

import "github.com/spf13/pflag"

const controlPlaneAddr = "api.pivox.app:443"

func addControlPlaneFlag(_ *pflag.FlagSet) {
	// In production builds, control plane address is hardcoded.
	// No flag is exposed.
}
