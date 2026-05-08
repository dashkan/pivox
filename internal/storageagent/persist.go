package storageagent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"google.golang.org/protobuf/proto"

	// Pure-Go SQLite driver. Registers under the "sqlite" driver name.
	// Pinned via go.mod; no cgo needed for cross-compiled agent builds.
	_ "modernc.org/sqlite"

	agentv1 "github.com/dashkan/pivox/internal/pkg/gen/pivox/agent/v1"
)

// schemaVersion identifies the on-disk shape of the agent local store.
// Bump in lockstep with any DDL change in `schema`. OpenStore fails fast
// if it encounters an existing DB at a different version — operators
// recover by deleting the DB file (the controller re-delivers state on
// the next handshake). When this hits production, this becomes numbered
// migrations; pre-prod freedom (per root CLAUDE.md) lets us evolve the
// schema directly for now.
const schemaVersion = 1

// schema is the agent local store DDL. Applied on every Open via
// CREATE IF NOT EXISTS, gated by a schemaVersion equality check.
const schema = `
CREATE TABLE IF NOT EXISTS schema_meta (
    version INTEGER PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS sessions (
    token         TEXT PRIMARY KEY,
    patterns_json BLOB NOT NULL,
    expiry_unix   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry_idx ON sessions (expiry_unix);

CREATE TABLE IF NOT EXISTS denied_patterns (
    pattern TEXT PRIMARY KEY
);

CREATE TABLE IF NOT EXISTS endpoints (
    name         TEXT PRIMARY KEY,
    config_proto BLOB NOT NULL
);
`

// Store is the storage agent's local SQLite-backed persistence layer.
// SessionStore, DeniedPatterns, and EndpointStore use it as a write-through
// backing for their in-memory state, so an agent restart preserves
// granted sessions, denied patterns, and endpoint configurations without
// requiring a controller round-trip first.
//
// Logging contract: this layer is intentionally silent. It returns
// errors (wrapped with context) and, where useful, row counts. Callers
// own all logging decisions — no `*slog.Logger` is threaded through.
//
// Concurrency: Store is safe for concurrent use. The connection pool is
// pinned to one connection: SQLite serializes writes regardless, and a
// single conn avoids SQLITE_BUSY churn from competing writer
// transactions. WAL mode + a 5s busy timeout absorb routine contention.
// Trade-off: a long-running write transaction (e.g. a large
// `ReplaceEndpoints`) blocks reads (e.g. `LoadSessions`) until commit.
// For today's volumes — single-digit denied patterns, low-tens
// endpoints, sub-millisecond writes — this is acceptable and chosen
// deliberately. If write durations grow, raise the cap to 2 conns and
// rely on WAL+busy_timeout for read/write isolation.
type Store struct {
	db *sql.DB
}

// StoreConfig is the constructor input for Store.
type StoreConfig struct {
	// Path is the filesystem path to the SQLite database file. Required.
	// Created if it does not exist.
	Path string
}

// StoredSession is the persistence-layer representation of a granted
// session. Patterns is JSON-encoded on disk; Expiry is stored as a
// Unix-second timestamp (sub-second precision is dropped — acceptable
// because session lifetimes are measured in minutes/hours, not ms).
type StoredSession struct {
	Token    string
	Patterns []string
	Expiry   time.Time
}

