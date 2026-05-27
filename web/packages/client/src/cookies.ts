/**
 * Shared cookie names used across both client (write/read) and SSR
 * (read) paths. `@pivox/client` is the single import that both
 * `'use client'` modules and server-only modules already pull in,
 * so it's the natural single source of truth.
 *
 * Cookies are namespaced `pivox.<purpose>` to avoid collision with
 * any other cookies the host origin might set.
 */

/**
 * Active organization name selected in the app-shell org picker.
 * Value is the canonical AIP resource name (`organizations/<slug>`),
 * NOT just the slug — preserved across navigations and SSR passes
 * so the right org's data is prefetched on cold loads.
 *
 * Non-HttpOnly so client JS can write it on picker changes;
 * readable by SSR for prefetch. Not a credential — a leaked value
 * tells an attacker which org the user is viewing, which they
 * could already learn by inspecting the rendered page.
 */
export const ACTIVE_ORG_COOKIE = 'pivox.active-organization';
