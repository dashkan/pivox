/**
 * React bindings for `@pivox/storage`.
 *
 * Centralizes the "read + subscribe + re-render on change" pattern
 * that consumers used to hand-roll. Two notification channels are
 * combined inside the hook so consumers don't have to know about
 * them:
 *
 *   1. SAME-WINDOW pub-sub (`subscribeLocal`) — fires on every
 *      `storage.set` / `storage.clear` in this tab, regardless of
 *      the item's `broadcast` flag. This is the channel that makes
 *      same-tab `set()`-then-re-render work; without it, the
 *      component that just wrote wouldn't know to re-render
 *      (BroadcastChannel never delivers to the posting context).
 *   2. CROSS-TAB BroadcastChannel (`subscribeToChanges`) — fires
 *      when ANOTHER tab/window writes. Subscribed only when
 *      `item.broadcast === true`; for per-tab items the channel
 *      isn't even attached (saves a listener slot AND preserves the
 *      documented per-tab semantics).
 *
 * # Why the native `storage` event isn't a third channel
 *
 * Earlier versions attached `window.addEventListener('storage', ...)`
 * as a third channel. It's removed because in Pivox today it never
 * fires usefully:
 *   - On the cookie backend (start app), the `storage` event NEVER
 *     fires for our writes — cookies don't emit it.
 *   - On the localStorage backend, the event only fires for writes
 *     in OTHER same-origin windows of this app. Pivox's electron
 *     app runs single-instance, so there's no other window to fire
 *     from.
 *
 * If a future Pivox feature opens additional BrowserWindows on
 * electron, revisit this — but gate any reintroduced listener on
 * `item.broadcast` so it doesn't silently defeat the per-tab opt-out
 * we configured for SIDEBAR_OPEN, ACTIVE_ORG, etc.
 *
 * SSR-safe via the third arg of `useSyncExternalStore` (server
 * snapshot): pass `initialValue` from the SSR cookie read; client
 * hydration matches without flicker.
 */

import { useCallback, useSyncExternalStore } from 'react';

import { subscribeToChanges, subscribeLocal } from './notify';
import { storage } from './operations';

import type { StorageItem } from './define';

/**
 * Reactive read of a storage item. Returns the current value (or
 * `null` if absent / unparseable) and re-renders the calling
 * component on any change — same-tab writes via `storage.set`,
 * cross-tab broadcasts for items with `broadcast: true`, and
 * cross-window localStorage events on electron multi-window.
 *
 * `initialValue` is the SSR-resolved value (e.g., threaded from a
 * route's `beforeLoad` via `getCookie` for start). Pass it as the
 * server snapshot so client hydration matches server HTML for the
 * first paint. Non-SSR consumers (electron, pure CSR) omit it; the
 * hook falls back to a client-side `storage.get(item)` for the
 * client snapshot.
 *
 * @example
 *   const theme = useStorageValue(THEME, initialTheme) ?? 'system';
 *   const sidebarOpen = useStorageValue(SIDEBAR_OPEN, initialSidebarOpen);
 */
export function useStorageValue<T>(
  item: StorageItem<T>,
  initialValue?: T | null,
): T | null {
  const subscribe = useCallback(
    (onChange: () => void) => {
      const unsubs: Array<() => void> = [];

      // SAME-WINDOW — always. Filter by item name inside the handler
      // so multiple hooks in the same tab don't all wake for every
      // unrelated item's write.
      unsubs.push(
        subscribeLocal((changedName) => {
          if (changedName === item.name) onChange();
        }),
      );

      // CROSS-TAB — only if the item opts into broadcast.
      // subscribeToChanges already filters by name internally.
      if (item.broadcast) {
        unsubs.push(subscribeToChanges(item.name, onChange));
      }

      return () => {
        // Per-unsub try/catch — if one teardown throws (e.g.,
        // BroadcastChannel.removeEventListener against a channel
        // closed by __resetChannelForTests mid-test, or any browser
        // quirk on teardown), the remaining unsubs MUST still run or
        // we leak listener slots across mount/unmount cycles. Mirrors
        // the per-listener defense in fireLocal (notify.ts).
        for (const u of unsubs) {
          try {
            u();
          } catch (err) {
            console.error(
              `[@pivox/storage/react] unsubscribe threw for '${item.name}':`,
              err,
            );
          }
        }
      };
    },
    [item],
  );

  // Server snapshot — used during SSR + the first client render
  // before hydration. Must match what the server would render, so we
  // use the explicit initialValue if provided. Falling back to null
  // matches `storage.get` semantics on a server pass.
  const serverSnapshot = useCallback(
    (): T | null => initialValue ?? null,
    [initialValue],
  );

  // Client snapshot — what useSyncExternalStore polls after every
  // subscribed change. Reads through `storage.get`, which checks
  // the in-tab cache first (consistent with any recent write,
  // whether local or broadcast-delivered) and falls back to the
  // cookie / localStorage backend.
  const getSnapshot = useCallback((): T | null => storage.get(item), [item]);

  return useSyncExternalStore(subscribe, getSnapshot, serverSnapshot);
}
