package db

import (
	"context"
	"fmt"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	pgxvector "github.com/pgvector/pgvector-go/pgx"
)

// NewPool builds a pgxpool from a libpq URL, applying the two cross-cutting
// concerns every Pivox pool needs, then connects and pings:
//
//   - otelpgx query tracer — a span per query (no-op when OTel export is off).
//   - pgvector type registration per connection — REQUIRED so `vector` columns
//     (currently assets.embedding) decode; without it the first vector read
//     panics. Lives in AfterConnect (a closure, easy to forget when wiring a
//     pool by hand), which is exactly why this is centralized here rather than
//     re-declared in each binary + the test harness.
//
// Optional opts tweak the parsed config before connecting (after the tracer +
// pgvector defaults are applied) — e.g. the test harness sets a search_path.
//
// The caller owns the returned pool and must Close it.
func NewPool(ctx context.Context, url string, opts ...func(*pgxpool.Config)) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return pgxvector.RegisterTypes(ctx, conn)
	}
	for _, opt := range opts {
		opt(cfg)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return pool, nil
}
