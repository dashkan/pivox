/**
 * Cross-context change notification + read-side cache for
 * `@pivox/storage`.
 *
 * # Why this module exists
 *
 * The cookie backend (web — start app) has no native change-event
 * surface. Cookies set in tab A are invisible to tab B's listeners
 * until tab B re-reads `document.cookie` — and tab B has no signal
 * telling it WHEN to re-read. BroadcastChannel solves that.
 *
 * # Why this module CACHES
 *
 * Chrome's site isolation puts each tab in its own process. Cookie
 * writes IPC through the network process; BroadcastChannel hops over
 * IPC too — and the broadcast can land in the receiving tab BEFORE
 * the cookie store has finished propagating. Empirically observed:
 * a broadcast saying `pivox.theme=system` arrives, the receiver
 * calls `document.cookie` synchronously, and gets the STALE value
 * `light`. The new value shows up ~50ms later.
 *
 * The fix is the cache in this module. Every cross-tab message
 * payload carries the NEW value (not just the name), and a single
 * channel-level listener writes that value into the in-memory cache
 * BEFORE the per-subscriber handlers run. `operations.ts`'s
 * `readCookieRaw` checks the cache first; on a cache hit, it never
 * touches `document.cookie` for this tick. Race eliminated.
 *
 * The cookie remains the DURABLE source of truth — SSR reads it on
 * the next request, the boot script reads it on the next page load.
 * The cache is purely an in-tab read-side accelerator that survives
 * only as long as the page does.
 *
 * # Why BroadcastChannel rather than a dual-write tripwire
 *
 * The obvious alternative is "on every cookie write, ALSO write to
 * localStorage so other tabs get the `storage` event for free." It
 * works but the localStorage value is a phantom — written, never
 * read, easy for the next refactor to "clean up the duplicate" and
 * silently break cross-tab sync. BroadcastChannel keeps the cookie
 * as the sole durable backend and treats notification as a separate
 * concern.
 *
 * # Listener contract
 *
 * `subscribe(itemName, handler)` returns an unsubscribe function. The
 * handler is invoked with no arguments — it's a "something changed,
 * re-read" notification, NOT a "here's the new value" payload. Same
 * semantics as the native `storage` event. The cache update has
 * already happened by the time the handler fires, so any
 * `storage.get(item)` inside the handler reads the new value.
 *
 * # Same-window behavior
 *
 * BroadcastChannel does NOT deliver messages to the window that
 * posted them. Consumers that need same-window updates (e.g.,
 * ThemeSwitcher reacting to its own click) dispatch a custom in-tab
 * event in addition — see theme-switcher.tsx for the pattern. We
 * don't try to unify the two channels here because most consumers
 * already have a state-management path for the same-window write.
 *
 * SSR-safe: every export short-circuits when `BroadcastChannel` is
 * undefined. The cache lives as a module-level Map that's reset on
 * server-side cold start; SSR cookie reads go through h3's
 * `getCookie` directly in `prefs.ts`, never through this module.
 */

const CHANNEL_NAME = 'pivox.storage';

type ChangePayload = {
  /** Name of the StorageItem that changed. */
  readonly name: string;
  /**
   * The new raw value (the same string that was written to the
   * cookie / localStorage). `null` means the item was cleared.
   * Carried in the payload so the receiver can prime its cache
   * without waiting for the underlying cookie write to propagate
   * across Chrome's site-isolation IPC boundary.
   */
  readonly value: string | null;
};

/**
 * In-memory cache of the most-recently-seen raw value per item.
 * Populated by:
 *   - `notifyChange`, called from `operations.ts` after every local
 *     write — keeps same-tab `get()` reads consistent during the
 *     brief window before the cookie commit lands.
 *   - The channel-level message listener (constructed in
 *     `getChannel`), which writes the broadcast payload into the
 *     cache BEFORE any per-subscriber handler runs — this is the
 *     mechanism that beats the cross-tab cookie-propagation race.
 *
 * Values:
 *   - `string`: the item has a known value (write OR clear-followed-
 *     by-write).
 *   - `null`: the item was explicitly cleared. Reads short-circuit
 *     to `null` without touching the cookie (which may still hold
 *     a stale value mid-propagation).
 *   - Absent from the map: never observed, fall through to the
 *     durable backend (cookie or localStorage).
 *
 * SSR-safe: the Map is module state that doesn't exist on the server
 * side until the first import, and SSR cookie reads bypass this
 * module entirely.
 */
const valueCache = new Map<string, string | null>();

