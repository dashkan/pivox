//go:build dev

package main

import "github.com/spf13/pflag"

var controlPlaneAddr = "localhost:50051"

const defaultPort = 8443

func addControlPlaneFlag(f *pflag.FlagSet) {
	f.StringVar(&controlPlaneAddr, "server", envOrDefault("PIVOX_SERVER", "localhost:50051"), "Pivox server gRPC address (dev only)")
}
