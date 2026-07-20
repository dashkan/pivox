// @vitest-environment jsdom
import { createReactQueryApi } from '@pivox/client/react-query';
import { ConnectorEditFeature } from '@pivox/features/connectors';
import { SecretEditFeature } from '@pivox/features/secrets';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, waitFor } from '@testing-library/react';
import { StrictMode } from 'react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { ApiClient } from '@pivox/client';
import type { ReactNode } from 'react';

// Radix + form fields measure the DOM; jsdom needs these shims.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
  Element.prototype.scrollIntoView = () => {};
});

const CONNECTOR_DETAIL = '/v1/organizations/{organization}/connectors/{connector}';
const SECRET_DETAIL = '/v1/organizations/{organization}/secrets/{secret}';

/**
 * A REAL openapi-react-query `$api` backed by a spy client whose GET stays
 * in-flight for 40ms — long enough that an observer teardown mid-flight surfaces
 * as an aborted (canceled) request, the fingerprint we're guarding against.
 * `detailPath` selects which record path to count/abort-track.
 */
function makeSlowClient(detailPath: string, record: unknown) {
  const aborted: string[] = [];
  const detailCalls: string[] = [];
  const GET = vi.fn((path: string, init: { signal?: AbortSignal }) => {
    const isDetail = path === detailPath;
    if (isDetail) detailCalls.push(path);
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        resolve({
          data: isDetail ? record : { spaces: [] },
          error: undefined,
          response: { status: 200, headers: new Headers() },
        });
      }, 40);
      init?.signal?.addEventListener('abort', () => {
        if (isDetail) aborted.push(path);
        clearTimeout(timer);
        reject(new DOMException('aborted', 'AbortError'));
      });
    });
  });
  const client = {
    GET,
    POST: vi.fn(),
    PATCH: vi.fn(),
    DELETE: vi.fn(),
  } as unknown as ApiClient;
  return { client, detailCalls, aborted };
}

function renderWith(client: ApiClient, node: (api: ReturnType<typeof createReactQueryApi>) => ReactNode) {
  const $api = createReactQueryApi(client);
  // 60s staleTime mirrors the app's QueryClient default (router.tsx).
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
  });
  return render(<QueryClientProvider client={qc}>{node($api)}</QueryClientProvider>);
}

/**
 * PRODUCTION contract: opening the edit form issues the single-record detail
 * query EXACTLY ONCE, with no aborted (canceled) request. Production is not
 * wrapped in `<StrictMode>`, so a component mounts once — this is the behavior
 * users actually get in a prod build.
 *
 * NOTE (dev-only artifact, deliberately NOT asserted here): TanStack Start's
 * default client entry hydrates under `<StrictMode>`, which double-invokes
 * mount→unmount→mount in DEVELOPMENT. openapi-react-query's queryFn reads the
 * react-query `signal` (sets `#abortSignalConsumed`), so the strict-unmount
 * makes react-query CANCEL the in-flight request (`retryer.cancel`) instead of
 * reusing it; the strict-remount then refetches. That is the harmless
 * `sample-api (canceled)` + `sample-api 200` pair seen in the dev Network tab —
 * it does not occur in production and is not a masking target. This test locks
 * the prod contract so a genuine key-instability regression (org/space/id
 * resolving a render late) would flip the count to 2 and fail.
 */
