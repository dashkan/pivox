'use client';

import { THEME, type Theme } from '@pivox/storage';
import { useStorageValue } from '@pivox/storage/react';

/**
 * Reactive read of the user's selected theme — the exact value the
 * {@link ThemeSwitcher} writes (`'light' | 'dark' | 'system'`). Any
 * component that needs to follow the app's theme choice (rather than
 * reading the OS directly) reads it here, so an explicit light/dark
 * selection always wins over the system preference. The app default is
 * `'system'`.
 *
 * `initialTheme` is the SSR-resolved value (threaded from the route on
 * the `start` app, same as the ThemeSwitcher's own prop) so the server
 * snapshot matches the first paint without flicker. Non-SSR consumers
 * (electron, pure client-rendered subtrees) omit it.
 *
 * @example
 *   const theme = useTheme();          // 'light' | 'dark' | 'system'
 *   <ReactFlow colorMode={theme} />;   // canvas follows the app
 */
export function useTheme(initialTheme?: Theme): Theme {
  return useStorageValue(THEME, initialTheme) ?? 'system';
}
