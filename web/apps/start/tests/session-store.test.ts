import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { SessionTokens } from '../src/server/oidc/session';

// The store talks to Postgres via postgres.js's tagged-template `sql`. We mock
// the driver factory and capture every tagged-template call's (strings, values)
// so we can assert BEHAVIOR — that dynamic data is bound as a parameter (lands
// in `values`, never concatenated into the SQL text), that lazy-expire deletes
// then returns undefined, that the pool is created once, etc.
interface TaggedCall {
  text: string;
  values: unknown[];
}

type Tag = ((strings: TemplateStringsArray, ...values: unknown[]) => Promise<unknown[]>) & {
  json: (value: unknown) => unknown;
};

const { factory, state } = vi.hoisted(() => {
  const state: { calls: TaggedCall[]; results: unknown[][] } = { calls: [], results: [] };
  const tag: Tag = Object.assign(
    (strings: TemplateStringsArray, ...values: unknown[]): Promise<unknown[]> => {
      state.calls.push({ text: Array.from(strings).join('?'), values });
      return Promise.resolve(state.results.shift() ?? []);
    },
    // Mirrors postgres.js `sql.json(v)`: marks a value as a jsonb parameter so
    // the test can prove the token blob is bound, not string-interpolated.
    { json: (value: unknown) => ({ __json: value }) },
  );
  const factory = vi.fn(() => tag);
  return { factory, state };
});

vi.mock('postgres', () => ({ default: factory }));

const DB_URL = 'postgres://u:p@localhost:5432/sessions?sslmode=disable';
const DAY_MS = 1000 * 60 * 60 * 24;

// The store ensures its schema once per process before the first op: a CREATE
// TABLE + two CREATE INDEX statements (all IF NOT EXISTS). Every test loads a
// fresh module (vi.resetModules in beforeEach), so this fires exactly once per
// test, ahead of the operation under test. Tests skip these to assert only the
// operation's own SQL.
const SCHEMA_STMT_COUNT = 3;

/** The store calls minus the leading schema-ensure statements. */
function opCalls(): TaggedCall[] {
  return state.calls.slice(SCHEMA_STMT_COUNT);
}

// The mock drains `state.results` FIFO for every tagged call, including the
// schema-ensure DDL that runs first. DDL returns nothing, so pad the queue with
// empty rows for those statements and queue the real op results after them.
function primeResults(...results: unknown[][]): void {
  state.results = [...new Array<unknown[]>(SCHEMA_STMT_COUNT).fill([]), ...results];
}

const TOKENS: SessionTokens = {
  access_token: 'access-1',
  refresh_token: 'refresh-1',
  id_token: 'id-1',
  expires_at: 1_700_000_000_000,
};

beforeEach(() => {
  // Reset the module so the lazily-created singleton pool is rebuilt per test.
  vi.resetModules();
  state.calls.length = 0;
  state.results = [];
  factory.mockClear();
  vi.stubEnv('PIVOX_SESSIONS_DATABASE_URL', DB_URL);
});

afterEach(() => {
  vi.unstubAllEnvs();
});

async function load() {
  return import('../src/server/oidc/session-store');
}

describe('createSession', () => {
  it('inserts the token set under a fresh 43-char base64url id and a 30d horizon', async () => {
    const { createSession } = await load();

    const before = Date.now();
    const id = await createSession(TOKENS, 'sub-123');

    // 32 bytes base64url == 43 chars, alphabet [A-Za-z0-9_-].
    expect(id).toMatch(/^[A-Za-z0-9_-]{43}$/);

    expect(opCalls()).toHaveLength(1);
    const [call] = opCalls();
    expect(call.text).toContain('INSERT INTO web_sessions');

    // Parameterized: id, jsonb(tokens), sub, expires_at are all bound values.
    expect(call.values[0]).toBe(id);
    expect(call.values[1]).toEqual({ __json: TOKENS });
    expect(call.values[2]).toBe('sub-123');
    expect(call.values[3]).toBeInstanceOf(Date);

    const horizonMs = (call.values[3] as Date).getTime() - before;
    expect(horizonMs).toBeGreaterThan(29 * DAY_MS);
    expect(horizonMs).toBeLessThanOrEqual(30 * DAY_MS + 1000);

    // The secret id and sub never appear in the SQL text — proof of binding.
    expect(call.text).not.toContain(id);
    expect(call.text).not.toContain('sub-123');
  });

  it('generates a distinct id on each call', async () => {
    const { createSession } = await load();
    const a = await createSession(TOKENS, 's');
    const b = await createSession(TOKENS, 's');
    expect(a).not.toBe(b);
  });
});

