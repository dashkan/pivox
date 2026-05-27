'use client';

import { THEME_COOKIE } from '@pivox/client';
import { Button } from '@pivox/primitives/button';
import { cn } from '@pivox/primitives/utils';
import { useEffect, useSyncExternalStore } from 'react';

type Theme = 'light' | 'system' | 'dark';

/**
 * localStorage mirror of the theme preference.
 *
 * Two-store strategy:
 *   - Cookie (`pivox.theme`) — readable by SSR (used by the start
 *     app's `_app` beforeLoad to seed initial state and by the
 *     pre-hydration inline script in `__root.tsx` to apply the dark
 *     class before paint).
 *   - localStorage (`pivox-theme`) — durable fallback. Cookies on
 *     `file://` origins (electron production) don't persist
 *     reliably across sessions; localStorage does.
 *
 * Both stores are written on every change. Reads consult the cookie
 * first (fastest + SSR-friendly path), then fall back to localStorage.
 * On cookie-empty + localStorage-present (cold load on electron OR a
 * pre-cookie-version user), the value is promoted to the cookie for
 * next time WITHOUT removing it from localStorage — keeping electron
 * users' preference intact even if the cookie write evaporates.
 */
const STORAGE_KEY = 'pivox-theme';

// Custom in-tab event for same-tab cookie writes — neither the native
// `storage` event nor any cookie-change event fires for same-tab writes,
// so we notify subscribers in the same tab via a synthetic event.
const THEME_EVENT = 'pivox-theme-change';

const themes: Array<Theme> = ['light', 'system', 'dark'];

const ONE_YEAR_SECONDS = 60 * 60 * 24 * 365;

function isTheme(v: string | null | undefined): v is Theme {
  return v === 'light' || v === 'dark' || v === 'system';
}

function readThemeCookie(): Theme | null {
  if (typeof document === 'undefined') return null;
  const prefix = `${THEME_COOKIE}=`;
  for (const part of document.cookie.split('; ')) {
    if (part.startsWith(prefix)) {
      const v = part.slice(prefix.length);
      return isTheme(v) ? v : null;
    }
  }
  return null;
}

function writeThemeCookie(value: Theme): void {
  if (typeof document === 'undefined') return;
  const secure =
    typeof location !== 'undefined' && location.protocol === 'https:';
  document.cookie =
    `${THEME_COOKIE}=${value};` +
    ` path=/;` +
    ` max-age=${String(ONE_YEAR_SECONDS)};` +
    ` samesite=lax` +
    (secure ? `; secure` : '');
}

function readLocalStorageTheme(): Theme | null {
  if (typeof window === 'undefined') return null;
  try {
    const v = window.localStorage.getItem(STORAGE_KEY);
    return isTheme(v) ? v : null;
  } catch {
    // localStorage can throw under sandbox / private-mode restrictions.
    return null;
  }
}

function writeLocalStorageTheme(value: Theme): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(STORAGE_KEY, value);
  } catch {
    // Quota / disabled storage — silent. Cookie still holds the value
    // for this session.
  }
}

function getStoredTheme(): Theme {
  // Cookie first: SSR-readable, also what the pre-hydration inline
  // script reads, so this matches what's already on the page.
  const cookie = readThemeCookie();
  if (cookie) return cookie;
  // Fall back to localStorage. Two cases land here:
  //   1. Electron on file://, where the cookie write may have
  //      silently failed and localStorage is the durable store.
  //   2. Pre-cookie user on first post-upgrade load.
  // In both cases, promote the value to the cookie for SSR + faster
  // future reads — but DON'T remove from localStorage so electron
  // (and any other cookie-unfriendly origin) keeps a working backup.
  const local = readLocalStorageTheme();
  if (local) {
    writeThemeCookie(local);
    return local;
  }
  return 'system';
}

function getSystemPreference(): 'light' | 'dark' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

function applyTheme(theme: Theme) {
  const resolved = theme === 'system' ? getSystemPreference() : theme;
  document.documentElement.classList.toggle('dark', resolved === 'dark');
}

