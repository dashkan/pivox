/**
 * Browser storage layer for Pivox. Cookie + localStorage dual-store
 * keyed by typed `StorageItem` descriptors.
 *
 * Typical usage:
 *
 *   import { storage, THEME } from '@pivox/storage';
 *
 *   const current = storage.get(THEME);     // Theme | null
 *   storage.set(THEME, 'dark');             // writes cookie + localStorage
 *   storage.clear(THEME);                   // clears both
 *
 * Defining a new item:
 *
 *   import { defineItem } from '@pivox/storage';
 *   export const MY_PREF = defineItem<MyType>({
 *     name: 'pivox.my-pref',
 *     path: '/my-route',
 *     parse: (v) => isMyType(v) ? v : null,
 *   });
 *
 * Items self-register; `allItems()` returns the catalog for the SSR
 * pre-hydration script.
 */

export { defineItem, allItems, type StorageItem } from './define';
export { storage, get, set, clear } from './operations';
export { subscribeToChanges } from './notify';
export { THEME, LAST_EMAIL, ACTIVE_ORG, type Theme } from './items';
export { buildBootScript } from './boot-script';
