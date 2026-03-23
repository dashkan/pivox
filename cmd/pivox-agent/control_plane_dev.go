//go:build dev

package main

import "github.com/spf13/pflag"

var controlPlaneAddr = "localhost:50051"

func addControlPlaneFlag(f *pflag.FlagSet) {
	f.StringVar(&controlPlaneAddr, "control-plane", envOrDefault("PIVOX_CONTROL_PLANE", "localhost:50051"), "Control plane gRPC address (dev only)")
}
