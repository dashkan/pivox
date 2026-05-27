/**
 * Server-side reads for user-preference cookies — the values that
 * SSR needs to know about so it can render with the user's actual
 * preferences on first paint (no flicker on hydration).
 *
 * Wrapped in `createServerFn` rather than directly importing
 * `getCookie` into route files because routes are shared client/SSR
 * code and TanStack Start's import-protection blocks
 * `@tanstack/react-start/server` from anything client-reachable.
 * The server fn keeps the import surface server-only; the client
 * sees an RPC stub.
 */

import { THEME_COOKIE } from '@pivox/client';
import { createServerFn } from '@tanstack/react-start';
import { getCookie } from '@tanstack/react-start/server';

export type Theme = 'light' | 'dark' | 'system';

function isTheme(v: string | null | undefined): v is Theme {
  return v === 'light' || v === 'dark' || v === 'system';
}

/**
 * getThemeCookie server-fn: returns the SSR-time value of the user's
 * theme preference, or null if absent / malformed.
 *
 * Used by `_app.tsx`'s beforeLoad to thread initialTheme into the
 * ThemeSwitcher's SSR snapshot. Without this, the switcher would
 * default to 'system' on the server render and flicker to the saved
 * value once the client reads its own cookie copy on hydration.
 */
export const getThemeCookie = createServerFn({ method: 'GET' })
  .handler(async (): Promise<Theme | null> => {
    try {
      const v = getCookie(THEME_COOKIE);
      return isTheme(v) ? v : null;
    } catch {
      // h3 getCookie shouldn't throw, but never fail beforeLoad over
      // a preference read — degrade to 'system' default.
      return null;
    }
  });
