// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { useQuery } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { AgentOption } from '@pivox/ui/resource-admin';
import type { ReactNode } from 'react';

import { connectorAgentsQueryKey } from '../src/lib/connector-agents-query';

const AGENTS_STALE_TIME = 5 * 60 * 1000;

const agentOptions: AgentOption[] = [
  { value: 'organizations/acme/storageGateways/gw/agents/a1', label: 'edge-01' },
];

/**
 * Mirrors the connectors route's composite agent-options query. Proves the
 * loader-primed key is read on load (no gateways/agents fan-out fires) — the
 * SSR-prefetch contract for removing the last client-side call.
 */
describe('connector agents composite query (SSR priming)', () => {
  it('serves the loader-primed options without calling the fan-out queryFn', async () => {
    const orgSlug = 'acme';
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    // Exactly the loader's priming.
    queryClient.setQueryData(connectorAgentsQueryKey(orgSlug), agentOptions);

    const queryFn = vi.fn(() => Promise.resolve<AgentOption[]>([]));
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { result } = renderHook(
      () =>
        useQuery({
          queryKey: connectorAgentsQueryKey(orgSlug),
          queryFn,
          enabled: Boolean(orgSlug),
          staleTime: AGENTS_STALE_TIME,
        }),
      { wrapper },
    );

    // Primed data is present immediately and the fan-out never runs.
    expect(result.current.data).toEqual(agentOptions);
    await waitFor(() => expect(result.current.isFetching).toBe(false));
    expect(queryFn).not.toHaveBeenCalled();
  });

  it('uses a stable key shared by loader and client (parity)', () => {
    expect(connectorAgentsQueryKey('acme')).toEqual(['connector-agents', 'acme']);
    // Same inputs → deep-equal keys (no drift between loader and client).
    expect(connectorAgentsQueryKey('acme')).toEqual(
      connectorAgentsQueryKey('acme'),
    );
  });
});
