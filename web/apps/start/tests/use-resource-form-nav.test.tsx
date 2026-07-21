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
  const removeSpy = vi.spyOn(queryClient, 'removeQueries');
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
  const { result } = renderHook(() => useResourceFormNav(undefined, config), {
    wrapper,
  });
  return { result, invalidateSpy, removeSpy };
}

describe('useResourceFormNav — goBackAndRefresh cache handling', () => {
  it('invalidates each LIST family (refetchType "none") and REMOVES each DETAIL family', () => {
    const { result, invalidateSpy, removeSpy } = setup();

    act(() => {
      result.current.goBackAndRefresh();
    });

    // LIST: one invalidate per list family, none others. `refetchType: 'none'`
    // marks them stale WITHOUT an eager refetch — the default ('active') would
    // race the destination list route's own on-mount fetch, and react-query
    // cancels one, surfacing a doubled (canceled + 200) request. 'none' lets the
    // list refetch exactly once, on mount, because it is invalidated + stale.
    expect(invalidateSpy).toHaveBeenCalledTimes(config.listKeys.length);
    for (const queryKey of config.listKeys) {
      expect(invalidateSpy).toHaveBeenCalledWith(
        expect.objectContaining({ queryKey, refetchType: 'none' }),
      );
    }

    // DETAIL: one remove per detail family. Invalidate is NOT enough — on reopen
    // the stale record is served synchronously and the form provider seeds its
    // inputs once (keyed on record.name), so a same-identity background refetch
    // never re-seeds. Removing forces a cold reopen that re-seeds from fresh data
    // (regression: "edit form shows pre-edit values after save").
    expect(removeSpy).toHaveBeenCalledTimes(config.detailKeys.length);
    for (const queryKey of config.detailKeys) {
      expect(removeSpy).toHaveBeenCalledWith(
        expect.objectContaining({ queryKey }),
      );
    }
    // Detail families are removed, never invalidated.
    for (const queryKey of config.detailKeys) {
      expect(invalidateSpy).not.toHaveBeenCalledWith(
        expect.objectContaining({ queryKey }),
      );
    }

    // Navigation back to the sanitized return target still fires.
    expect(pushMock).toHaveBeenCalledWith('/connectors');
  });

  it('plain goBack (cancel) touches no cache — nothing changed', () => {
    const { result, invalidateSpy, removeSpy } = setup();

    act(() => {
      result.current.goBack();
    });

    expect(invalidateSpy).not.toHaveBeenCalled();
    expect(removeSpy).not.toHaveBeenCalled();
    expect(pushMock).toHaveBeenCalledWith('/connectors');
  });
});
