'use client';

import { Button } from '@pivox/primitives/button';
import { cn } from '@pivox/primitives/utils';
import { storage, THEME, type Theme } from '@pivox/storage';
import { useStorageValue } from '@pivox/storage/react';
import { useEffect } from 'react';

const themes: Array<Theme> = ['light', 'system', 'dark'];

function getSystemPreference(): 'light' | 'dark' {
  return window.matchMedia('(prefers-color-scheme: dark)').matches
    ? 'dark'
    : 'light';
}

function applyTheme(theme: Theme) {
  const resolved = theme === 'system' ? getSystemPreference() : theme;
  document.documentElement.classList.toggle('dark', resolved === 'dark');
}

export function ThemeSwitcher({
  className,
  initialTheme,
}: {
  className?: string;
  /**
   * SSR-resolved theme from the `pivox.theme` cookie. Threaded by
   * the route so the hook's server snapshot returns the user's
   * actual preference, not the default `'system'` — without this
   * the icon would flicker from `system` to the saved value on
   * hydration.
   *
   * Optional because non-SSR consumers (electron) don't supply one;
   * `'system'` is the right default for them.
   */
  initialTheme?: Theme;
}) {
  // useStorageValue handles three notification channels internally:
  //   - same-window pub-sub (so the icon updates on this tab's own click)
  //   - BroadcastChannel (so OTHER tabs sync — THEME has `broadcast: true`)
  //   - native `storage` event (electron multi-window localStorage backend)
  // No custom event, no useSyncExternalStore boilerplate. The SSR
  // server snapshot uses `initialTheme` so the first paint matches
  // server-rendered HTML.
  const theme = useStorageValue(THEME, initialTheme) ?? 'system';

  // Apply theme to the document whenever it changes.
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  // Re-apply when system preference changes and we're in 'system' mode.
  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const handler = () => {
      if ((storage.get(THEME) ?? 'system') === 'system') applyTheme('system');
    };
    mql.addEventListener('change', handler);
    return () => {
      mql.removeEventListener('change', handler);
    };
  }, []);

  const setTheme = (next: Theme) => {
    storage.set(THEME, next);
    // No custom event needed — storage.set fires the same-window pub-sub
    // inside @pivox/storage's notify.ts, which wakes useStorageValue
    // in this tab.
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
