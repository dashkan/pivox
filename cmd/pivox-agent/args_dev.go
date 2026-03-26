//go:build dev

package main

import "github.com/spf13/pflag"

var cloudHost = "localhost:50051"

const defaultPort = 8443

func addControlPlaneFlag(f *pflag.FlagSet) {
	f.StringVar(&cloudHost, "server", envOrDefault("PIVOX_CLOUD_HOST", cloudHost), "Pivox server gRPC address (dev only)")
}
