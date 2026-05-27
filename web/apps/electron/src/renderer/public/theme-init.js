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
 * Reads in the same order as <ThemeSwitcher /> (packages/ui/src/
 * theme-switcher/theme-switcher.tsx):
 *   1. `pivox.theme` cookie — fast path, SSR-readable on the web.
 *   2. `pivox-theme` localStorage — durable fallback. Cookies on
 *      `file://` origins (electron production) don't persist
 *      reliably across sessions, so localStorage is the source of
 *      truth in that environment.
 *
 * Default is 'system', which resolves via the `prefers-color-scheme`
 * media query.
 */
(function applyStoredTheme() {
  try {
    var cookieMatch = document.cookie.match(/(?:^|; )pivox\.theme=([^;]+)/);
    var stored = cookieMatch && cookieMatch[1];
    if (stored !== 'light' && stored !== 'dark' && stored !== 'system') {
      var local = localStorage.getItem('pivox-theme');
      stored =
        local === 'light' || local === 'dark' || local === 'system'
          ? local
          : 'system';
    }
    var prefersDark = window.matchMedia(
      '(prefers-color-scheme: dark)',
    ).matches;
    var isDark = stored === 'system' ? prefersDark : stored === 'dark';
    if (isDark) document.documentElement.classList.add('dark');
  } catch (e) {
    // localStorage / matchMedia / cookie access unavailable (sandboxed
    // renderer?). Silently fall through to the default (light) —
    // better than crashing pre-React.
  }
})();
