/**
 * Top-level navigation policy for the renderer.
 *
 * `setWindowOpenHandler` only covers `window.open` / target=_blank. It does NOT
 * cover the renderer navigating its OWN top-level frame (`location.href = ...`,
 * a plain link, a meta-refresh). Without a `will-navigate` guard, a compromised
 * renderer — or an XSS in any dependency — can navigate out of the bundled
 * origin to an attacker page THAT STILL HAS THE PRELOAD BRIDGE ATTACHED:
 * preload is per-BrowserWindow, not per-origin. The attacker page then gets
 * `window.api`, including `auth:get-access-token`, i.e. a live access token.
 *
 * Kept free of `electron` imports so it is unit-testable under plain Node; the
 * event wiring in main.ts is a thin adapter over this.
 */

/**
 * - `allow`         — in-app navigation; let it proceed.
 * - `open-external` — a real web page; hand to the system browser instead.
 * - `deny`          — block outright, and do NOT hand to the OS.
 */
export type NavigationDecision = 'allow' | 'open-external' | 'deny';

/**
 * Decides what to do with a top-level navigation.
 *
 * @param targetUrl      the URL the renderer is trying to navigate to.
 * @param rendererOrigin the renderer's own origin — the Vite dev server origin
 *                       under `forge start`, or `null` in a packaged build
 *                       (loaded from file://, which has an opaque origin).
 */
export function decideNavigation(
  targetUrl: string,
  rendererOrigin: string | null,
): NavigationDecision {
  let target: URL;
  try {
    target = new URL(targetUrl);
  } catch {
    // Unparseable — not something we can reason about. Block.
    return 'deny';
  }

  if (rendererOrigin === null) {
    // Packaged: the app IS the file:// document. Only file:// is in-app.
    if (target.protocol === 'file:') return 'allow';
  } else {
    // Dev: the app is the dev server. Compare full origins (scheme + host +
    // port) — a substring/startsWith check would accept localhost.evil.com.
    if (target.origin === rendererOrigin) return 'allow';
    // A file:// navigation while the renderer is served over http is not us.
    if (target.protocol === 'file:') return 'deny';
  }

  // Real web pages go to the system browser, never into this window.
  if (target.protocol === 'http:' || target.protocol === 'https:') {
    return 'open-external';
  }

  // Everything else — javascript:, data:, smb:, and our own pivox:// scheme
  // (which exists for the OIDC deep link, not for navigation). Deny rather than
  // hand to the OS: shell.openExternal on these would launch another local
  // handler.
  return 'deny';
}
