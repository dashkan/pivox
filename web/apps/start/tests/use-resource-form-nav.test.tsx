// @vitest-environment jsdom
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';

import type { ReactNode } from 'react';

// `useResourceFormNav` depends on TanStack Router for navigation (`useRouter`)
// and the soft dirty-guard (`useBlocker`). Neither is under test here — we're
// asserting the react-query invalidation SHAPE — so stub the module. `vi.hoisted`
// keeps the push spy addressable from the hoisted `vi.mock` factory.
const { pushMock } = vi.hoisted(() => ({ pushMock: vi.fn() }));
vi.mock('@tanstack/react-router', () => ({
  useRouter: () => ({ history: { push: pushMock } }),
  useBlocker: () => {},
}));

import {
  useResourceFormNav,
  type ResourceFormNavConfig,
} from '../src/lib/use-resource-form-nav';

const config: ResourceFormNavConfig = {
  listRoute: '/connectors',
  listKeys: [
    ['get', '/v1/organizations/{organization}/connectors'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/connectors'],
  ],
  detailKeys: [
    ['get', '/v1/organizations/{organization}/connectors/{connector}'],
    ['get', '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}'],
  ],
  confirmMessage: 'Discard unsaved changes to this connector?',
};

function setup() {
  pushMock.mockClear();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const invalidateSpy = vi.spyOn(queryClient, 'invalidateQueries');
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => useResourceFormNav(undefined, config), {
    wrapper,
  });
  return { result, invalidateSpy };
}

describe('useResourceFormNav — goBackAndRefresh invalidation shape', () => {
  it('invalidates every list + detail family with refetchType "none"', () => {
    const { result, invalidateSpy } = setup();

    act(() => {
      result.current.goBackAndRefresh();
    });

    // One invalidate per list + detail family, and no others.
    expect(invalidateSpy).toHaveBeenCalledTimes(
      config.listKeys.length + config.detailKeys.length,
    );

    // CRITICAL: every invalidation must use refetchType 'none'. The default
    // ('active') eagerly refetches, which — for the list — races the fetch the
    // destination list route fires on mount, so react-query cancels one and the
    // Network tab shows a doubled (canceled + 200) request. 'none' marks the
    // families stale WITHOUT an eager refetch; the list refetches exactly once,
    // on mount, because it is now invalidated (and staleTime 0).
    for (const [args] of invalidateSpy.mock.calls) {
      expect(args).toMatchObject({ refetchType: 'none' });
    }

    // Behavior preserved: both list families are invalidated (so the saved row
    // shows on return) and both detail families (so a reopened edit refetches).
    for (const queryKey of [...config.listKeys, ...config.detailKeys]) {
      expect(invalidateSpy).toHaveBeenCalledWith(
        expect.objectContaining({ queryKey, refetchType: 'none' }),
      );
    }

    // Navigation back to the sanitized return target still fires.
    expect(pushMock).toHaveBeenCalledWith('/connectors');
  });

  it('plain goBack (cancel) invalidates nothing — nothing changed', () => {
    const { result, invalidateSpy } = setup();

    act(() => {
      result.current.goBack();
    });

    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(pushMock).toHaveBeenCalledWith('/connectors');
  });
});
