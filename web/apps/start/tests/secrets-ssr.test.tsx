// @vitest-environment jsdom
import { buildSecretsListRequest, SecretsFeature } from '@pivox/features/secrets';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { render, screen } from '@testing-library/react';
import { beforeAll, describe, expect, it, vi } from 'vitest';

import type { ApiClient } from '@pivox/client';
import type { ListControlsValue } from '@pivox/ui/resource-admin';

import { $api } from '../src/lib/api-client';
import { searchToValue } from '../src/lib/secrets-search';

// Radix Select (page-size) measures the DOM; jsdom needs a ResizeObserver shim.
beforeAll(() => {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  };
});

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;

// The spaces query would hit the network; stub it out.
const apiClient = {
  GET: vi.fn(async () => ({ data: { spaces: [] } })),
  POST: vi.fn(),
  PATCH: vi.fn(),
  DELETE: vi.fn(),
} as unknown as ApiClient;

/**
 * Proves the SSR contract end-to-end WITHOUT the live stack: prime the
 * QueryClient exactly as the route loader does — same `buildSecretsListRequest`
 * and `$api.queryOptions` key — then render the feature and assert the row is in
 * the output. If the key drifted, the hook would miss the cache and render empty.
 */
describe('secrets SSR priming', () => {
  it('renders rows from the loader-primed cache (query-key parity)', () => {
    const listState: ListControlsValue = searchToValue({});
    const req = buildSecretsListRequest('acme', listState);

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const { queryKey } = $api.queryOptions('get', SECRETS_PATH, {
      params: { path: { organization: 'acme' }, query: req.query },
    });
    queryClient.setQueryData(queryKey, {
      secrets: [
        {
          name: 'organizations/acme/secrets/stripe-key',
          displayName: 'Stripe Key',
          createTime: '2026-01-01T00:00:00Z',
          updateTime: '2026-02-01T00:00:00Z',
        },
      ],
    });

    render(
      <QueryClientProvider client={queryClient}>
        <SecretsFeature
          $api={$api}
          apiClient={apiClient}
          parent="organizations/acme"
          listState={listState}
          onListStateChange={() => {}}
          onCreate={() => {}}
          onEdit={() => {}}
        />
      </QueryClientProvider>,
    );

    // The primed row renders synchronously — this is what lands in SSR HTML.
    expect(screen.getByText('Stripe Key')).toBeDefined();
  });
});