/**
 * Returns the cache entry for `name`. Distinguishes three states:
 *   - `undefined` — not in cache; caller should fall through to the
 *     durable backend.
 *   - `null` — item was cleared; caller should return null without
 *     reading the durable backend (which may be stale).
 *   - `string` — item has a known value.
 *
 * Internal to the package — consumed by `operations.ts` only.
 *
 * Side effect: ensures the BroadcastChannel singleton is constructed
 * so the channel-level cache-update listener is wired up. Without
 * this, a tab that ONLY reads (never sets, never subscribes) would
 * miss cross-tab updates entirely — broadcasts would land but no
 * listener would write them into the cache. The lazy-construct
 * trigger needs to fire at least once before any external broadcast
 * arrives; routing it through every `get()` call is the cheapest way
 * to guarantee that without adding a separate "warm up the channel"
 * call to every consumer.
 */
export function getCachedValue(name: string): string | null | undefined {
  // Trigger singleton construction (idempotent — only does work on
  // the very first call per page lifetime). Listener attachment is
  // a side effect of `getChannel`; we don't use the return value.
  getChannel();
  return valueCache.get(name);
}

/**
 * Lazy-initialized BroadcastChannel singleton plus the channel-level
 * cache-update listener.
 *
 * Browsers limit the number of open channels per origin, and
 * constructing one per call would also lose the message-port wiring
 * on the consumer side. The channel-level listener registered here
 * runs FIRST for every incoming message (`addEventListener` fires in
 * registration order), so the cache is always updated before any
 * per-subscriber handler from `subscribeToChanges` runs.
 *
 * Returns null when the API is unavailable so callers degrade
 * gracefully on server / very-old-browser paths.
 */
let channel: BroadcastChannel | null | undefined;
function getChannel(): BroadcastChannel | null {
  if (channel !== undefined) return channel;
  if (typeof BroadcastChannel === 'undefined') {
    channel = null;
    return null;
  }
  channel = new BroadcastChannel(CHANNEL_NAME);
  // Channel-level cache-update listener. MUST be added before any
  // subscribeToChanges registration so it fires first and the cache
  // is populated by the time per-subscriber handlers run.
  channel.addEventListener('message', (ev: MessageEvent<ChangePayload>) => {
    writeCache(ev.data.name, ev.data.value);
  });
  return channel;
}

function writeCache(name: string, value: string | null): void {
  // Store null explicitly — distinguishes "cleared" from "never
  // observed." See valueCache's doc comment for the three states.
  valueCache.set(name, value);
}

/**
 * Notify other browsing contexts that `name`'s value changed AND
 * prime this tab's cache so same-tab readers see the new value
 * immediately (without re-reading the cookie). Called from `set()`
 * and `clear()` after the underlying write completes.
 *
 * `value` is the new raw value (the same string that was written to
 * the cookie / localStorage), or `null` for a clear. Carried in the
 * broadcast payload so receiving tabs can populate their own caches
 * without the cross-process cookie-propagation race.
 *
 * Silently no-ops the broadcast portion when BroadcastChannel is
 * unavailable, but the local cache write still happens.
 */
export function notifyChange(name: string, value: string | null): void {
  // Always update the local cache, even when broadcast is
  // unavailable. The cache is the read-side accelerator for THIS
  // tab too — operations.ts relies on it to make same-tab reads
  // consistent with the most recent write.
  writeCache(name, value);
  const ch = getChannel();
  if (!ch) return;
  try {
    const payload: ChangePayload = { name, value };
    ch.postMessage(payload);
  } catch {
    // BroadcastChannel.postMessage can throw if the channel was
    // closed (page unload, GC). Notification is best-effort; the
    // value itself is already persisted to the durable backend.
  }
}

/**
 * Subscribe to cross-context changes for `itemName`. Returns an
 * unsubscribe function — call it to detach the listener. The
 * `handler` callback receives no arguments; treat the call as a
 * "re-read storage" signal. Same shape as the DOM `storage` event.
 *
 * The cache is ALREADY updated by the time the handler fires (the
 * channel-level listener in `getChannel` runs first), so
 * `storage.get(item)` inside the handler returns the new value
 * without racing the cookie propagation.
 *
 * SSR-safe / no-op when BroadcastChannel is unavailable: returns a
 * no-op unsubscribe so callers don't have to null-check.
 */
export function subscribeToChanges(
  itemName: string,
  handler: () => void,
): () => void {
  const ch = getChannel();
  if (!ch) return () => {};
  const listener = (ev: MessageEvent<ChangePayload>) => {
    if (ev.data.name === itemName) handler();
  };
  ch.addEventListener('message', listener);
  return () => {
    ch.removeEventListener('message', listener);
  };
}

/**
 * Test-only: close the singleton and clear the cache so the next call
 * rebuilds them. Mirrors `__resetRegistryForTests` and lives behind
 * the same `@pivox/storage/test-utils` subpath. Tests that swap
 * `location`, exercise the cache, or rely on a fresh BroadcastChannel
 * call this between cases.
 *
 * @internal
 */
export function __resetChannelForTests(): void {
  if (channel) {
    try {
      channel.close();
    } catch {
      // Already closed — ignore.
    }
  }
  channel = undefined;
  valueCache.clear();
}
