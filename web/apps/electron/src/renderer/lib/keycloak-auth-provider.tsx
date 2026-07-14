import { AuthContext, type AuthContextValue, type AuthUser } from '@pivox/features/auth';
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';

/**
 * Electron's Keycloak auth provider — the renderer half of the auth system.
 *
 * All OIDC, token storage, and refresh live in the main process; this provider
 * is a thin bridge: it reads the auth state over IPC, re-reads on every
 * `auth:changed` event (login / logout / boot restore / token refresh), and maps
 * the result into the platform-neutral `AuthContext` that the app shell, route
 * gate, and `useUserId` consume. Mirrors the web app's KeycloakAuthProvider,
 * but sourced from IPC instead of an SSR-resolved session — so unlike the web
 * app it has a real `loading` phase while the main process restores on boot.
 */

type IpcAuthUser = {
  id: string;
  email?: string;
  displayName?: string;
  photoURL?: string;
};

/** Map the IPC user (optional fields) to the shared AuthUser (nullable fields). */
function toAuthUser(user: IpcAuthUser | null): AuthUser | null {
  if (!user) return null;
  return {
    id: user.id,
    email: user.email ?? null,
    displayName: user.displayName ?? null,
    photoURL: user.photoURL ?? null,
  };
}

export function KeycloakAuthProvider({
  onBeforeSignOut,
  children,
}: {
  /** Runs before the network logout — clears user-scoped caches. */
  onBeforeSignOut?: () => void;
  children: ReactNode;
}) {
  const [state, setState] = useState<{ loading: boolean; user: AuthUser | null }>({
    loading: true,
    user: null,
  });

  // Latest onBeforeSignOut without making signOut depend on it (use-latest).
  // Assigned in an effect (not during render) so the ref update is a commit-phase
  // side effect.
  const onBeforeSignOutRef = useRef(onBeforeSignOut);
  useEffect(() => {
    onBeforeSignOutRef.current = onBeforeSignOut;
  }, [onBeforeSignOut]);

  useEffect(() => {
    let active = true;
    // Monotonic request id: getAuthState reads race (initial read + a burst of
    // auth:changed events), and IPC responses can resolve out of order. Apply
    // only the latest so a slow stale read can't clobber fresh state (e.g.
    // bounce an authenticated user back to /auth/login).
    let seq = 0;
    const sync = async (): Promise<void> => {
      const mine = ++seq;
      const next = await window.api.getAuthState();
      if (active && mine === seq) {
        setState({ loading: !next.ready, user: toAuthUser(next.user) });
      }
    };
    void sync();
    // Re-read on every main-process auth transition (login / logout / boot
    // restore complete / refresh-failure sign-out).
    const unsubscribe = window.api.onAuthChanged(() => {
      void sync();
    });
    return () => {
      active = false;
      unsubscribe();
    };
  }, []);

  const signOut = useCallback(async (): Promise<void> => {
    onBeforeSignOutRef.current?.();
    await window.api.logout();
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({ user: state.user, loading: state.loading, signOut }),
    [state.user, state.loading, signOut],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}
