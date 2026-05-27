'use client';

import { ACTIVE_ORG_COOKIE, organizationId } from '@pivox/client';
import { useCallback, useEffect, useMemo, useState } from 'react';

import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  AppShellContextValue,
  NavMainItem,
  NavSpacesSpace,
  OrgPickerOrg,
} from '@pivox/ui/app-shell';

import { useAuth } from '@/auth/use-auth';

/**
 * Legacy localStorage key for the active organization. Phase 6 moved
 * this to a cookie so SSR can read it; this key sticks around only
 * long enough to migrate values on first mount. TODO: remove after
 * 2026-09 (well past 30 days post-deploy for any still-active
 * browser to have mounted at least once and migrated).
 */
const LEGACY_STORAGE_KEY = 'pivox.app-shell.active-organization';

/**
 * 1-year cookie lifetime. The active-org preference is per-browser
 * UX state, not a security credential — long expiry matches the
 * "remember my last context" feel users expect from sidebar pickers.
 */
const ACTIVE_ORG_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

function readActiveOrgCookie(): string | null {
  if (typeof document === 'undefined') return null;
  const prefix = `${ACTIVE_ORG_COOKIE}=`;
  for (const part of document.cookie.split('; ')) {
    if (part.startsWith(prefix)) {
      const value = part.slice(prefix.length);
      return value || null;
    }
  }
  return null;
}

function writeActiveOrgCookie(value: string): void {
  if (typeof document === 'undefined') return;
  // `secure` mirrors the session-cookie convention in
  // apps/start/src/server/auth-session.ts: on for HTTPS, off for
  // dev's http://localhost. Using location.protocol rather than an
  // env var because this code runs client-side where process.env
  // isn't reliably the same shape across bundlers.
  const secure = typeof location !== 'undefined' && location.protocol === 'https:';
  document.cookie =
    `${ACTIVE_ORG_COOKIE}=${value};` +
    ` path=/;` +
    ` max-age=${String(ACTIVE_ORG_COOKIE_MAX_AGE)};` +
    ` samesite=lax` +
    (secure ? `; secure` : '');
}

/**
 * Builds the AppShell context value from live data: user from
 * `useAuth()`, orgs from `/v1/accounts/me/organizations`, spaces
 * from `/v1/organizations/{organization}/spaces` scoped to the
 * active org. Active-org selection persists to localStorage so the
 * user lands back on their last-used org on next visit.
 *
 * `$api` is dependency-injected by the consuming route (each app
 * creates its own `$api = createReactQueryApi(apiClient)` against
 * its own apiClient — same shape, different baseUrl). Keeps this
 * hook app-agnostic; works in start and electron alike.
 *
 * `navMain` is passed in by the route — the hook doesn't synthesize
 * navigation items because the route shape is app-specific (start
 * and electron have different routes). Today both apps pass an
 * empty list; iterations land item config here without re-shaping
 * the hook.
 */
