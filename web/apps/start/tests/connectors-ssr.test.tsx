// @vitest-environment jsdom
import { buildConnectorsListRequest } from '@pivox/features/connectors';
import { ConnectorsFeature } from '@pivox/features/connectors';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { ApiClient } from '@pivox/client';
import type { ListControlsValue } from '@pivox/ui/resource-admin';

import { $api } from '../src/lib/api-client';
import { searchToValue } from '../src/lib/connectors-search';

// Radix Select (page-size) measures the DOM; jsdom needs a ResizeObserver shim.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;

// The agents fan-out + spaces query would hit the network; stub them out.
const apiClient = {
  GET: vi.fn(async () => ({ data: { storageGateways: [] } })),
  POST: vi.fn(),
  PATCH: vi.fn(),
  DELETE: vi.fn(),
} as unknown as ApiClient;

/**
 * Proves the SSR contract end-to-end WITHOUT the live stack: prime the
 * QueryClient exactly as the route loader does — same `buildConnectorsListRequest`
 * and `$api.queryOptions` key — then render the feature and assert the row is in
 * the output. If the key drifted, the hook would miss the cache and render empty.
 */
describe('connectors SSR priming', () => {
  it('renders rows from the loader-primed cache (query-key parity)', () => {
    const listState: ListControlsValue = searchToValue({});
    const req = buildConnectorsListRequest('acme', listState);

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    // Exactly the loader's priming for the org rollup.
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
        />
      </QueryClientProvider>,
    );

    // The primed row renders synchronously — this is what lands in SSR HTML.
    expect(screen.getByText('Stripe Payments')).toBeDefined();
    // Agent options are injected (SSR-prefetched at the route); the feature no
    // longer fans out gateways/agents itself.
    expect(apiClient.GET).not.toHaveBeenCalled();
  });
});
