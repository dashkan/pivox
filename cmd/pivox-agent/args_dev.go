//go:build dev

package main

import "github.com/spf13/pflag"

// Service-to-service port (:50052), not the public gRPC port (:50051).
// AgentService lives on its own listener with registration-token auth;
// see cmd/pivox-cloud/main.go and configs/nginx.conf.
var cloudHost = "localhost:50052"

const defaultPort = 8443

func addControlPlaneFlag(f *pflag.FlagSet) {
	f.StringVar(&cloudHost, "server", envOrDefault("PIVOX_CLOUD_HOST", cloudHost), "Pivox server gRPC address (dev only)")
}
