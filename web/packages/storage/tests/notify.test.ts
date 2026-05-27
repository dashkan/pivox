// @vitest-environment jsdom
/**
 * Tests for the cross-context notification layer.
 *
 * BroadcastChannel semantics are partly browser-implementation-driven
 * (delivery to other contexts, NOT the poster), so we exercise both
 * sides here: producing a notification via `notifyChange` and
 * receiving it via `subscribeToChanges`. jsdom provides a
 * BroadcastChannel implementation that mirrors the spec at the same-
 * origin level — same singleton sees all messages, but listeners only
 * fire for posts from OTHER channel instances (not the one that
 * posted), which matches real-browser behavior.
 *
 * Two channel instances:
 *   - The singleton inside `notify.ts` (lazy-constructed on first call)
 *     is the "producer" — `notifyChange` posts on it.
 *   - A separate `new BroadcastChannel('pivox.storage')` in each test
 *     stands in as a "different context" (other tab) — it receives
 *     messages posted by the singleton.
 *
 * Reset the singleton between tests via `__resetChannelForTests` so a
 * stale subscription from one test doesn't leak into the next.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { clear, defineItem, get, set } from '../src';
import { notifyChange, subscribeToChanges } from '../src/notify';
import {
  __resetChannelForTests,
  __resetRegistryForTests,
} from '../src/test-utils';

describe('notifyChange + subscribeToChanges', () => {
  // Stand-in for "another tab" — receives messages the singleton posts.
  let otherTab: BroadcastChannel;

  beforeEach(() => {
    __resetChannelForTests();
    __resetRegistryForTests();
    otherTab = new BroadcastChannel('pivox.storage');
  });

  afterEach(() => {
    otherTab.close();
    __resetChannelForTests();
  });

  it('notifyChange posts a message with the changed item name + new value', async () => {
    const received: Array<{ name: string; value: string | null }> = [];
    otherTab.addEventListener(
      'message',
      (ev: MessageEvent<{ name: string; value: string | null }>) => {
        received.push(ev.data);
      },
    );

    notifyChange('pivox.test.item', 'fresh');

    // BroadcastChannel delivery is microtask-async; await a tick so
    // the message lands before assertion.
    await new Promise((r) => setTimeout(r, 0));

    expect(received).toEqual([{ name: 'pivox.test.item', value: 'fresh' }]);
  });

  it('subscribeToChanges fires when ANOTHER channel posts the watched name', async () => {
    const handler = vi.fn();
    const unsubscribe = subscribeToChanges('pivox.test.watched', handler);

    // Post FROM the otherTab — the subscriber lives on the singleton,
    // and BroadcastChannel delivers across instances of the same name.
    otherTab.postMessage({ name: 'pivox.test.watched', value: 'x' });
    await new Promise((r) => setTimeout(r, 0));

    expect(handler).toHaveBeenCalledTimes(1);
    unsubscribe();
  });

  it('subscribeToChanges does NOT fire for a different item name', async () => {
    const handler = vi.fn();
    const unsubscribe = subscribeToChanges('pivox.test.watched', handler);

    otherTab.postMessage({ name: 'pivox.test.other', value: 'x' });
    await new Promise((r) => setTimeout(r, 0));

    expect(handler).not.toHaveBeenCalled();
    unsubscribe();
  });

  it('unsubscribe detaches the listener', async () => {
    const handler = vi.fn();
    const unsubscribe = subscribeToChanges('pivox.test.watched', handler);
    unsubscribe();

    otherTab.postMessage({ name: 'pivox.test.watched', value: 'x' });
    await new Promise((r) => setTimeout(r, 0));

    expect(handler).not.toHaveBeenCalled();
  });

  it('set() emits a notification carrying the new value', async () => {
    const received: Array<{ name: string; value: string | null }> = [];
    otherTab.addEventListener(
      'message',
      (ev: MessageEvent<{ name: string; value: string | null }>) => {
        received.push(ev.data);
      },
    );

    const item = defineItem<string>({
      name: 'pivox.test.set-notify',
      parse: (v) => v || null,
    });
    set(item, 'hi');
    await new Promise((r) => setTimeout(r, 0));

    expect(received).toEqual([{ name: 'pivox.test.set-notify', value: 'hi' }]);
  });

  it('clear() emits a notification with value=null', async () => {
    const received: Array<{ name: string; value: string | null }> = [];
    otherTab.addEventListener(
      'message',
      (ev: MessageEvent<{ name: string; value: string | null }>) => {
        received.push(ev.data);
      },
    );

    const item = defineItem<string>({
      name: 'pivox.test.clear-notify',
      parse: (v) => v || null,
    });
    clear(item);
    await new Promise((r) => setTimeout(r, 0));

    expect(received).toEqual([
      { name: 'pivox.test.clear-notify', value: null },
    ]);
  });

  it('handler does NOT fire in the same context that posted (self-delivery is off)', async () => {
    const handler = vi.fn();
    const unsubscribe = subscribeToChanges('pivox.test.self', handler);

    // notifyChange posts on the SINGLETON; the singleton is what
    // subscribeToChanges listens on. BroadcastChannel never delivers
    // to the same instance — so the handler must not fire.
    notifyChange('pivox.test.self', 'any');
    await new Promise((r) => setTimeout(r, 0));

    expect(handler).not.toHaveBeenCalled();
    unsubscribe();
  });

  /**
   * Cross-tab propagation race — the bug this whole module exists to
   * prevent.
   *
   * Real-world scenario (confirmed live in Chrome with site
   * isolation): tab A writes a cookie + posts a broadcast. Tab B
   * receives the broadcast BEFORE its `document.cookie` reflects
   * the write — Chrome's network process IPC propagates cookies
   * asynchronously across processes. Without the cache, tab B's
   * broadcast handler reads `storage.get(item)` and gets the STALE
   * cookie value; `useSyncExternalStore` sees the same snapshot
   * and skips the re-render.
   *
   * The fix: the broadcast payload carries the new value, and a
   * channel-level listener writes it into an in-memory cache BEFORE
   * any per-subscriber handler runs. `readCookieRaw` checks the
   * cache first, so the broadcast value wins over the stale cookie.
   *
   * This test simulates the race in jsdom by stubbing
   * `document.cookie` to return a STALE value, posting a broadcast
   * carrying the NEW value from a stand-in tab, and asserting that
   * `get(item)` returns the new value. Before the cache, the
   * second assertion below would have read 'stale-value' and the
   * cross-tab sync bug would have shipped.
   */
  it('beats the cross-tab cookie-propagation race (cache wins over stale cookie)', async () => {
    const item = defineItem<string>({
      name: 'pivox.test.race',
      parse: (v) => v || null,
    });

    // Stub document.cookie to a STALE value — simulates the
    // scenario where the cookie write from another tab hasn't
    // propagated through Chrome's network-process IPC yet.
    const desc = Object.getOwnPropertyDescriptor(Document.prototype, 'cookie');
    const passthroughSet = (v: string): void => {
      desc?.set?.call(document, v);
    };
    Object.defineProperty(document, 'cookie', {
      configurable: true,
      get: () => `${item.name}=stale-value; path=/`,
      set: passthroughSet,
    });

    try {
      // Pre-condition: with no cache entry, get() falls through to
      // the (stale) cookie. Proves the stub is working.
      expect(get(item)).toBe('stale-value');

      // The other tab posts a broadcast carrying the NEW value.
      // The channel-level listener inside notify.ts writes it
      // into the cache before any per-subscriber handler runs.
      otherTab.postMessage({ name: item.name, value: 'fresh-value' });
      await new Promise((r) => setTimeout(r, 0));

      // After the broadcast: cache holds 'fresh-value'; get() must
      // return that, NOT the stale cookie. This is the assertion
      // that would have failed before the cache existed.
      expect(get(item)).toBe('fresh-value');
    } finally {
      Reflect.deleteProperty(document, 'cookie');
    }
  });

  it('broadcast with value=null clears the cache (cleared-item path)', async () => {
    const item = defineItem<string>({
      name: 'pivox.test.race-clear',
      parse: (v) => v || null,
    });

    // Prime the cache via a same-tab set().
    set(item, 'present');
    expect(get(item)).toBe('present');

    // Another tab clears — broadcasts {value: null}. The cache must
    // now hold null (sentinel for "cleared"), so get() returns null
    // without consulting the cookie (which may still hold the
    // stale value mid-deletion-propagation).
    otherTab.postMessage({ name: item.name, value: null });
    await new Promise((r) => setTimeout(r, 0));

    expect(get(item)).toBeNull();
  });

  it('local set() primes the cache so same-tab get() is consistent', () => {
    // Even without a broadcast in flight, the cache makes same-tab
    // reads deterministic. Without the cache, set() followed by
    // get() in the same tick could race the cookie commit on some
    // browsers.
    const item = defineItem<string>({
      name: 'pivox.test.same-tab-prime',
      parse: (v) => v || null,
    });
    set(item, 'just-written');
    expect(get(item)).toBe('just-written');
  });
});
