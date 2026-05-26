/**
 * FOUC-prevention theme bootstrap. Runs synchronously in <head>
 * BEFORE the React bundle loads so the `dark` class lands on <html>
 * before the first paint.
 *
 * Pair of the inline script in apps/start/src/routes/__root.tsx. Lives
 * as an external file (not inline) so it can be prettier-formatted
 * and edited without recomputing a CSP hash — `script-src 'self'`
 * already covers same-origin script assets.
 *
 * Reads the same `pivox-theme` localStorage key as
 * <ThemeSwitcher /> (packages/ui/src/theme-switcher/theme-switcher.tsx).
 * Default is 'system', which resolves via the `prefers-color-scheme`
 * media query.
 */
(function applyStoredTheme() {
  try {
    var stored = localStorage.getItem('pivox-theme') || 'system';
    var prefersDark = window.matchMedia('(prefers-color-scheme: dark)').matches;
    var isDark = stored === 'system' ? prefersDark : stored === 'dark';
    if (isDark) document.documentElement.classList.add('dark');
  } catch (e) {
    // localStorage / matchMedia unavailable (sandboxed renderer?).
    // Silently fall through to the default (light) — better than
    // crashing pre-React.
  }
})();