// OpenStore opens (or creates) a SQLite database per cfg and applies
// the agent's local schema. Pragmas: WAL journal, NORMAL sync (durable
// across process crashes; loses at most the last fsync's worth on power
// loss — acceptable for a write-through cache the controller can
// re-deliver), 5s busy timeout, foreign keys ON.
//
// Panics if cfg.Path is empty (programmer error — the agent boot path
// is responsible for resolving Path from operator config). Returns an
// error if the file cannot be opened, the schema cannot be applied, or
// the on-disk schema version does not match schemaVersion.
func OpenStore(cfg StoreConfig) (*Store, error) {
	if cfg.Path == "" {
		panic("storageagent: StoreConfig.Path is required")
	}

	// modernc.org/sqlite consumes pragmas via the URL `_pragma` query
	// parameter. They run before the first user statement, so the WAL
	// switch is in effect by the time the schema applies.
	dsn := "file:" + cfg.Path + "?" + url.Values{
		"_pragma": []string{
			"journal_mode(WAL)",
			"synchronous(NORMAL)",
			"busy_timeout(5000)",
			"foreign_keys(ON)",
		},
	}.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", cfg.Path, err)
	}

	// SQLite serializes writes regardless; capping the pool at 1 avoids
	// SQLITE_BUSY churn from multiple connections all trying to begin a
	// writer transaction. See the doc comment on Store for the trade-off.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx := context.Background()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	if err := ensureSchemaVersion(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db}, nil
}

// ensureSchemaVersion checks that the on-disk DB matches the agent
// binary's schemaVersion. On a fresh DB it inserts schemaVersion. On a
// version mismatch it returns an error so the operator can blow away
// the file (the controller re-delivers state on the next handshake).
func ensureSchemaVersion(ctx context.Context, db *sql.DB) error {
	var existing sql.NullInt64
	if err := db.QueryRowContext(ctx,
		`SELECT version FROM schema_meta LIMIT 1`).Scan(&existing); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read schema_meta: %w", err)
		}
		// Fresh DB — insert our version.
		if _, err := db.ExecContext(ctx,
			`INSERT INTO schema_meta (version) VALUES (?)`, schemaVersion); err != nil {
			return fmt.Errorf("insert schema_meta: %w", err)
		}
		return nil
	}
	if existing.Int64 != schemaVersion {
		return fmt.Errorf(
			"schema_meta.version mismatch: on-disk=%d, binary=%d (delete the agent db file to recover; the controller re-delivers state on next handshake)",
			existing.Int64, schemaVersion)
	}
	return nil
}

// Close closes the underlying database handle.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// ---------------------------------------------------------------------------
// Sessions
// ---------------------------------------------------------------------------

// SaveSession upserts a session row. A row with the same token is
// replaced (matches in-memory `SessionStore.Grant` which overwrites).
// Token must be non-empty.
func (s *Store) SaveSession(ctx context.Context, sess StoredSession) error {
	if sess.Token == "" {
		return errors.New("save session: token is required")
	}
	patternsJSON, err := json.Marshal(sess.Patterns)
	if err != nil {
		return fmt.Errorf("marshal session patterns: %w", err)
	}

	const stmt = `
INSERT INTO sessions (token, patterns_json, expiry_unix)
VALUES (?, ?, ?)
ON CONFLICT(token) DO UPDATE SET
    patterns_json = excluded.patterns_json,
    expiry_unix   = excluded.expiry_unix
`
	if _, err := s.db.ExecContext(ctx, stmt, sess.Token, patternsJSON, sess.Expiry.Unix()); err != nil {
		return fmt.Errorf("save session: %w", err)
	}
	return nil
}

// DeleteSession removes a single session by token. No-op if absent.
func (s *Store) DeleteSession(ctx context.Context, token string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token = ?`, token); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpiredSessions removes every session whose expiry is strictly
// before now (boundary matches in-memory `SessionStore.Authorize`,
// which uses `time.Now().After(sess.Expiry)` — strict). Returns the
// number of rows deleted so the caller can log a useful summary.
func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE expiry_unix < ?`, now.Unix())
	if err != nil {
		return 0, fmt.Errorf("flush expired sessions: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		// modernc.org/sqlite always populates RowsAffected, but the
		// database/sql contract doesn't guarantee it; fall back to 0.
		return 0, nil //nolint:nilerr // count is best-effort
	}
	return int(n), nil
}

