/**
 * Storage operations with per-platform backend selection.
 *
 * Backends:
 *   - **Cookie** on http(s) origins (web — start app). SSR can read
 *     cookies via the request `Cookie` header, which is why this is
 *     the right backend whenever SSR is in play.
 *   - **localStorage** on non-http origins (electron's `file://`,
 *     extensions, etc.). No SSR, no cross-request need. Durable.
 *
 * The two apps don't share storage anyway — different origins —
 * so each platform picking the backend that matches its lifecycle
 * is the right shape.
 *
 * SSR-safe: when `document` is undefined (server pass), ops short-
 * circuit. SSR code reads cookies through h3's `getCookie` directly
 * in the start app's `server/prefs.ts`, not through this module.
 */

import { getCachedValue, notifyChange } from './notify';

import type { StorageItem } from './define';

/**
 * Picks the backend for the current runtime.
 *
 * Web origins (`http:`, `https:`) get cookies. Anything else
 * (`file:`, electron's renderer when packaged; any non-http
 * protocol) gets localStorage. The default-to-localStorage in the
 * unknown case is safer than defaulting to cookies — cookies on
 * `file://` silently fail to persist, whereas localStorage works
 * everywhere with storage enabled.
 */
function backend(): 'cookie' | 'localStorage' {
  if (typeof location === 'undefined') return 'localStorage';
  return location.protocol === 'http:' || location.protocol === 'https:'
    ? 'cookie'
    : 'localStorage';
}

function cookieAttrs<T>(item: StorageItem<T>): string {
  const secure =
    typeof location !== 'undefined' && location.protocol === 'https:';
  return (
    ` path=${item.path};` +
    ` max-age=${String(item.maxAge)};` +
    ` samesite=lax` +
    (secure ? `; secure` : '')
  );
}

/**
 * Read the raw value for `name` from the cookie store, with a cache
 * pre-check.
 *
 * The cache (in `notify.ts`) wins when present. Three reasons:
 *   1. Same-tab consistency — a `set()` followed by `get()` in the
 *      same tick must return the just-written value. Some browsers
 *      delay reflecting `document.cookie` writes until after the
 *      current task; the cache makes this deterministic.
 *   2. Cross-tab consistency — when a BroadcastChannel message
 *      arrives in this tab, the cache is updated with the broadcast
 *      payload BEFORE the consumer handler runs. The cookie write
 *      from the other tab may not have propagated through Chrome's
 *      site-isolation IPC yet; the cache fills the gap.
 *   3. Cleared values — a cleared item lives as `null` in the cache,
 *      so a quick clear/read on the same tab returns null even if
 *      the cookie deletion is still in flight.
 *
 * If the cache holds no entry for this name (never written, never
 * received a broadcast), we fall through to the cookie.
 */
function readCookieRaw(name: string): string | null {
  const cached = getCachedValue(name);
  // `undefined` = no cache entry; `string | null` = cached value
  // (null means explicitly cleared). Both string and null short-
  // circuit the cookie read.
  if (cached !== undefined) return cached;
  if (typeof document === 'undefined') return null;
  const prefix = `${name}=`;
  // Split on `;` followed by any whitespace (including none). RFC 6265
  // §4.2.1 specifies the cookie-pair separator as `; ` (semicolon +
  // space), but writers in the wild — including some test harnesses
  // and middleware — omit the space. A literal `split('; ')` would
  // leave a leading space on the second-and-later entries, breaking
  // the prefix check below. The boot script applies the same split
  // pattern in boot-script.ts.
  for (const part of document.cookie.split(/;\s*/)) {
    if (part.startsWith(prefix)) {
      const v = part.slice(prefix.length);
      if (!v) return null;
      try {
        return decodeURIComponent(v);
      } catch {
        // Malformed percent-encoding — treat as absent.
        return null;
      }
    }
  }
  return null;
}

/**
 * Read the raw value for `name` from localStorage, with the same
 * cache pre-check as the cookie path. See `readCookieRaw` for the
 * race / consistency rationale — localStorage on electron has a
 * similar (though usually shorter) propagation window across
 * BrowserWindows when multiple are open.
 */
function readLocalStorageRaw(name: string): string | null {
  const cached = getCachedValue(name);
  if (cached !== undefined) return cached;
  if (typeof window === 'undefined') return null;
  try {
    return window.localStorage.getItem(name);
  } catch {
    // Sandbox / private-mode can throw on access.
    return null;
  }
}

