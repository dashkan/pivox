// @vitest-environment jsdom
import { renderHook } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { ReactQueryApi } from '@pivox/client/react-query';

const setMock = vi.fn();
const getMock = vi.fn<() => string | null>(() => null);
vi.mock('@pivox/storage', () => ({
  ACTIVE_ORG: { name: 'pivox.active-organization' },
  storage: {
    get: (...a: unknown[]) => getMock(...(a as [])),
    set: (...a: unknown[]) => setMock(...(a as [])),
  },
}));
vi.mock('@/auth/use-auth', () => ({
  useAuth: () => ({ user: null, signOut: vi.fn() }),
}));

import { useAppShell } from '@/app-shell/use-app-shell';

const ORGS = {
  accountOrganizations: [
    { organization: 'organizations/acme', displayName: 'Acme' },
    { organization: 'organizations/globex', displayName: 'Globex' },
  ],
};

/** Fake $api: orgs query returns ORGS; spaces query returns empty. */
function makeApi(): ReactQueryApi {
  const useQuery = (
    _m: string,
    path: string,
    _init: unknown,
    options?: { enabled?: boolean },
  ) => {
    if (path === '/v1/accounts/me/organizations') {
      return { data: ORGS, isLoading: false, error: undefined };
    }
    if (options?.enabled === false) {
      return { data: undefined, isLoading: false, error: undefined };
    }
    return { data: { spaces: [] }, isLoading: false, error: undefined };
  };
  return { useQuery } as unknown as ReactQueryApi;
}

beforeEach(() => {
  setMock.mockClear();
  getMock.mockReset();
  getMock.mockReturnValue(null);
});

describe('useAppShell scope coherence with URL-scoped routes', () => {
  // Finding 2: on a flat (uncontrolled) route with no/stale cookie, the
  // orgs[0] default must seed local state IN PLACE — never route away, which
  // would strand the user (they'd never reach secrets/workflows).
  it('uncontrolled + no cookie: defaults to first org locally, does NOT navigate', () => {
    const onSelectOrganization = vi.fn();
    const { result } = renderHook(() =>
      useAppShell({
        $api: makeApi(),
        onCreateOrganization: vi.fn(),
        onSelectOrganization, // web injects this; it navigates
        // no activeOrganization prop => uncontrolled (cookie-driven)
      }),
    );
    expect(onSelectOrganization).not.toHaveBeenCalled();
    expect(setMock).toHaveBeenCalledWith(
      expect.anything(),
      'organizations/acme',
    );
    expect(result.current.state.activeOrganization).toBe('organizations/acme');
  });

  // Finding 1: on a controlled (URL-derived) org route, the last-visited hint
  // must track the URL so flat cookie-scoped routes and the root redirect see
  // the actual last org — not a stale earlier pick.
  it('controlled org: syncs the last-visited cookie to the URL org', () => {
    renderHook(() =>
      useAppShell({
        $api: makeApi(),
        onCreateOrganization: vi.fn(),
        activeOrganization: 'organizations/globex', // URL-derived
        activeSpace: null,
        onSelectOrganization: vi.fn(),
        onSelectSpace: vi.fn(),
      }),
    );
    expect(setMock).toHaveBeenCalledWith(
      expect.anything(),
      'organizations/globex',
    );
  });

  // Controlled mode must never run the orgs[0] default (the URL owns scope; an
  // unknown slug is a route-level notFound, not a silent fallback here).
  it('controlled org: never defaults via navigation', () => {
    const onSelectOrganization = vi.fn();
    renderHook(() =>
      useAppShell({
        $api: makeApi(),
        onCreateOrganization: vi.fn(),
        activeOrganization: 'organizations/acme',
        activeSpace: null,
        onSelectOrganization,
        onSelectSpace: vi.fn(),
      }),
    );
    expect(onSelectOrganization).not.toHaveBeenCalled();
  });
});
