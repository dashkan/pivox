import { randomBytes } from 'node:crypto'

import postgres from 'postgres'

import type { SessionTokens } from './session'

/**
 * Postgres-backed session store for the OIDC (Keycloak) BFF.
 *
 * The browser holds only an opaque session id (see {@link createSession}) in the
 * httpOnly `__pivox_oidc` cookie; the token set lives here, in the `web_sessions`
 * table, and never reaches the client. The BFF reads a session on every request,
 * so each read is a single indexed primary-key lookup over a pooled connection.
 *
 * This store lives in its OWN database (`sessions`), not the app `pivox` DB. The
 * table is BFF-owned: the Go backend never reads it, and the `sessions` DB has no
 * Go migrations — so the BFF creates its own schema idempotently on first use
 * (see {@link ensureSchema}).
 *
 * Trust model: the opaque id IS the session secret (32 bytes of CSPRNG entropy),
 * and the token blob is stored as PLAINTEXT jsonb — Postgres is the trust
 * boundary (it already holds identities + auth artifacts), so app-level
 * encryption would add key management without moving the boundary. Revocation is
 * a row delete; refresh-token rotation is an UPDATE of `tokens` — the id is
 * stable across both.
 *
 * A Worker Process periodic job (`purge_web_sessions`) GCs rows past
 * `expires_at`; this module also lazy-expires on read (see {@link getSession}) so
 * a stale row is never honoured even between purge ticks.
 */

/**
 * Purge horizon for a session row, also the sliding-window length bumped on each
 * active use ({@link updateSession}). This is NOT the access-token lifetime
 * (that lives inside the `tokens` blob and is enforced at API-call time via
 * refresh-or-401); it bounds how long an idle session survives before GC.
 */
const SESSION_TTL_MS = 1000 * 60 * 60 * 24 * 30 // 30 days

/** Row shape we read back from `web_sessions`. */
interface SessionRow {
  tokens: SessionTokens
  expires_at: Date
}

let sqlInstance: postgres.Sql | undefined

/**
 * Binds the token set as a jsonb parameter. `SessionTokens` is JSON-safe by
 * construction (string/number/optional fields only) and, being a type alias
 * (not an interface), carries the implicit index signature postgres.js's
 * `json()` parameter requires — so it binds directly with no assertion.
 */
function jsonTokens(db: postgres.Sql, tokens: SessionTokens): ReturnType<postgres.Sql['json']> {
  return db.json(tokens)
}

/**
 * Lazily creates and memoizes the connection pool from
 * `PIVOX_SESSIONS_DATABASE_URL` (a libpq URL for the BFF-owned `sessions` DB).
 * Reused across requests — the BFF must not open a connection per session read.
 * Throws if the URL is unset, surfacing a misconfiguration at first use rather
 * than silently running session-less.
 */
function sql(): postgres.Sql {
  if (!sqlInstance) {
    const url = process.env.PIVOX_SESSIONS_DATABASE_URL
    if (!url) {
      throw new Error('PIVOX_SESSIONS_DATABASE_URL is required for the BFF session store')
    }
    // postgres.js parameterizes every interpolated value by default; we never
    // build SQL by string concatenation in this module.
    sqlInstance = postgres(url, { max: 10 })
  }
  return sqlInstance
}

/**
 * Memoized schema-ensure. The `sessions` DB has no Go migrations, so the BFF
 * owns its schema and creates it idempotently. This runs the CREATE TABLE/INDEX
 * statements exactly once per process: the first store call kicks it off and
 * caches the promise, every subsequent call awaits the same settled promise.
 * Each statement is `IF NOT EXISTS`, so it's safe under concurrent BFF
 * processes racing on a fresh DB.
 *
 * On failure the promise rejects and is NOT cached — the next call retries the
 * ensure rather than inheriting a permanently-poisoned promise.
 */
let ready: Promise<void> | undefined

function ensureSchema(): Promise<void> {
  if (!ready) {
    const db = sql()
    ready = (async () => {
      await db`
        CREATE TABLE IF NOT EXISTS web_sessions (
          id          TEXT PRIMARY KEY,
          tokens      JSONB NOT NULL,
          sub         TEXT NOT NULL,
          created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
          updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
          expires_at  TIMESTAMPTZ NOT NULL
        )
      `
      await db`CREATE INDEX IF NOT EXISTS web_sessions_expires_at_idx ON web_sessions (expires_at)`
      await db`CREATE INDEX IF NOT EXISTS web_sessions_sub_idx ON web_sessions (sub)`
    })().catch((err: unknown) => {
      // Drop the cached promise so a transient failure (DB not yet up) can be
      // retried on the next call rather than poisoning the process.
      ready = undefined
      throw err
    })
  }
  return ready
}

/**
 * Generates a fresh opaque session id and persists the token set under it.
 * The id is 32 bytes of CSPRNG entropy, base64url-encoded (43 chars) — it is the
 * sole bearer of the session, so it must be unguessable. `expires_at` is set to
 * the purge horizon (now + 30 days). Returns the id to set as the cookie value.
 */
export async function createSession(tokens: SessionTokens, sub: string): Promise<string> {
  await ensureSchema()
  const id = randomBytes(32).toString('base64url')
  const expiresAt = new Date(Date.now() + SESSION_TTL_MS)
  const db = sql()
  await db`
    INSERT INTO web_sessions (id, tokens, sub, expires_at)
    VALUES (${id}, ${jsonTokens(db, tokens)}, ${sub}, ${expiresAt})
  `
  return id
}

/**
 * Loads the token set for a session id. Returns `undefined` when the row is
 * absent OR has passed its purge horizon — in the latter case the row is deleted
 * first (lazy expiry), so an expired id can never be honoured even before the
 * purge job runs.
 */
export async function getSession(id: string): Promise<SessionTokens | undefined> {
  await ensureSchema()
  const db = sql()
  const rows = await db<SessionRow[]>`
    SELECT tokens, expires_at FROM web_sessions WHERE id = ${id}
  `
  if (rows.length === 0) return undefined
  const row = rows[0]
  if (row.expires_at.getTime() < Date.now()) {
    await deleteSession(id)
    return undefined
  }
  return row.tokens
}

/**
 * Replaces the token set for a session id and slides the purge horizon forward
 * (now + 30 days) so active sessions don't get GC'd. Used after a refresh-token
 * rotation; the id (and thus the cookie) is unchanged.
 */
export async function updateSession(id: string, tokens: SessionTokens): Promise<void> {
  await ensureSchema()
  const expiresAt = new Date(Date.now() + SESSION_TTL_MS)
  const db = sql()
  await db`
    UPDATE web_sessions
    SET tokens = ${jsonTokens(db, tokens)},
        updated_at = now(),
        expires_at = ${expiresAt}
    WHERE id = ${id}
  `
}

/** Deletes a single session (logout / dead-session cleanup / lazy expiry). */
export async function deleteSession(id: string): Promise<void> {
  await ensureSchema()
  const db = sql()
  await db`DELETE FROM web_sessions WHERE id = ${id}`
}

/**
 * Deletes every session for a subject — the primitive behind a future
 * "sign out everywhere" / forced-revocation flow. Wired now; no caller yet.
 */
export async function deleteSessionsBySub(sub: string): Promise<void> {
  await ensureSchema()
  const db = sql()
  await db`DELETE FROM web_sessions WHERE sub = ${sub}`
}