describe('edit-form detail query fires once per mount (prod contract)', () => {
  it('connector edit-open: one detail fetch, no abort', async () => {
    const { client, detailCalls, aborted } = makeSlowClient(CONNECTOR_DETAIL, {
      name: 'organizations/local-corp/connectors/sample-api',
      displayName: 'Sample',
      http: { baseUrl: 'https://x' },
    });
    renderWith(client, ($api) => (
      <ConnectorEditFeature
        $api={$api}
        apiClient={client}
        parent="organizations/local-corp"
        connectorId="sample-api"
        agentOptions={[]}
        back={<a href="/connectors">back</a>}
        onCancel={() => {}}
        onSubmitSuccess={() => {}}
      />
    ));
    await waitFor(() => expect(detailCalls.length).toBeGreaterThan(0));
    await new Promise((r) => setTimeout(r, 90));
    expect({ detailCount: detailCalls.length, aborted }).toEqual({
      detailCount: 1,
      aborted: [],
    });
  });

  it('secret edit-open: one detail fetch, no abort', async () => {
    const { client, detailCalls, aborted } = makeSlowClient(SECRET_DETAIL, {
      name: 'organizations/local-corp/secrets/stripe-key',
      displayName: 'Stripe key',
    });
    renderWith(client, ($api) => (
      <SecretEditFeature
        $api={$api}
        apiClient={client}
        parent="organizations/local-corp"
        secretId="stripe-key"
        back={<a href="/secrets">back</a>}
        onCancel={() => {}}
        onSubmitSuccess={() => {}}
      />
    ));
    await waitFor(() => expect(detailCalls.length).toBeGreaterThan(0));
    await new Promise((r) => setTimeout(r, 90));
    expect({ detailCount: detailCalls.length, aborted }).toEqual({
      detailCount: 1,
      aborted: [],
    });
  });

  // Evidence pin: proves the dev double-fetch is a StrictMode artifact, not a
  // key/enabled bug. Under <StrictMode> the SAME code path double-mounts and the
  // first in-flight detail request is canceled (openapi-react-query consumes the
  // abort signal → react-query cancels on the strict-unmount), then refetched.
  // This is React's documented dev behavior; it vanishes in a prod build.
  it('DEV StrictMode double-mount is the source of the canceled+200 pair', async () => {
    const { client, detailCalls, aborted } = makeSlowClient(CONNECTOR_DETAIL, {
      name: 'organizations/local-corp/connectors/sample-api',
      displayName: 'Sample',
      http: { baseUrl: 'https://x' },
    });
    const $api = createReactQueryApi(client);
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
    });
    render(
      <StrictMode>
        <QueryClientProvider client={qc}>
          <ConnectorEditFeature
            $api={$api}
            apiClient={client}
            parent="organizations/local-corp"
            connectorId="sample-api"
            agentOptions={[]}
            back={<a href="/connectors">back</a>}
            onCancel={() => {}}
            onSubmitSuccess={() => {}}
          />
        </QueryClientProvider>
      </StrictMode>,
    );
    await waitFor(() => expect(detailCalls.length).toBeGreaterThan(0));
    await new Promise((r) => setTimeout(r, 90));
    // Two starts, the first aborted — exactly the dev Network-tab fingerprint.
    expect(detailCalls.length).toBe(2);
    expect(aborted.length).toBe(1);
  });
});

/**
 * The 2×2 truth table: {plain mount | <StrictMode>} × {COLD cache | WARM cache}.
 *
 * This block reconciles the user's discriminating observation — "the double
 * happens ONLY on items that were NOT previously loaded (cold); previously
 * loaded (warm) items do NOT double." The naive worry was that with a
 * `staleTime:0` detail query a warm entry is immediately stale, so
 * refetch-on-mount would refetch it and StrictMode would double the warm case
 * too. That worry is FALSE for Pivox: the detail hook sets NO `staleTime`, so it
 * inherits the app QueryClient default of 60s (router.tsx). A warm entry primed
 * <60s ago is FRESH, so refetch-on-mount does not fire at all — warm issues ZERO
 * detail fetches, on either the plain or the StrictMode path. Cold has no data,
 * so it fires the initial fetch (1 in prod); under StrictMode the strict-unmount
 * cancels that in-flight initial fetch and the strict-remount refetches → 2/1.
 *
 * WARM is seeded exactly as the SSR loader does it: `$api.queryOptions(...)` to
 * derive the byte-identical react-query key, then `setQueryData` under it — the
 * same call `$connectorId.edit.tsx` / `$secretId.edit.tsx` loaders make. This is
 * also how a warm entry arises on a CLIENT navigation: the loader early-returns
 * on the client, so a first edit-open fetches the record into cache; reopening
 * within the 60s window finds it fresh.
 */
type Cell = { detailCount: number; abortCount: number };

