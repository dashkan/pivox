/**
 * Pivox-app storage items. Each is a typed identifier + per-item
 * config; consumers import the constant they need and pass it to
 * storage operations.
 *
 * Path scoping is deliberate per item — every cookie has its scope
 * justified in its block. Items used by SSR routes (active org) need
 * path=`/` so the cookie rides on the navigation that triggers the
 * SSR pass; items used only on a specific route (login last-email)
 * scope down to minimize HTTP-egress exposure of the value.
 *
 * Threat-model notes:
 *   - Email is PII per GDPR/CCPA. Path-scoping to `/auth/login`
 *     means the cookie isn't sent on API / asset requests, narrowing
 *     log-capture exposure to just the login route — where the email
 *     is already in the request body anyway. XSS on any non-login
 *     page can't read the cookie (per RFC 6265 §5.4 path-matching).
 *   - Active organization name is org-affiliation metadata. Path=`/`
 *     because the SSR shell reads it on every authed route. Same XSS
 *     reach as the rest of the page; not a new vector.
 *   - Theme is purely UI state, not sensitive.
 */

import { defineItem } from './define';

/** Light, dark, or follow OS preference. */
export type Theme = 'light' | 'dark' | 'system';

/**
 * THEME — user's preferred color scheme.
 *
 * Path `/` because the dark class is applied to <html> on every page;
 * the pre-hydration inline script needs the cookie regardless of
 * which route the user lands on. Not sensitive.
 */
export const THEME = defineItem<Theme>({
  name: 'pivox.theme',
  path: '/',
  // Cross-tab sync ON: theme is the one preference users expect to
  // mirror everywhere. Toggling dark mode in one tab should darken
  // every other open tab. Verified live in browser.
  broadcast: true,
  parse: (v) => (v === 'light' || v === 'dark' || v === 'system' ? v : null),
  // Applies the `dark` class to <html> before the body paints, so
  // dark-mode users don't see a flash of light-mode content. Runs
  // synchronously in the inline boot script — must be self-contained
  // (no closures, only globals). Resolves 'system' → light/dark via
  // matchMedia at boot time; subsequent OS preference changes are
  // handled by the ThemeSwitcher component's media-query listener.
  onBoot: (value) => {
    const resolved =
      value === 'system'
        ? matchMedia('(prefers-color-scheme: dark)').matches
          ? 'dark'
          : 'light'
        : value;
    if (resolved === 'dark') document.documentElement.classList.add('dark');
  },
});

/**
 * LAST_EMAIL — auto-fill email value for the login card on next
 * visit. Set by the "Remember me" checkbox after a successful
 * password sign-in; cleared by social / SSO sign-ins.
 *
 * Path `/auth/login` because (a) only the login route reads it, and
 * (b) the email is PII — scoping the cookie limits the requests
 * that carry the value in headers (no leak into API / asset logs,
 * Sentry captures on other pages, etc.). XSS on non-login pages
 * can't read it either.
 */
export const LAST_EMAIL = defineItem<string>({
  name: 'pivox.login.last-email',
  path: '/auth/login',
  parse: (v) => (v.length > 0 ? v : null),
});

/**
 * ACTIVE_ORG — currently-selected organization resource name
 * (`organizations/<slug>`) from the org picker.
 *
 * Path `/` because the `_app` SSR layout's beforeLoad reads it to
 * prefetch the right org's spaces on every authed navigation — the
 * cookie has to ride on those navigations to be useful. Same XSS
 * reach as the rest of the authenticated UI.
 */
export const ACTIVE_ORG = defineItem<string>({
  name: 'pivox.active-organization',
  path: '/',
  parse: (v) => (v.length > 0 ? v : null),
});

/**
 * SIDEBAR_OPEN — persisted state of the app-shell sidebar
 * (expanded vs collapsed).
 *
 * Written + read by `@pivox/ui/sidebar-provider`'s
 * `SidebarProvider` wrapper, which routes shadcn's `<SidebarProvider>`
 * through controlled mode so persistence lives in `@pivox/storage`
 * (cookie on web, localStorage on electron) — NOT in the dead
 * `sidebar_state` cookie that shadcn writes internally and never
 * reads back. Cookie name follows Pivox convention rather than
 * shadcn's underscore-style; the dead `sidebar_state` cookie from
 * shadcn line 83 remains as harmless write-only noise (not modifying
 * vendored shadcn code is the explicit tradeoff).
 *
 * Path `/` because the sidebar lives in the `_app` layout that
 * wraps every authed route — the SSR beforeLoad reads it on every
 * navigation, so the cookie has to ride on those requests.
 *
 * `broadcast` left at the default `false` — sidebar state is a
 * per-tab UI preference. Different tabs are different workflows;
 * toggling the sidebar in one window should not collapse it in
 * another. State still persists across reloads in the same tab
 * (cookie is durable) — cookies are origin-shared, so after reload
 * any tab reads the most recent write, but within a tab's session
 * the user's last toggle stays put.
 *
 * Value type is `boolean` — serialized as 'true' / 'false' via
 * `String(value)` on write. parse accepts the same shape on read.
 */
export const SIDEBAR_OPEN = defineItem<boolean>({
  name: 'pivox.sidebar-state',
  path: '/',
  parse: (v) => (v === 'true' ? true : v === 'false' ? false : null),
});
