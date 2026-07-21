// @vitest-environment jsdom
import { buildConnectorsListRequest } from '@pivox/features/connectors';
import { ConnectorsFeature } from '@pivox/features/connectors';
import { DEFAULT_PAGE_SIZE } from '@pivox/ui/resource-admin';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it } from 'vitest';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

// The renderer's OWN data layer — the exact `$api`/`apiClient` instances the
// packaged app uses. Importing them here is the load-bearing part of the proof:
// the shared `@pivox/features` list hooks are DI'd on these, and this test
// exercises Electron's real injected instances, not test doubles.
import { $api, apiClient } from '../../src/renderer/lib/api-client';

// Radix Select (the page-size picker) measures the DOM; jsdom needs a
// ResizeObserver shim (same as the start app's SSR mount test).
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;

/**
 * VALIDATION SPIKE — proves the shared resource-admin List half mounts and
 * renders in the ELECTRON renderer with only per-app wiring:
 *
 *   - Electron's own `$api` / `apiClient` (from `@renderer/lib/api-client`,
 *     backed by the main-process OIDC/IPC bearer) are injected — no shared-code
 *     edit, no SSR, no BFF.
 *   - Navigation is injected as plain callbacks (`onCreate`/`onEdit`), so the
 *     component renders with NO TanStack Router mounted — the clearest possible
 *     demonstration that the abstraction is router-agnostic.
 *   - List-controls state is supplied as a plain controlled value; a real route
 *     would source it from local state or the router's search params.
 *
 * If the abstraction secretly needed SSR, the start app's router, or a start-only
 * import, this mount would throw. It renders the primed row instead.
 */
describe('shared ResourceList mounts in the Electron renderer', () => {
  it('renders connector rows via injected $api/apiClient, no router', () => {
    const listState: ListControlsValue = {
      filters: {},
      sort: null,
      pageSize: DEFAULT_PAGE_SIZE,
      scope: '',
      pageToken: undefined,
    };
    const req = buildConnectorsListRequest('acme', listState);

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    // Prime the cache under the descriptor's exact react-query key so the row is
    // present synchronously — the mount never needs the network (which, in a
    // renderer test, has no main-process IPC bearer behind it).
    const { queryKey } = $api.queryOptions('get', CONNECTORS_PATH, {
      params: { path: { organization: 'acme' }, query: req.query },
    });
    queryClient.setQueryData(queryKey, {
      connectors: [
        {
          name: 'organizations/acme/connectors/stripe',
          displayName: 'Stripe Payments',
          http: { baseUrl: 'https://api.stripe.com' },
          updateTime: '2026-02-01T00:00:00Z',
        },
      ],
    });

    render(
      <QueryClientProvider client={queryClient}>
        <ConnectorsFeature
          $api={$api}
          apiClient={apiClient}
          parent="organizations/acme"
          listState={listState}
          onListStateChange={() => {}}
          agentOptions={[]}
          onCreate={() => {}}
          onEdit={() => {}}
        />
      </QueryClientProvider>,
    );

    expect(screen.getByText('Stripe Payments')).toBeDefined();
  });
});
