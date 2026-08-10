// @vitest-environment jsdom
import { createReactQueryApi } from '@pivox/client/react-query';
import { ConnectorEditFeature } from '@pivox/features/connectors';
import { SecretEditFeature } from '@pivox/features/secrets';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, render, renderHook, screen, waitFor } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { ApiClient } from '@pivox/client';
import type { ReactNode } from 'react';

// `useResourceFormNav` uses TanStack Router only for navigation + the soft dirty
// guard; neither is under test. Stub them so the hook runs headless — we exercise
// its react-query cache handling (the fix) against the real edit features.
const { pushMock } = vi.hoisted(() => ({ pushMock: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  useRouter: () => ({ history: { push: pushMock } }),
  useBlocker: () => {},
}));

import {
  useResourceFormNav,
  type ResourceFormNavConfig,
} from '../src/lib/use-resource-form-nav';

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

/** GET returns the CURRENT server record on the detail path; empty elsewhere. */
function makeClient(detailPath: string, record: () => unknown) {
  const GET = vi.fn((path: string) =>
    Promise.resolve({
      data: path === detailPath ? record() : { spaces: [] },
      error: undefined,
      response: { status: 200, headers: new Headers() },
    }),
  );
  return {
    GET,
    POST: vi.fn(),
    PATCH: vi.fn(),
    DELETE: vi.fn(),
  } as unknown as ApiClient;
}

/**
 * The bug this guards: after saving an edit, reopening the SAME item showed the
 * PRE-EDIT (stale) values in the form, even though the list reflected the change.
 * Root cause was two-fold — the detail query was only INVALIDATED (so the stale
 * record was still served synchronously on reopen) and the form provider seeds
 * its inputs once, keyed on record.name (unchanged across a same-identity
 * refetch), so the background-refetched fresh data never re-seeded the inputs.
 * The fix removes the detail cache on save, forcing a cold reopen that re-seeds.
 *
 * These tests drive the REAL `useResourceFormNav.goBackAndRefresh` between the
 * two edit-opens, so they exercise the actual production cache handling.
 */
async function assertReopenShowsFresh(opts: {
  detailPath: string;
  config: ResourceFormNavConfig;
  seedKey: (api: ReturnType<typeof createReactQueryApi>) => readonly unknown[];
  stale: Record<string, unknown>;
  fresh: Record<string, unknown>;
  renderEdit: (api: ReturnType<typeof createReactQueryApi>, client: ApiClient) => ReactNode;
}) {
  let current: unknown = opts.fresh;
  const client = makeClient(opts.detailPath, () => current);
  const $api = createReactQueryApi(client);
  // 60s staleTime mirrors the app QueryClient default (router.tsx): a warm entry
  // is FRESH, so without the fix the reopen never refetches and never re-seeds.
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 60_000 } },
  });

  // A prior edit-open warmed the detail cache with the pre-edit record.
  qc.setQueryData(opts.seedKey($api), opts.stale);

  // Drive the real hook's save-success handler (removes the detail cache).
  const { result } = renderHook(() => useResourceFormNav(undefined, opts.config), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  act(() => {
    result.current.goBackAndRefresh();
  });

  // Reopen the edit form; the server now returns the fresh record.
  render(
    <QueryClientProvider client={qc}>{opts.renderEdit($api, client)}</QueryClientProvider>,
  );

  await waitFor(() => expect(client.GET).toHaveBeenCalled());
  await waitFor(() =>
    expect(screen.getByDisplayValue('New name')).toHaveProperty('value', 'New name'),
  );
  expect(screen.queryByDisplayValue('Old name')).toBeNull();
}

const SECRET_CONFIG: ResourceFormNavConfig = {
  listRoute: '/secrets',
  listKeys: [
    ['get', '/v1/organizations/{organization}/secrets'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/secrets'],
  ],
  detailKeys: [
    ['get', SECRET_DETAIL],
    ['get', '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}'],
  ],
  confirmMessage: 'Discard unsaved changes to this secret?',
};

const CONNECTOR_CONFIG: ResourceFormNavConfig = {
  listRoute: '/connectors',
  listKeys: [
    ['get', '/v1/organizations/{organization}/connectors'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/connectors'],
  ],
  detailKeys: [
    ['get', CONNECTOR_DETAIL],
    ['get', '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}'],
  ],
  confirmMessage: 'Discard unsaved changes to this connector?',
};

describe('reopen edit after save shows fresh values (not the stale seed)', () => {
  it('secret edit-open re-seeds from fresh data after goBackAndRefresh', async () => {
    await assertReopenShowsFresh({
      detailPath: SECRET_DETAIL,
      config: SECRET_CONFIG,
      seedKey: ($api) =>
        $api.queryOptions('get', SECRET_DETAIL, {
          params: { path: { organization: 'local-corp', secret: 'stripe-key' } },
        }).queryKey,
      stale: {
        name: 'organizations/local-corp/secrets/stripe-key',
        displayName: 'Old name',
        etag: 'e1',
      },
      fresh: {
        name: 'organizations/local-corp/secrets/stripe-key',
        displayName: 'New name',
        etag: 'e2',
      },
      renderEdit: ($api, client) => (
        <SecretEditFeature
          $api={$api}
          apiClient={client}
          parent="organizations/local-corp"
          secretId="stripe-key"
          back={<a href="/secrets">back</a>}
          onCancel={() => {}}
          onSubmitSuccess={() => {}}
        />
      ),
    });
  });

  it('connector edit-open re-seeds from fresh data after goBackAndRefresh', async () => {
    await assertReopenShowsFresh({
      detailPath: CONNECTOR_DETAIL,
      config: CONNECTOR_CONFIG,
      seedKey: ($api) =>
        $api.queryOptions('get', CONNECTOR_DETAIL, {
          params: { path: { organization: 'local-corp', connector: 'sample-api' } },
        }).queryKey,
      stale: {
        name: 'organizations/local-corp/connectors/sample-api',
        displayName: 'Old name',
        http: { baseUrl: 'https://x' },
        etag: 'e1',
      },
      fresh: {
        name: 'organizations/local-corp/connectors/sample-api',
        displayName: 'New name',
        http: { baseUrl: 'https://x' },
        etag: 'e2',
      },
      renderEdit: ($api, client) => (
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
      ),
    });
  });
});
