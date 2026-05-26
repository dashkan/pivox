import { useAuth } from '@pivox/features/auth';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useEffect } from 'react';

import { createSession } from '@/server/auth-session';

/**
 * Silent-recovery interstitial. Reached only via redirect from
 * `_app`'s `beforeLoad` when the session cookie is present but
 * invalid (expired or revoked) — see `apps/start/src/server/auth-
 * session.ts` for the cookie-state matrix.
 *
 * The page tries to re-mint a session cookie from whatever the
 * Firebase JS SDK still has in IndexedDB. Three outcomes:
 *
 *   - Firebase JS SDK still holds a valid refresh token (the
 *     overwhelming case for cookie-just-expired) → `getIdToken(true)`
 *     succeeds → `createSession` mints a fresh cookie → navigate to
 *     the original target.
 *   - Refresh token was revoked (sign-out elsewhere, password change,
 *     account disabled) → `getIdToken` throws → fall through to
 *     `/auth/login`.
 *   - SDK reports no user at all (rare — IndexedDB cleared / private
 *     mode) → straight to `/auth/login`.
 *
 * UX is intentionally minimal: a "Verifying session…" line, no brand
 * chrome, no spinner that would imply something is wrong. The
 * interstitial is meant to be invisible-fast in the happy case
 * (200–400ms) and a clean transition in the failure case.
 *
 * No `beforeLoad` here on purpose — this route is the recovery path;
 * it must reach the client to ask Firebase JS what it knows.
 */
type VerifySessionSearch = {
  return?: string;
};

export const Route = createFileRoute('/auth/verify-session')({
  validateSearch: (search: Record<string, unknown>): VerifySessionSearch => {
    // Path-relative + reject `//host/path` (protocol-relative URL —
    // would be an open redirect). Same shape as /auth/login.
    if (
      typeof search.return === 'string' &&
      search.return.startsWith('/') &&
      !search.return.startsWith('//')
    ) {
      return { return: search.return };
    }
    return {};
  },
  component: VerifySessionPage,
});

function VerifySessionPage() {
  const { user, loading } = useAuth();
  const navigate = useNavigate({ from: '/auth/verify-session' });
  const { return: returnUrl } = Route.useSearch();

  useEffect(() => {
    if (loading) return;
    if (!user) {
      // Firebase JS SDK has no user at all — nothing to recover from.
      // Fall through to login (carry the return target so a successful
      // re-auth lands back at the original page).
      void navigate({
        to: '/auth/login',
        search: returnUrl ? { return: returnUrl } : {},
        replace: true,
      });
      return;
    }
    // No cancellation guard — Strict Mode's dev-only double-mount
    // can fire this IIFE twice, but every operation in it is
    // idempotent: two `createSession` calls produce the same cookie,
    // two `navigate` calls land at the same URL. Net cost in dev: one
    // wasted createSession round-trip per mount. In production
    // (where Strict Mode doesn't double-fire) the effect runs once.
    // Guarding with a `cancelled` flag tripped eslint's
    // no-unnecessary-condition rule (the cleanup function's flag
    // mutation isn't visible to flow analysis), and the suppression
    // wasn't worth the protection.
    void (async () => {
      try {
        // `forceRefresh: true` so we get a brand-new ID token rather
        // than a cached one — the cached one could itself be stale
        // depending on the SDK's refresh schedule.
        const idToken = await user.getIdToken(true);
        await createSession({ data: { idToken } });
        await navigate({ to: returnUrl ?? '/', replace: true });
      } catch {
        // Refresh token revoked / network failure — defer to the real
        // login flow rather than retry-looping here.
        await navigate({
          to: '/auth/login',
          search: returnUrl ? { return: returnUrl } : {},
          replace: true,
        });
      }
    })();
  }, [user, loading, navigate, returnUrl]);

  return (
    <div className="flex min-h-screen items-center justify-center p-6">
      <p className="text-sm text-muted-foreground">Verifying session…</p>
    </div>
  );
}