// LoadSessions returns every session row. Used at agent boot to
// repopulate the in-memory `SessionStore`.
func (s *Store) LoadSessions(ctx context.Context) ([]StoredSession, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT token, patterns_json, expiry_unix FROM sessions`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []StoredSession
	for rows.Next() {
		var (
			token        string
			patternsJSON []byte
			expiryUnix   int64
		)
		if err := rows.Scan(&token, &patternsJSON, &expiryUnix); err != nil {
			return nil, fmt.Errorf("scan session: %w", err)
		}
		var patterns []string
		if err := json.Unmarshal(patternsJSON, &patterns); err != nil {
			return nil, fmt.Errorf("unmarshal session patterns for %q: %w", token, err)
		}
		out = append(out, StoredSession{
			Token:    token,
			Patterns: patterns,
			Expiry:   time.Unix(expiryUnix, 0).UTC(),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Denied patterns
// ---------------------------------------------------------------------------

// ReplaceDeniedPatterns atomically replaces the denied-patterns set.
// Mirrors `DeniedPatterns.Update` (full replacement; the controller
// always pushes the full set in HandshakeAck and ConfigUpdate).
func (s *Store) ReplaceDeniedPatterns(ctx context.Context, patterns []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin denied-patterns tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM denied_patterns`); err != nil {
		return fmt.Errorf("clear denied patterns: %w", err)
	}
	for _, p := range patterns {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO denied_patterns (pattern) VALUES (?)
             ON CONFLICT(pattern) DO NOTHING`, p); err != nil {
			return fmt.Errorf("insert denied pattern %q: %w", p, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit denied patterns: %w", err)
	}
	return nil
}

// LoadDeniedPatterns returns every denied pattern. Used at agent boot.
func (s *Store) LoadDeniedPatterns(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pattern FROM denied_patterns`)
	if err != nil {
		return nil, fmt.Errorf("query denied patterns: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan denied pattern: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate denied patterns: %w", err)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Endpoints
// ---------------------------------------------------------------------------

// ReplaceEndpoints atomically replaces the endpoint set. Configs are
// stored as serialized proto bytes so the schema is stable across
// proto evolution: protobuf's wire format is forward/backward
// compatible — added fields silently round-trip, removed fields drop
// on unmarshal. Acceptable under pre-prod freedom.
//
// A nil entry mid-slice is a programmer error and returns an error
// rather than being silently skipped (a dropped endpoint would lead to
// 404s the operator can't explain).
func (s *Store) ReplaceEndpoints(ctx context.Context, configs []*agentv1.EndpointConfig) error {
	for i, cfg := range configs {
		if cfg == nil {
			return fmt.Errorf("replace endpoints: configs[%d] is nil", i)
		}
		if cfg.GetName() == "" {
			return fmt.Errorf("replace endpoints: configs[%d] has empty name", i)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin endpoints tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM endpoints`); err != nil {
		return fmt.Errorf("clear endpoints: %w", err)
	}
	for _, cfg := range configs {
		b, err := proto.Marshal(cfg)
		if err != nil {
			return fmt.Errorf("marshal endpoint %q: %w", cfg.GetName(), err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO endpoints (name, config_proto) VALUES (?, ?)
             ON CONFLICT(name) DO UPDATE SET config_proto = excluded.config_proto`,
			cfg.GetName(), b); err != nil {
			return fmt.Errorf("insert endpoint %q: %w", cfg.GetName(), err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit endpoints: %w", err)
	}
	return nil
}

// LoadEndpoints returns every endpoint config. Used at agent boot.
func (s *Store) LoadEndpoints(ctx context.Context) ([]*agentv1.EndpointConfig, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, config_proto FROM endpoints`)
	if err != nil {
		return nil, fmt.Errorf("query endpoints: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []*agentv1.EndpointConfig
	for rows.Next() {
		var (
			name        string
			configProto []byte
		)
		if err := rows.Scan(&name, &configProto); err != nil {
			return nil, fmt.Errorf("scan endpoint: %w", err)
		}
		var cfg agentv1.EndpointConfig
		if err := proto.Unmarshal(configProto, &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal endpoint %q: %w", name, err)
		}
		out = append(out, &cfg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate endpoints: %w", err)
	}
	return out, nil
}
