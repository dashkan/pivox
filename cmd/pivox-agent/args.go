//go:build !dev

package main

import "github.com/spf13/pflag"

var cloudHost = "api.pivox.app"

const defaultPort = 443

func addControlPlaneFlag(f *pflag.FlagSet) {
	f.StringVar(&cloudHost, "server", envOrDefault("PIVOX_CLOUD_HOST", cloudHost), "Pivox server gRPC address (dev only)")
}
