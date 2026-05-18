'use client';

import { Button } from '@pivox/primitives/button';
import { cn } from '@pivox/primitives/utils';
import { useEffect, useSyncExternalStore } from 'react';

type Theme = 'light' | 'system' | 'dark';

const STORAGE_KEY = 'pivox-theme';
// Custom in-tab event for same-tab storage writes — the native `storage`
// event only fires for OTHER tabs.
const THEME_EVENT = 'pivox-theme-change';

const themes: Array<Theme> = ['light', 'system', 'dark'];

function getStoredTheme(): Theme {
  return (localStorage.getItem(STORAGE_KEY) as Theme | null) ?? 'system';
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
  window.addEventListener('storage', onStoreChange);
  window.addEventListener(THEME_EVENT, onStoreChange);
  return () => {
    window.removeEventListener('storage', onStoreChange);
    window.removeEventListener(THEME_EVENT, onStoreChange);
  };
}

export function ThemeSwitcher({ className }: { className?: string }) {
  // useSyncExternalStore avoids both the SSR hydration mismatch *and* the
  // setState-in-effect cascade the manual useEffect dance triggers.
  // The server snapshot returns 'system' to match initial pre-hydration HTML.
  const theme = useSyncExternalStore(
    subscribeToTheme,
    getStoredTheme,
    () => 'system' as Theme,
  );

  // Apply theme to the document whenever it changes. Side-effecting on
  // theme is the legitimate use of useEffect.
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
    return () => mql.removeEventListener('change', handler);
  }, []);

  const setTheme = (next: Theme) => {
    localStorage.setItem(STORAGE_KEY, next);
    // `storage` event doesn't fire for same-tab writes — notify our own
    // subscribers via a synthetic event.
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