export function useAppShell(input: {
  $api: ReactQueryApi;
  /** Invoked by OrgPicker's "Create Organization" CTA. */
  onCreateOrganization: () => void;
  /** Top-level nav items. Empty placeholder works for the moment. */
  navMain?: NavMainItem[];
  /**
   * Server-verified user, threaded by the SSR route when available.
   * Used as the displayed user until `useAuth()` resolves on
   * hydration — eliminates the avatar / display-name flash on cold
   * loads. Electron (no SSR) passes nothing; useAuth() is the
   * sole source there.
   */
  initialUser?: {
    displayName: string | null;
    email: string | null;
    photoURL: string | null;
  };
  /**
   * Server-resolved active org from the SSR cookie read. Pass this
   * from the route's beforeLoad/context when SSR has already seen
   * the cookie — synchronizes initial state across SSR and client
   * paints, and prevents the validation effect from racing the
   * cookie-hydration setState. Electron (no SSR) omits this.
   */
  initialActiveOrganization?: string | null;
}): AppShellContextValue {
  const {
    $api,
    onCreateOrganization,
    navMain = [],
    initialUser,
    initialActiveOrganization,
  } = input;
  const { user, signOut } = useAuth();
  // Prefer the live Firebase user once useAuth resolves (mutations
  // on the account update displayName/photoURL in IndexedDB before
  // they round-trip through Pivox). Fall back to the SSR-seeded
  // user when still loading.
  const displayUser = user ?? initialUser ?? null;
  const [profileOpen, setProfileOpen] = useState(false);

  // Initialize active-org synchronously so the validation effect
  // below sees a non-null value on its first run. The original
  // shape (`useState(null)` + a `useEffect` that read the cookie)
  // had a race: both effects ran after the first commit, the
  // validation effect's closure captured `activeOrganization=null`
  // (because effect-1's setState hadn't committed yet), and it
  // overwrote the user's selection with `orgs[0]` — every refresh
  // silently reverted the picker to the alphabetically-first org
  // and rewrote the cookie. Lazy initializer eliminates the race.
  //
  // Precedence: SSR-resolved value (matches the HTML the server
  // rendered, no hydration mismatch) > client-side cookie read
  // (covers electron + any pure CSR path) > null.
  const [activeOrganization, setActiveOrganizationState] = useState<
    string | null
  >(() => {
    if (initialActiveOrganization) return initialActiveOrganization;
    if (typeof document === 'undefined') return null;
    return readActiveOrgCookie();
  });

  const setActiveOrganization = useCallback((organization: string) => {
    setActiveOrganizationState(organization);
    writeActiveOrgCookie(organization);
  }, []);

  // One-shot legacy localStorage → cookie migration. Runs only when
  // the cookie is genuinely absent; the lazy initializer above
  // already covers the "cookie has a value" case. Drop this block
  // once enough time has passed for active users to have migrated
  // (~30 days post-deploy is fine).
  useEffect(() => {
    if (typeof window === 'undefined') return;
    if (readActiveOrgCookie()) return;
    const legacy = window.localStorage.getItem(LEGACY_STORAGE_KEY);
    if (!legacy) return;
    writeActiveOrgCookie(legacy);
    window.localStorage.removeItem(LEGACY_STORAGE_KEY);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-shot legacy migration; falls through to validation effect below
    setActiveOrganizationState(legacy);
  }, []);

  // Orgs query — slim caller-scoped view (used by post-sign-in
  // bootstrap and the picker). The path's `{parent}` is literal
  // (`accounts/me` is baked into the URL by the proto's path
  // binding), but openapi-typescript still types it as a required
  // path param — pass the literal value here. openapi-fetch sees
  // no `{parent}` placeholder in the path and doesn't substitute.
  const orgsQuery = $api.useQuery('get', '/v1/accounts/me/organizations', {
    params: { path: { parent: 'accounts/me' } },
  });

  // Client-side sort by displayName. The endpoint doesn't expose
  // `order_by` (intentionally unpaginated, see proto) and the result
  // set is small (1–10 typical), so sorting on the client is the
  // right tradeoff — saves a round-trip surface and keeps the proto
  // free of pagination/sort fields it doesn't need.
  const orgs = useMemo<OrgPickerOrg[]>(
    () =>
      (orgsQuery.data?.accountOrganizations ?? [])
        .filter(
          (o): o is { organization: string; displayName?: string } =>
            typeof o.organization === 'string' && o.organization.length > 0,
        )
        .map((o) => ({
          organization: o.organization,
          displayName: o.displayName ?? o.organization,
        }))
        .sort((a, b) =>
          a.displayName.localeCompare(b.displayName, undefined, {
            sensitivity: 'base',
          }),
        ),
    [orgsQuery.data],
  );

  // Validate persisted active-org against the loaded org list. If
  // the persisted org isn't (or is no longer) in the list, default
  // to the first one. Defaulting also fires on first sign-in when
  // there's no persisted value.
  //
  // The synchronous setState in the effect IS the cascading render
  // the rule warns about — but the cascade is bounded (the next
  // render's deps include `activeOrganization`, so this effect's
  // guard fires and we early-return). The alternative — initialize
  // active-org via useState's lazy initializer — doesn't work
  // because the orgs query result isn't available at mount; we
  // need to react to its arrival.
  useEffect(() => {
    if (orgs.length === 0) return;
    if (
      activeOrganization &&
      orgs.some((o) => o.organization === activeOrganization)
    ) {
      return;
    }
    const first = orgs[0]?.organization;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- canonical react-to-external-data: derive default once orgs arrive, bounded by the early-return guard
    if (first) setActiveOrganization(first);
  }, [orgs, activeOrganization, setActiveOrganization]);

  // Spaces query — scoped to the active org. Disabled until we have
  // one (initial render before localStorage hydrate + orgs load).
  //
  // The `{organization}` path param is the slug only — grpc-gateway
  // flattens the proto's `{parent=organizations/*}` binding into a
  // literal `organizations/` prefix + a `{organization}` segment.
  // `activeOrganization` is the canonical resource name
  // (`organizations/{slug}`); `organizationId()` extracts the slug.
  const activeOrgSlug = activeOrganization ? organizationId(activeOrganization) : '';
  const spacesQuery = $api.useQuery(
    'get',
    '/v1/organizations/{organization}/spaces',
    { params: { path: { organization: activeOrgSlug } } },
    { enabled: !!activeOrgSlug },
  );

  const spaces = useMemo<NavSpacesSpace[]>(
    () =>
      (spacesQuery.data?.spaces ?? [])
        .filter(
          (s): s is { name: string; displayName?: string } =>
            typeof s.name === 'string' && s.name.length > 0,
        )
        .map((s) => ({
          space: s.name,
          displayName: s.displayName ?? s.name,
          // TODO: real route once space landing pages exist; '/' is
          // a placeholder so clicks at least don't 404.
          href: '/',
        })),
    [spacesQuery.data],
  );

  return {
    state: {
      user: displayUser
        ? {
            displayName: displayUser.displayName,
            email: displayUser.email,
            photoURL: displayUser.photoURL,
          }
        : null,
      orgs,
      orgsLoading: orgsQuery.isLoading,
      activeOrganization,
      spaces,
      spacesLoading: spacesQuery.isLoading,
      navMain,
      profileOpen,
    },
    actions: {
      setActiveOrganization,
      createOrganization: onCreateOrganization,
      setProfileOpen,
      signOut: async () => {
        setProfileOpen(false);
        await signOut();
      },
    },
  };
}
