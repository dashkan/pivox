package lro

import (
	"context"
	"log/slog"
	"time"

	db "github.com/dashkan/pivox/internal/db/generated"
)

// DefaultReaperInterval is the production scan cadence — used when
// ReaperConfig.Interval is left zero. Mirrors the cmd/pivox-cloud
// boot wiring so tests and call sites that omit Interval get the
// same behavior production does.
const DefaultReaperInterval = 5 * time.Minute

// Reaper periodically deletes expired operations.
type Reaper struct {
	queries  db.Querier
	interval time.Duration
	logger   *slog.Logger
}

// ReaperConfig is the constructor input for Reaper.
type ReaperConfig struct {
	// Queries is the sqlc query interface. Required.
	Queries db.Querier
	// Interval is the scan cadence. Zero ⇒ DefaultReaperInterval.
	Interval time.Duration
	// Logger is the slog logger used for sweep failures. Required.
	Logger *slog.Logger
}

// NewReaper constructs a Reaper from cfg. Panics on a missing
// required field — startup-time programmer error, fail loud on boot.
func NewReaper(cfg ReaperConfig) *Reaper {
	if cfg.Queries == nil {
		panic("lro: ReaperConfig.Queries is required")
	}
	if cfg.Logger == nil {
		panic("lro: ReaperConfig.Logger is required")
	}
	interval := cfg.Interval
	if interval == 0 {
		interval = DefaultReaperInterval
	}
	return &Reaper{
		queries:  cfg.Queries,
		interval: interval,
		logger:   cfg.Logger,
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
