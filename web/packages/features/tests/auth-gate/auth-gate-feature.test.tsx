// @vitest-environment jsdom
import { render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { AuthGateFeature } from '@/auth-gate/auth-gate-feature';
import { useAuth } from '@/auth/use-auth';

import type { AuthContextValue, AuthUser } from '@/auth/use-auth';

// The gate reads auth from context via useAuth; mock it so each case
// controls (loading, user) directly without standing up a provider.
vi.mock('@/auth/use-auth', () => ({ useAuth: vi.fn() }));

const mockUseAuth = vi.mocked(useAuth);

const user: AuthUser = {
  id: 'kc-sub',
  email: 'a@example.com',
  displayName: 'A',
  photoURL: null,
};

function authState(over: Partial<AuthContextValue>): AuthContextValue {
  return { user: null, loading: false, signOut: vi.fn(), ...over };
}

describe('AuthGateFeature', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('redirects to /auth/login via the injected Navigate when there is no user', () => {
    mockUseAuth.mockReturnValue(authState({ user: null, loading: false }));
    const Navigate = vi.fn(() => null);

    render(
      <AuthGateFeature Navigate={Navigate}>
        <div>protected</div>
      </AuthGateFeature>,
    );

    expect(Navigate).toHaveBeenCalledTimes(1);
    expect(Navigate.mock.calls[0][0]).toEqual({
      to: '/auth/login',
      replace: true,
    });
    expect(screen.queryByText('protected')).toBeNull();
  });

  it('renders children and does not navigate when a user is present', () => {
    mockUseAuth.mockReturnValue(authState({ user, loading: false }));
    const Navigate = vi.fn(() => null);

    render(
      <AuthGateFeature Navigate={Navigate}>
        <div>protected</div>
      </AuthGateFeature>,
    );

    expect(Navigate).not.toHaveBeenCalled();
    expect(screen.queryByText('protected')).not.toBeNull();
  });

  it('renders the loading splash and does not navigate while auth is settling', () => {
    mockUseAuth.mockReturnValue(authState({ user: null, loading: true }));
    const Navigate = vi.fn(() => null);

    render(
      <AuthGateFeature Navigate={Navigate}>
        <div>protected</div>
      </AuthGateFeature>,
    );

    expect(Navigate).not.toHaveBeenCalled();
    expect(screen.queryByText('protected')).toBeNull();
    expect(screen.queryByText('Loading…')).not.toBeNull();
  });
});