describe('getSession', () => {
  it('returns the stored tokens for a live row (single SELECT, no delete)', async () => {
    const { getSession } = await load();
    primeResults([{ tokens: TOKENS, expires_at: new Date(Date.now() + 60_000) }]);

    const got = await getSession('id-abc');

    expect(got).toEqual(TOKENS);
    expect(opCalls()).toHaveLength(1);
    const [call] = opCalls();
    expect(call.text).toContain('SELECT tokens, expires_at FROM web_sessions WHERE id =');
    expect(call.values).toEqual(['id-abc']);
    expect(call.text).not.toContain('id-abc');
  });

  it('returns undefined for a missing row without issuing a delete', async () => {
    const { getSession } = await load();
    primeResults([]);

    expect(await getSession('missing')).toBeUndefined();
    expect(opCalls()).toHaveLength(1);
  });

  it('lazy-expires: deletes the row and returns undefined when past the horizon', async () => {
    const { getSession } = await load();
    primeResults([{ tokens: TOKENS, expires_at: new Date(Date.now() - 1000) }]);

    expect(await getSession('stale')).toBeUndefined();

    // SELECT, then DELETE of the same id (schema-ensure is memoized, so the
    // nested deleteSession adds no further DDL).
    const ops = opCalls();
    expect(ops).toHaveLength(2);
    expect(ops[1].text).toContain('DELETE FROM web_sessions WHERE id =');
    expect(ops[1].values).toEqual(['stale']);
  });
});

describe('updateSession', () => {
  it('updates tokens, bumps updated_at, and slides the horizon forward', async () => {
    const { updateSession } = await load();
    const before = Date.now();

    await updateSession('id-9', TOKENS);

    expect(opCalls()).toHaveLength(1);
    const [call] = opCalls();
    expect(call.text).toContain('UPDATE web_sessions');
    expect(call.text).toContain('updated_at = now()');
    expect(call.values[0]).toEqual({ __json: TOKENS });
    expect(call.values[1]).toBeInstanceOf(Date);
    expect(call.values[2]).toBe('id-9');

    const horizonMs = (call.values[1] as Date).getTime() - before;
    expect(horizonMs).toBeGreaterThan(29 * DAY_MS);
  });
});

describe('deleteSession / deleteSessionsBySub', () => {
  it('deletes a single session by id (parameterized)', async () => {
    const { deleteSession } = await load();
    await deleteSession('id-d');
    expect(opCalls()[0].text).toContain('DELETE FROM web_sessions WHERE id =');
    expect(opCalls()[0].values).toEqual(['id-d']);
  });

  it('deletes all sessions for a subject (parameterized)', async () => {
    const { deleteSessionsBySub } = await load();
    await deleteSessionsBySub('sub-z');
    expect(opCalls()[0].text).toContain('DELETE FROM web_sessions WHERE sub =');
    expect(opCalls()[0].values).toEqual(['sub-z']);
  });
});

describe('schema ensure', () => {
  it('issues CREATE TABLE + index DDL (idempotent) before the first op', async () => {
    const { deleteSession } = await load();
    await deleteSession('id-x');

    const ddl = state.calls.slice(0, SCHEMA_STMT_COUNT);
    expect(ddl[0].text).toContain('CREATE TABLE IF NOT EXISTS web_sessions');
    expect(ddl[1].text).toContain('CREATE INDEX IF NOT EXISTS web_sessions_expires_at_idx');
    expect(ddl[2].text).toContain('CREATE INDEX IF NOT EXISTS web_sessions_sub_idx');
    // The op itself follows the DDL.
    expect(opCalls()[0].text).toContain('DELETE FROM web_sessions WHERE id =');
  });

  it('runs the schema ensure exactly once across many ops in a process', async () => {
    const store = await load();
    await store.deleteSession('a');
    await store.deleteSession('b');
    await store.createSession(TOKENS, 'sub');
    await store.updateSession('a', TOKENS);

    const ddl = state.calls.filter((c) => c.text.includes('CREATE'));
    expect(ddl).toHaveLength(SCHEMA_STMT_COUNT);
  });
});

describe('connection pool', () => {
  it('creates the pool once from PIVOX_SESSIONS_DATABASE_URL and reuses it across calls', async () => {
    const store = await load();
    await store.deleteSession('a');
    await store.deleteSession('b');

    expect(factory).toHaveBeenCalledTimes(1);
    expect(factory).toHaveBeenCalledWith(DB_URL, { max: 10 });
  });

  it('throws when PIVOX_SESSIONS_DATABASE_URL is unset', async () => {
    vi.stubEnv('PIVOX_SESSIONS_DATABASE_URL', '');
    const { getSession } = await load();
    await expect(getSession('x')).rejects.toThrow('PIVOX_SESSIONS_DATABASE_URL is required');
    expect(factory).not.toHaveBeenCalled();
  });
});
