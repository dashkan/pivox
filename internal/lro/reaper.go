package lro

import (
	"context"
	"log/slog"
	"time"

	db "github.com/pivoxai/pivox/internal/db/generated"
)

// Reaper periodically deletes expired operations.
type Reaper struct {
	queries  *db.Queries
	interval time.Duration
	logger   *slog.Logger
}

// NewReaper creates a new operation reaper.
func NewReaper(queries *db.Queries, interval time.Duration, logger *slog.Logger) *Reaper {
	return &Reaper{
		queries:  queries,
		interval: interval,
		logger:   logger,
	}
}

// Run starts the reaper loop. It blocks until the context is cancelled.
func (r *Reaper) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.queries.DeleteExpiredOperations(ctx); err != nil {
				r.logger.Error("failed to delete expired operations", "error", err)
			}
		}
	}
}
