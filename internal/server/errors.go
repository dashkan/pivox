package server

import (
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dashkan/pivox-server/internal/apierr"
)

// handleResourceError translates common database errors into gRPC status errors.
func handleResourceError(err error, resourceType, resourceName string) error {
	if err == pgx.ErrNoRows {
		return apierr.NotFound(resourceType, resourceName)
	}
	if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
		return apierr.AlreadyExists(resourceType, resourceName)
	}
	return apierr.Internal("database error")
}
