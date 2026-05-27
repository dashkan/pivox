/**
 * Server-side reads for storage items — the values that SSR needs to
 * know about so it can render with the user's actual preferences on
 * first paint (no flicker on hydration).
 *
 * Wrapped in `createServerFn` rather than directly importing
 * `getCookie` into route files because routes are shared client/SSR
 * code and TanStack Start's import-protection blocks
 * `@tanstack/react-start/server` from anything client-reachable.
 * The server fn keeps the import surface server-only; the client
 * sees an RPC stub.
 *
 * Item descriptors come from @pivox/storage — same source of truth
 * the client uses, so server reads + client writes can't drift.
 */

import { LAST_EMAIL, THEME, type Theme } from '@pivox/storage';
import { createServerFn } from '@tanstack/react-start';
import { getCookie } from '@tanstack/react-start/server';

export type { Theme };

/**
 * getThemeCookie server-fn: returns the SSR-time value of the user's
 * theme preference, or null if absent / malformed.
 *
 * Used by `_app.tsx`'s beforeLoad to thread initialTheme into the
 * ThemeSwitcher's SSR snapshot. Without this, the switcher would
 * default to 'system' on the server render and flicker to the saved
 * value once the client reads its own cookie copy on hydration.
 */
export const getThemeCookie = createServerFn({ method: 'GET' }).handler(
  (): Theme | null => {
    try {
      const v = getCookie(THEME.name);
      return v ? THEME.parse(v) : null;
    } catch {
      // h3 getCookie shouldn't throw, but never fail beforeLoad over
      // a preference read — degrade to default.
      return null;
    }
  },
);

/**
 * getLastEmailCookie server-fn: returns the SSR-time value of the
 * remembered auto-fill email, or null if absent.
 *
 * Used by the login route's beforeLoad to thread initialEmail into
 * the LoginCard. Without this, the email field would flicker from
 * empty → filled when client hydration reads the cookie/localStorage.
 *
 * h3's getCookie auto-decodes URL-encoded values, so the helper
 * returns the original email string — `@` characters and the like
 * are restored before the value reaches the route.
 */
export const getLastEmailCookie = createServerFn({ method: 'GET' }).handler(
  (): string | null => {
    try {
      const v = getCookie(LAST_EMAIL.name);
      return v ? LAST_EMAIL.parse(v) : null;
    } catch {
      return null;
    }
  },
);
