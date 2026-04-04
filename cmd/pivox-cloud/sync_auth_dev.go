//go:build dev

package main

import (
	"github.com/dashkan/pivox/internal/config"
	"github.com/spf13/cobra"
)

func addSyncAuthFlags(cmd *cobra.Command) {
	cmd.Flags().String("shared-secret", envOrDefault("SHARED_SECRET", "dev-secret"), "Shared secret for internal service-to-service auth (dev only)")
}

func loadSyncAuthConfig(cmd *cobra.Command) config.SyncAuthConfig {
	return config.SyncAuthConfig{
		SharedSecret: must(cmd.Flags().GetString("shared-secret")),
	}
}
