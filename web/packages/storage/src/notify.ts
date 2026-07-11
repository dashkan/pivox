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
 * Narrows an arbitrary BroadcastChannel `MessageEvent.data` to a
 * {@link ChangePayload}. What arrives is whatever anyone posted to the
 * same channel name — stale cross-deploy tabs, third-party code — so the
 * shape must be validated at runtime before use. Only `name` is required
 * (the routing key); `value` is read defensively (`?? null`) downstream.
 */
function isChangePayload(data: unknown): data is ChangePayload {
  return (
    data !== null &&
    typeof data === 'object' &&
    'name' in data &&
    typeof data.name === 'string'
  );
}

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
 * Same-window listeners — fires on EVERY local write regardless of
 * the item's `broadcast` flag.
 *
 * Same-tab consistency is non-negotiable: a React component that calls
 * `storage.set(item, value)` must re-render with the new value on the
 * next tick. The cross-tab `broadcast` flag is a separate concern (it
 * controls whether OTHER tabs see the change); within this tab, every
 * subscriber sees every write.
 *
 * BroadcastChannel doesn't deliver to the posting context, and the
 * native `storage` event only fires for cross-window writes — so
 * without this pub-sub, every consumer would need a custom in-tab
 * notification (e.g., `THEME_EVENT` in the old theme-switcher code).
 * Centralizing here removes that boilerplate at every call site.
 */
const localListeners = new Set<(name: string) => void>();

/**
 * Subscribe to same-window writes. Handler receives the name of the
 * item that changed. Returns an unsubscribe function.
 *
 * Primary consumer is `useStorageValue` in `react.ts` — every hook
 * registers one listener and filters by item name.
 */
export function subscribeLocal(handler: (name: string) => void): () => void {
  localListeners.add(handler);
  return () => {
    localListeners.delete(handler);
  };
}

function fireLocal(name: string): void {
  // Snapshot the set so a handler that mutates the set during
  // iteration (e.g., unsubscribes itself) doesn't skip later
  // handlers or trip the Set's "modified during iteration" semantics.
  for (const listener of [...localListeners]) {
    try {
      listener(name);
    } catch (err) {
      // A throwing listener must not stop the others. Log with the
      // item name so the offending consumer is identifiable.
      console.error(
        `[@pivox/storage] same-window listener threw for '${name}':`,
        err,
      );
    }
  }
}

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
  //
  // Guarded against malformed payloads — a future schema bump,
  // cross-version tab during a deploy, or third-party code that
  // posts to the same channel name with a non-conforming payload
  // shouldn't throw inside the addEventListener callback. The
  // browser would surface that as an unhandled rejection AND the
  // cache wouldn't get primed for the message; per-subscriber
  // handlers downstream might also fail to fire correctly. Better
  // to log and move on.
  // Annotated as MessageEvent<unknown> rather than
  // MessageEvent<ChangePayload> because what arrives is whatever
  // anyone posted to the same channel name — including stale
  // cross-deploy tabs and (in dev) third-party code. The shape
  // check below is the runtime narrowing back to ChangePayload.
  channel.addEventListener('message', (ev: MessageEvent<unknown>) => {
    try {
      // Defensive: validate the payload shape before destructuring.
      // The `name` field is required; `value` may be string or null.
      const data = ev.data;
      if (!isChangePayload(data)) return;
      writeCache(data.name, data.value ?? null);
    } catch (err) {
      console.error(
        '[@pivox/storage] channel-level cache update failed for payload:',
        ev.data,
        err,
      );
    }
  });
  return channel;
}

function writeCache(name: string, value: string | null): void {
  // Store null explicitly — distinguishes "cleared" from "never
  // observed." See valueCache's doc comment for the three states.
  valueCache.set(name, value);
}

/**
 * Notify subscribers that `name`'s value changed:
 *   1. Update the in-tab cache so same-tab reads see the new value
 *      without re-reading the cookie / localStorage.
 *   2. Fire the same-window pub-sub so consumers in THIS tab (e.g.,
 *      `useStorageValue` hooks) re-render.
 *   3. Post to BroadcastChannel IF `broadcast` is true — receiving
 *      tabs see the change via their own subscribers.
 *
 * `broadcast` mirrors `StorageItem.broadcast`. When false, the
 * BroadcastChannel post is skipped entirely; same-tab consistency
 * (steps 1 + 2) is unaffected because that's a separate guarantee
 * that consumers in this tab MUST get regardless of cross-tab opt-in.
 *
 * `value` is the new raw value (the same string that was written to
 * the cookie / localStorage), or `null` for a clear. Carried in the
 * broadcast payload so receiving tabs prime their own caches without
 * the cross-process cookie-propagation race.
 */
export function notifyChange(
  name: string,
  value: string | null,
  broadcast: boolean,
): void {
  // Always: update the local cache. The cache is the read-side
  // accelerator for THIS tab — operations.ts relies on it to make
  // same-tab reads consistent with the most recent write.
  writeCache(name, value);
  // Always: fire same-window pub-sub. Consumers in this tab (the one
  // that just wrote) need to re-render; broadcast doesn't gate that.
  fireLocal(name);
  // Optional: cross-tab broadcast.
  if (!broadcast) return;
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
  const listener = (ev: MessageEvent<unknown>) => {
    // Same defensive shape check as the channel-level listener in
    // `getChannel`. A malformed payload from a cross-version tab or
    // third-party code on the same channel name shouldn't crash the
    // per-subscriber handler.
    const data = ev.data;
    if (!isChangePayload(data)) return;
    if (data.name === itemName) handler();
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
  localListeners.clear();
}
