'use client';

import { organizationId } from '@pivox/client';
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
 * localStorage slot for the user's active organization. Namespaced
 * `pivox.app-shell.*` so a future broader settings vocabulary
 * doesn't collide. Per-browser, not per-account — same caveat as
 * the remember-me email storage in useLogin.
 */
const ACTIVE_ORG_STORAGE_KEY = 'pivox.app-shell.active-organization';

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
}): AppShellContextValue {
  const { $api, onCreateOrganization, navMain = [], initialUser } = input;
  const { user, signOut } = useAuth();
  // Prefer the live Firebase user once useAuth resolves (mutations
  // on the account update displayName/photoURL in IndexedDB before
  // they round-trip through Pivox). Fall back to the SSR-seeded
  // user when still loading.
  const displayUser = user ?? initialUser ?? null;
  const [profileOpen, setProfileOpen] = useState(false);
  const [activeOrganization, setActiveOrganizationState] = useState<
    string | null
  >(null);

  const setActiveOrganization = useCallback((organization: string) => {
    setActiveOrganizationState(organization);
    if (typeof window === 'undefined') return;
    window.localStorage.setItem(ACTIVE_ORG_STORAGE_KEY, organization);
  }, []);

  // Hydrate active-org from localStorage on mount. SSR-safe: server
  // renders with null (org picker collapses to nothing); client
  // hydrates with the persisted value. The validation effect below
  // catches the case where the persisted org no longer exists in
  // the user's membership list (deleted, lost access).
  useEffect(() => {
    if (typeof window === 'undefined') return;
    const saved = window.localStorage.getItem(ACTIVE_ORG_STORAGE_KEY);
    // eslint-disable-next-line react-hooks/set-state-in-effect -- one-shot localStorage hydration; lazy initializer is SSR-unsafe
    if (saved) setActiveOrganizationState(saved);
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