function subscribeToTheme(onStoreChange: () => void) {
  // `storage` event fires for OTHER tabs only — useful for the
  // legacy-migration transition where one tab might still be on the
  // old localStorage path. Once that's removed the storage listener
  // becomes vestigial; leave it for now.
  window.addEventListener('storage', onStoreChange);
  window.addEventListener(THEME_EVENT, onStoreChange);
  return () => {
    window.removeEventListener('storage', onStoreChange);
    window.removeEventListener(THEME_EVENT, onStoreChange);
  };
}

export function ThemeSwitcher({
  className,
  initialTheme,
}: {
  className?: string;
  /**
   * SSR-resolved theme from the `pivox.theme` cookie. Threaded by
   * the route so useSyncExternalStore's server snapshot returns the
   * user's actual preference, not the default `'system'` — without
   * this the icon would flicker from `system` to the saved value on
   * hydration.
   *
   * Optional because non-SSR consumers (electron) don't supply one;
   * `'system'` is the right default for them.
   */
  initialTheme?: Theme;
}) {
  // useSyncExternalStore avoids both the SSR hydration mismatch AND the
  // setState-in-effect cascade the manual useEffect dance triggers.
  // The server snapshot returns the SSR-resolved cookie value so
  // first-paint HTML matches what the client will render on hydration.
  const theme = useSyncExternalStore<Theme>(
    subscribeToTheme,
    getStoredTheme,
    () => initialTheme ?? 'system',
  );

  // Apply theme to the document whenever it changes.
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  // Re-apply when system preference changes and we're in 'system' mode.
  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = () => {
      if (getStoredTheme() === 'system') applyTheme('system');
    };
    mql.addEventListener('change', handler);
    return () => {
      mql.removeEventListener('change', handler);
    };
  }, []);

  const setTheme = (next: Theme) => {
    // Write both stores. Cookie is the SSR-readable + cross-tab
    // path; localStorage is the durable fallback that survives
    // file:// origins (electron production).
    writeThemeCookie(next);
    writeLocalStorageTheme(next);
    // No cookie-change event fires for same-tab writes — notify our own
    // subscribers via a synthetic event so useSyncExternalStore picks it up.
    window.dispatchEvent(new Event(THEME_EVENT));
  };

  const cycle = () => {
    const idx = themes.indexOf(theme);
    const next = themes[(idx + 1) % themes.length];
    if (next) setTheme(next);
  };

  return (
    <Button
      variant="ghost"
      size="icon"
      className={cn('relative', className)}
      onClick={cycle}
      aria-label={`Theme: ${theme}`}
    >
      <span className="relative size-4">
        {/* Sun */}
        <svg
          className={cn(
            'absolute inset-0 size-4 transition-all duration-300',
            theme === 'light'
              ? 'scale-100 rotate-0 opacity-100'
              : 'scale-0 rotate-90 opacity-0',
          )}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <circle cx="12" cy="12" r="4" />
          <path d="M12 2v2" />
          <path d="M12 20v2" />
          <path d="m4.93 4.93 1.41 1.41" />
          <path d="m17.66 17.66 1.41 1.41" />
          <path d="M2 12h2" />
          <path d="M20 12h2" />
          <path d="m6.34 17.66-1.41 1.41" />
          <path d="m19.07 4.93-1.41 1.41" />
        </svg>

        {/* Monitor (system) */}
        <svg
          className={cn(
            'absolute inset-0 size-4 transition-all duration-300',
            theme === 'system'
              ? 'scale-100 rotate-0 opacity-100'
              : 'scale-0 -rotate-90 opacity-0',
          )}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <rect width="20" height="14" x="2" y="3" rx="2" />
          <line x1="8" x2="16" y1="21" y2="21" />
          <line x1="12" x2="12" y1="17" y2="21" />
        </svg>

        {/* Moon */}
        <svg
          className={cn(
            'absolute inset-0 size-4 transition-all duration-300',
            theme === 'dark'
              ? 'scale-100 rotate-0 opacity-100'
              : 'scale-0 rotate-90 opacity-0',
          )}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
        </svg>
      </span>
    </Button>
  );
}
