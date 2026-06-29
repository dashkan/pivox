/**
 * Server-side prefetch helpers for SSR route loaders.
 *
 * `_app.tsx`'s `beforeLoad` runs once on the SSR pass to populate
 * the route's QueryClient before React render. Each helper here
 * encapsulates one prefetch: resolve the user's Keycloak access
 * token from the session cookie, fetch the underlying API as that
 * user, and return the response shape the client's `useQuery`
 * expects. `beforeLoad` calls `queryClient.setQueryData(...)` with
 * the result, keyed by the same key the client-side
 * `$api.queryOptions(...)` produces — so the client's hooks render
 * with hot data on first paint.
 *
 * createServerFn-wrapped so the bundler keeps these out of the
 * client build. Importing `prefetchOrgsForCurrentUser` from a route
 * file is safe because the client sees an RPC stub, not the actual
 * server code.
 *
 * Errors are caught and returned as `null`. A failed prefetch must
 * NOT fail the SSR render — the client-side `useQuery` will retry
 * on hydration and the user gets a brief skeleton instead of a
 * broken page.
 */

import { organizationId } from '@pivox/client';
import { ACTIVE_ORG } from '@pivox/storage';
import { createServerFn } from '@tanstack/react-start';
import { getCookie } from '@tanstack/react-start/server';

import { getSsrAccessToken } from './oidc/ssr-token';
import { createServerApiClient } from './pivox-server-api';

import type { components } from '@pivox/client/types';

/**
 * Slim wire-shape of `/v1/accounts/me/organizations`. Matches what
 * the client's `$api.useQuery` returns and what we feed into
 * `queryClient.setQueryData`. Schema name is grpc-gateway's
 * standard `v1` prefix + the proto type name.
 */
export type ListAccountOrganizationsResponse =
  components['schemas']['v1ListAccountOrganizationsResponse'];

/**
 * Slim wire-shape of `/v1/organizations/{organization}/spaces`.
 * Same hydration role as ListAccountOrganizationsResponse — primes
 * the route's QueryClient with data the client's useQuery picks up
 * by matching key.
 */
export type ListSpacesResponse = components['schemas']['v1ListSpacesResponse'];

/**
 * getActiveOrgCookie server-fn: returns the SSR-time value of the
 * active-org cookie, or null if absent / malformed.
 *
 * Exposed as a server fn (rather than a direct
 * `getCookie(ACTIVE_ORG.name)` call in beforeLoad) because
 * `_app.tsx` is shared SSR+client code and TanStack Start's
 * import-protection plugin blocks `@tanstack/react-start/server`
 * imports from any module the client bundle reaches. Wrapping the
 * read in a server fn keeps the import isolated to this
 * server-only file; the client sees only the RPC stub.
 */
export const getActiveOrgCookie = createServerFn({ method: 'GET' }).handler(
  (): string | null => {
    try {
      const v = getCookie(ACTIVE_ORG.name);
      return v ? ACTIVE_ORG.parse(v) : null;
    } catch (err) {
      // getCookie shouldn't throw under normal h3 flow, but defensive
      // here matches the surrounding prefetch fns — beforeLoad must
      // never fail because the active-org cookie read tripped.
      console.warn('[ssr-prefetch] active-org cookie read threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  },
);

/**
 * prefetchOrgsForCurrentUser server-fn: fetches the caller's org
 * list using the user's Keycloak access token (from the session
 * cookie). Returns the response body on success, `null` on any
 * failure (no session, gateway error). `null` is the signal to skip
 * cache priming — client-side useQuery will pick up.
 */
export const prefetchOrgsForCurrentUser = createServerFn({
  method: 'GET',
}).handler(async (): Promise<ListAccountOrganizationsResponse | null> => {
  const accessToken = await getSsrAccessToken();
  if (!accessToken) {
    // No usable session → unauthed visit (auth gate redirects) or a
    // refresh that couldn't complete. Nothing to prime.
    return null;
  }

  try {
    const client = createServerApiClient(accessToken);
    const { data, response } = await client.GET(
      '/v1/accounts/me/organizations',
      { params: { path: { parent: 'accounts/me' } } },
    );
    if (!data) {
      // openapi-fetch returns data=undefined on non-2xx — log
      // the status so misconfigured backends (wrong audience,
      // SA not allowlisted, etc.) are diagnosable.
      console.warn('[ssr-prefetch] orgs: gateway non-2xx or empty body', {
        status: response.status,
      });
      return null;
    }
    return data;
  } catch (err) {
    // Most likely: PIVOX_API_URL missing so createServerApiClient
    // throws. Surface the message so the operator can see why SSR
    // prefetch is degrading to CSR.
    console.warn('[ssr-prefetch] orgs: threw', {
      message: err instanceof Error ? err.message : String(err),
    });
    return null;
  }
});

/**
 * Result of prefetchSpacesForActiveOrg. Returns null whenever the
 * server can't determine an active org (no cookie, no session,
 * malformed cookie value); the orgSlug field is non-null on success
 * so beforeLoad can reuse it when constructing the matching
 * queryKey via `$api.queryOptions(...)`.
 */
export interface PrefetchedSpaces {
  orgSlug: string;
  spaces: ListSpacesResponse;
}

/**
 * prefetchSpacesForActiveOrg server-fn: reads the active-org cookie
 * (`pivox.active-organization`, written client-side by the org
 * picker), mints an actor JWT for the verified user, and fetches
 * `/v1/organizations/{org}/spaces` for that org.
 *
 * Returns `null` when there's no active-org cookie — first-time
 * visitors, sign-out, freshly-created accounts. The shell's
 * spaces section renders skeleton in that case; the client picks
 * an active org after orgs load and the client-side useQuery fires
 * naturally.
 *
 * Errors swallow to null for the same SSR-shouldn't-fail reasoning
 * as prefetchOrgsForCurrentUser.
 */
export const prefetchSpacesForActiveOrg = createServerFn({
  method: 'GET',
}).handler(async (): Promise<PrefetchedSpaces | null> => {
  const accessToken = await getSsrAccessToken();
  if (!accessToken) return null;

  const activeOrg = getCookie(ACTIVE_ORG.name);
  if (!activeOrg) return null;

  try {
    const orgSlug = organizationId(activeOrg);
    if (!orgSlug) {
      console.warn('[ssr-prefetch] spaces: active-org cookie parsed empty', {
        value: activeOrg,
      });
      return null;
    }

    const client = createServerApiClient(accessToken);
    const { data, response } = await client.GET(
      '/v1/organizations/{organization}/spaces',
      { params: { path: { organization: orgSlug } } },
    );
    if (!data) {
      console.warn('[ssr-prefetch] spaces: gateway non-2xx or empty body', {
        status: response.status,
        orgSlug,
      });
      return null;
    }
    return { orgSlug, spaces: data };
  } catch (err) {
    console.warn('[ssr-prefetch] spaces: threw', {
      message: err instanceof Error ? err.message : String(err),
    });
    return null;
  }
});
