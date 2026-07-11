/**
 * Typed item descriptor for a single value stored via the storage
 * abstraction. Items are pure data — name, path, parse function — and
 * are passed to `get` / `set` / `clear` operations as values.
 *
 * Each `defineItem` call self-registers in an internal catalog. The
 * catalog powers the pre-hydration inline script in `__root.tsx`, which
 * needs to enumerate every known item name to promote localStorage
 * values into cookies before React mounts. Without the catalog, the
 * script would need a hand-maintained duplicate list.
 *
 * Items are not class instances — they're frozen records. Consumers
 * import them as constants and pass them to operations:
 *
 *   import { THEME } from '@pivox/storage';
 *   import { storage } from '@pivox/storage';
 *   storage.get(THEME);
 *   storage.set(THEME, 'dark');
 *   storage.clear(THEME);
 */

export interface StorageItem<T> {
  /**
   * Cookie name AND localStorage key. The same string identifies both
   * stores — see the package README for the dual-store rationale.
   */
  readonly name: string;
  /**
   * Cookie path. Defaults to `/`. Scope items down (e.g.
   * `/auth/login`) to limit which requests carry the cookie — reduces
   * HTTP-egress exposure for items that don't need broad reach.
   */
  readonly path: string;
  /**
   * Parses a raw string from cookie / localStorage into the typed
   * value. Returns null when the string can't be coerced (unknown
   * variant, malformed, etc.) — `get()` returns null in that case.
   */
  readonly parse: (raw: string) => T | null;
  /**
   * Cookie max-age in seconds. Defaults to 1 year.
   */
  readonly maxAge: number;
  /**
   * Whether writes to this item broadcast to other browsing contexts
   * (tabs / windows). Defaults to `false` — most items are per-tab
   * UI state where cross-tab sync is undesired (sidebar collapsed
   * here ≠ collapsed everywhere). Set to `true` for items where the
   * user expects synchronized state across all open contexts (e.g.,
   * theme: changing dark mode in one tab should reflect everywhere).
   *
   * Implementation: when `false`, `notifyChange` still updates the
   * same-tab in-memory cache + fires the same-window pub-sub, but
   * skips the `BroadcastChannel.postMessage`. Receiving tabs never
   * hear about the change. When `true`, the broadcast posts and any
   * tab subscribed via `useStorageValue` re-renders.
   *
   * Note: same-tab consistency is NEVER opt-out — within a single
   * tab, `storage.set` followed by `storage.get` always reflects
   * the new value, and any `useStorageValue` consumer in the same
   * tab re-renders. The flag governs CROSS-TAB only.
   */
  readonly broadcast: boolean;
  /**
   * Ownership scope of the value, which governs sign-out behavior:
   *
   * - `'user'` — tied to the signed-in user (e.g. the selected org).
   *   MUST be cleared on sign-out via `clearUserScopedItems()` so the
   *   next user can't inherit the previous user's state.
   * - `'device'` — a per-device/per-tab preference (theme, sidebar,
   *   login auto-fill) that SURVIVES sign-out.
   *
   * Defaults to `'device'`: a setting is only user-state if it's
   * explicitly declared so. New per-user items must opt in with
   * `scope: 'user'` to be auto-cleared on sign-out.
   */
  readonly scope: 'user' | 'device';
  /**
   * Optional synchronous side effect, invoked by the pre-hydration
   * inline script with the parsed value (or null if absent in both
   * stores). Runs ONCE on app boot, BEFORE any framework mounts and
   * BEFORE the body paints.
   *
   * MUST be synchronous — async work belongs in a post-mount effect
   * where it can coexist with skeletons and error boundaries.
   * Returning a Promise here would silently escape the inline
   * script's try/catch AND defeat the FOUC-prevention purpose (paint
   * runs before microtask resolution).
   *
   * Constraints: the function is serialized via `.toString()` and
   * inlined as text into the boot script — it must be self-contained.
   * Reference only globals (window, document, matchMedia, localStorage).
   * NO closures over module imports, NO references to module-level vars.
   *
   * Use cases: applying a CSS class for theme, setting a document
   * attribute, hydrating a window-global before app code reads it.
   */
  readonly onBoot?: (value: T | null) => void;
}

const DEFAULT_MAX_AGE_SECONDS = 60 * 60 * 24 * 365;

/**
 * Module-internal registry of every defined item. `defineItem` writes
 * here as a side effect at module load time. `allItems()` returns the
 * frozen list — used by the SSR app's pre-hydration script + by tests.
 *
 * Keyed by `name` so a re-registration (Vite HMR re-executing items.ts,
 * tests redefining a name) replaces in place rather than duplicating.
 * Last-write-wins. Name collisions across two genuinely-different
 * items would silently shadow — that's prevented by code review on
 * items.ts (the only place items get defined), not by a runtime throw.
 */
const registry = new Map<string, StorageItem<unknown>>();

export function defineItem<T>(opts: {
  name: string;
  path?: string;
  parse: (raw: string) => T | null;
  maxAge?: number;
  /**
   * Opt-in cross-tab sync. Defaults to `false`. See StorageItem.broadcast
   * for the rationale on why per-tab is the safer default.
   */
  broadcast?: boolean;
  /**
   * Sign-out ownership. Defaults to `'device'`. Set `'user'` for
   * per-user state that must be cleared on sign-out. See
   * StorageItem.scope.
   */
  scope?: 'user' | 'device';
  onBoot?: (value: T | null) => void;
}): StorageItem<T> {
  const item: StorageItem<T> = Object.freeze({
    name: opts.name,
    path: opts.path ?? '/',
    parse: opts.parse,
    maxAge: opts.maxAge ?? DEFAULT_MAX_AGE_SECONDS,
    broadcast: opts.broadcast ?? false,
    scope: opts.scope ?? 'device',
    ...(opts.onBoot ? { onBoot: opts.onBoot } : {}),
  });
  // StorageItem<T> is invariant in T (T appears in parse's return AND onBoot's
  // parameter), so StorageItem<T> is not assignable to StorageItem<unknown>. The
  // registry only ever exposes T-independent read fields (name/path) to its
  // consumers, so erasing T at this storage boundary is sound.
  // oxlint-disable-next-line typescript/no-unsafe-type-assertion -- StorageItem<T> is invariant in T; the registry only exposes T-independent fields to consumers
  registry.set(opts.name, item as StorageItem<unknown>);
  return item;
}

/**
 * Returns every registered item. Iteration order matches first-
 * registration order (Map preserves insertion order; re-registering
 * an existing name keeps its original slot).
 *
 * Intended audience: the SSR app's pre-hydration inline script (reads
 * `.name` from each to know which cookies + localStorage keys to
 * inspect on cold load). Not for general application code — features
 * should import the specific items they use by name.
 */
export function allItems(): ReadonlyArray<StorageItem<unknown>> {
  return Array.from(registry.values());
}

/**
 * Test-only: clears the registry. Module-level state needs a reset
 * hook so tests that exercise registration behavior don't bleed
 * state across cases.
 *
 * Exported from a dedicated `@pivox/storage/test-utils` subpath rather
 * than the root entry — keeps the production bundle surface free of
 * a foot-gun that would let any consumer wipe the registry.
 *
 * @internal
 */
export function __resetRegistryForTests(): void {
  registry.clear();
}
