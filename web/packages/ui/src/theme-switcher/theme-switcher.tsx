'use client';

import { Button } from '@pivox/primitives/button';
import { cn } from '@pivox/primitives/utils';
import { storage, subscribeToChanges, THEME, type Theme } from '@pivox/storage';
import { useEffect, useSyncExternalStore } from 'react';

// Custom in-tab event so same-window theme writes notify subscribers
// in the SAME window — BroadcastChannel only delivers to OTHER
// browsing contexts, never to the poster.
const THEME_EVENT = 'pivox-theme-change';

const themes: Array<Theme> = ['light', 'system', 'dark'];

function getStoredTheme(): Theme {
  return storage.get(THEME) ?? 'system';
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
  // Three subscription paths, each covering a different update channel:
  //
  //   - `BroadcastChannel` (via `subscribeToChanges`): fires when
  //     ANOTHER browsing context (tab, window) writes the THEME item.
  //     Works on BOTH backends — cookies have no native change-event
  //     surface, so this is the only cross-tab signal on the start
  //     app. On electron it covers cross-window changes the same way
  //     the native `storage` event would, but uniformly.
  //
  //   - `storage` event: fires when ANOTHER window writes
  //     localStorage. Redundant with BroadcastChannel on electron (we
  //     get both for the same write), but the listener is cheap and
  //     gives us a fallback if BroadcastChannel is ever blocked by a
  //     future CSP / sandbox change. Dead on the start app (cookie
  //     backend never writes localStorage).
  //
  //   - `THEME_EVENT` (custom): fires for SAME-window writes so the
  //     icon updates instantly when the user clicks the switcher.
  //     Required because neither BroadcastChannel nor `storage` events
  //     deliver to the writer's own window.
  const unsubscribeBroadcast = subscribeToChanges(THEME.name, onStoreChange);
  window.addEventListener('storage', onStoreChange);
  window.addEventListener(THEME_EVENT, onStoreChange);
  return () => {
    unsubscribeBroadcast();
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
    storage.set(THEME, next);
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
