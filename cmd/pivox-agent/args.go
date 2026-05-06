package main

import "github.com/spf13/pflag"

// cloudHost is the production gRPC endpoint for the Pivox cloud API.
// Operators override via --server / PIVOX_CLOUD_HOST when pointing
// the agent at a non-default deployment (staging, an ngrok tunnel,
// a self-hosted instance).
var cloudHost = "api.pivox.app"

const defaultPort = 443

func addControlPlaneFlag(f *pflag.FlagSet) {
	f.StringVar(&cloudHost, "server", envOrDefault("PIVOX_CLOUD_HOST", cloudHost), "Pivox server gRPC address")
}