function writeCookieRaw<T>(item: StorageItem<T>, value: string): void {
  if (typeof document === 'undefined') return;
  document.cookie =
    `${item.name}=${encodeURIComponent(value)};` + cookieAttrs(item);
}

function writeLocalStorageRaw(name: string, value: string): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(name, value);
  } catch {
    // Quota / disabled storage.
  }
}

function clearCookieRaw<T>(item: StorageItem<T>): void {
  if (typeof document === 'undefined') return;
  // max-age=0 deletes. Per RFC 6265, deletion matches on
  // name + path + domain (NOT secure flag), so attribute mismatches
  // are tolerated — but use the same attributes as the write to
  // stay predictable across edge cases.
  const secure =
    typeof location !== 'undefined' && location.protocol === 'https:';
  document.cookie =
    `${item.name}=;` +
    ` path=${item.path};` +
    ` max-age=0;` +
    ` samesite=lax` +
    (secure ? `; secure` : '');
}

function clearLocalStorageRaw(name: string): void {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.removeItem(name);
  } catch {
    // Disabled storage.
  }
}

/**
 * Read the typed value for `item`, or null if absent / unparseable.
 *
 * Uses the platform's selected backend (cookie on web, localStorage
 * on electron). Pure — no side effects.
 *
 * A throwing `parse` is treated identically to a parse that returns
 * null: get() returns null. The documented contract is that parse
 * should return null on bad input, not throw — but storage is
 * lifecycle-critical (mounts call this during render-time `useState`
 * lazy initializers), so a parse that throws must NOT take down the
 * whole subtree. Matches the boot script's per-item try/catch.
 */
export function get<T>(item: StorageItem<T>): T | null {
  const raw =
    backend() === 'cookie'
      ? readCookieRaw(item.name)
      : readLocalStorageRaw(item.name);
  if (raw === null) return null;
  try {
    return item.parse(raw);
  } catch (err) {
    // Log with the item name so an offending parse is identifiable in
    // production logs without needing to grep across all items.
    console.error(`[@pivox/storage] parse threw for '${item.name}':`, err);
    return null;
  }
}

/**
 * Write `value` to the platform's selected backend. Stringified via
 * `String()` — items whose values aren't natively strings should
 * either pre-serialize in the caller or use a parse function that
 * deserializes from the stringified form.
 *
 * Throws if the stringified value wouldn't round-trip through the
 * item's parse function. Without this guard, `set` would silently
 * write a value that the next `get` returns null for — a lossy
 * round-trip that masks bugs at the call site. Callers that
 * legitimately need to clear a value should use `clear(item)`.
 */
export function set<T>(item: StorageItem<T>, value: T): void {
  const raw = String(value);
  // A throwing parse is treated the same as a parse that returns null
  // for round-trip purposes: the next get() would return null, so the
  // write would be lossy. Surface as the same lossy-round-trip error.
  let parsed: T | null;
  try {
    parsed = item.parse(raw);
  } catch {
    parsed = null;
  }
  if (parsed === null) {
    throw new Error(
      `[@pivox/storage] set('${item.name}', ...) wrote a value that ` +
        `parse() rejects (returns null or throws). The round-trip would ` +
        `be lossy — the next get() would return null. Use clear(item) to ` +
        `remove a value, or fix the parse function to accept the input.`,
    );
  }
  if (backend() === 'cookie') {
    writeCookieRaw(item, raw);
  } else {
    writeLocalStorageRaw(item.name, raw);
  }
  // Same-tab cache prime + same-window pub-sub + (optional) cross-tab
  // broadcast. The item's `broadcast` flag governs only the cross-tab
  // post — same-tab consistency (cache, pub-sub) always fires so
  // consumers in this tab re-render.
  notifyChange(item.name, raw, item.broadcast);
}

/**
 * Clear `item` from the platform's selected backend. After clear,
 * `get(item)` returns null.
 */
export function clear<T>(item: StorageItem<T>): void {
  if (backend() === 'cookie') {
    clearCookieRaw(item);
  } else {
    clearLocalStorageRaw(item.name);
  }
  // null payload signals a clear — same-tab cache stores null
  // (not "absent"), so subsequent get() returns null without
  // consulting the cookie. Cross-tab broadcast follows item.broadcast.
  notifyChange(item.name, null, item.broadcast);
}

/**
 * Composed surface. Lets consumers `import { storage }` and call
 * methods on it, mirroring the conventional `storage.get / set / clear`
 * shape — preferred over importing each top-level function separately.
 *
 * Both shapes work; the namespace object is just sugar.
 */
export const storage = { get, set, clear };