async function runConnectorCell(opts: { strict: boolean; warm: boolean }): Promise<Cell> {
  const record = {
    name: 'organizations/local-corp/connectors/sample-api',
    displayName: 'Sample',
    http: { baseUrl: 'https://x' },
  };
  const { client, detailCalls, aborted } = makeSlowClient(CONNECTOR_DETAIL, record);
  const $api = createReactQueryApi(client);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
  });
  if (opts.warm) {
    // Byte-identical key to the SSR loader's prime + the client hook's read.
    const { queryKey } = $api.queryOptions('get', CONNECTOR_DETAIL, {
      params: { path: { organization: 'local-corp', connector: 'sample-api' } },
    });
    qc.setQueryData(queryKey, record);
  }
  const tree = (
    <QueryClientProvider client={qc}>
      <ConnectorEditFeature
        $api={$api}
        apiClient={client}
        parent="organizations/local-corp"
        connectorId="sample-api"
        agentOptions={[]}
        back={<a href="/connectors">back</a>}
        onCancel={() => {}}
        onSubmitSuccess={() => {}}
      />
    </QueryClientProvider>
  );
  render(opts.strict ? <StrictMode>{tree}</StrictMode> : tree);
  // Fixed settle window (> 40ms in-flight + StrictMode remount). WARM fires no
  // detail fetch, so we cannot waitFor(detailCalls>0) — a fixed wait covers both.
  await new Promise((r) => setTimeout(r, 130));
  return { detailCount: detailCalls.length, abortCount: aborted.length };
}

async function runSecretCell(opts: { strict: boolean; warm: boolean }): Promise<Cell> {
  const record = {
    name: 'organizations/local-corp/secrets/stripe-key',
    displayName: 'Stripe key',
  };
  const { client, detailCalls, aborted } = makeSlowClient(SECRET_DETAIL, record);
  const $api = createReactQueryApi(client);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
  });
  if (opts.warm) {
    const { queryKey } = $api.queryOptions('get', SECRET_DETAIL, {
      params: { path: { organization: 'local-corp', secret: 'stripe-key' } },
    });
    qc.setQueryData(queryKey, record);
  }
  const tree = (
    <QueryClientProvider client={qc}>
      <SecretEditFeature
        $api={$api}
        apiClient={client}
        parent="organizations/local-corp"
        secretId="stripe-key"
        back={<a href="/secrets">back</a>}
        onCancel={() => {}}
        onSubmitSuccess={() => {}}
      />
    </QueryClientProvider>
  );
  render(opts.strict ? <StrictMode>{tree}</StrictMode> : tree);
  await new Promise((r) => setTimeout(r, 130));
  return { detailCount: detailCalls.length, abortCount: aborted.length };
}

describe('edit-form detail fetch: 2×2 truth table (plain|strict × cold|warm)', () => {
  it('connector — plain mount / COLD cache: 1 fetch, 0 aborts (the prod path)', async () => {
    expect(await runConnectorCell({ strict: false, warm: false })).toEqual({
      detailCount: 1,
      abortCount: 0,
    });
  });

  it('connector — plain mount / WARM cache: 0 fetches, 0 aborts (fresh, no refetch)', async () => {
    expect(await runConnectorCell({ strict: false, warm: true })).toEqual({
      detailCount: 0,
      abortCount: 0,
    });
  });

  it('connector — <StrictMode> / COLD cache: 2 fetches, 1 abort (dev double)', async () => {
    expect(await runConnectorCell({ strict: true, warm: false })).toEqual({
      detailCount: 2,
      abortCount: 1,
    });
  });

  it('connector — <StrictMode> / WARM cache: 0 fetches, 0 aborts (fresh — no double)', async () => {
    expect(await runConnectorCell({ strict: true, warm: true })).toEqual({
      detailCount: 0,
      abortCount: 0,
    });
  });

  it('secret — plain mount / COLD cache: 1 fetch, 0 aborts (the prod path)', async () => {
    expect(await runSecretCell({ strict: false, warm: false })).toEqual({
      detailCount: 1,
      abortCount: 0,
    });
  });

  it('secret — plain mount / WARM cache: 0 fetches, 0 aborts (fresh, no refetch)', async () => {
    expect(await runSecretCell({ strict: false, warm: true })).toEqual({
      detailCount: 0,
      abortCount: 0,
    });
  });

  it('secret — <StrictMode> / COLD cache: 2 fetches, 1 abort (dev double)', async () => {
    expect(await runSecretCell({ strict: true, warm: false })).toEqual({
      detailCount: 2,
      abortCount: 1,
    });
  });

  it('secret — <StrictMode> / WARM cache: 0 fetches, 0 aborts (fresh — no double)', async () => {
    expect(await runSecretCell({ strict: true, warm: true })).toEqual({
      detailCount: 0,
      abortCount: 0,
    });
  });
});
