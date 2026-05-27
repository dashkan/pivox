/**
 * Server-side prefetch helpers for SSR route loaders.
 *
 * `_app.tsx`'s `beforeLoad` runs once on the SSR pass to populate
 * the route's QueryClient before React render. Each helper here
 * encapsulates one prefetch: read the verified session, mint an
 * actor token, fetch the underlying API, and return the response
 * shape the client's `useQuery` expects. `beforeLoad` calls
 * `queryClient.setQueryData(...)` with the result, keyed by the
 * same key the client-side `$api.queryOptions(...)` produces — so
 * the client's hooks render with hot data on first paint.
 *
 * createServerFn-wrapped so the bundler keeps these out of the
 * client build. The actor-token machinery (google-auth-library,
 * iamcredentials) only ever runs server-side; importing
 * `prefetchOrgsForCurrentUser` from a route file is safe because
 * the client sees an RPC stub, not the actual server code.
 *
 * Errors are caught and returned as `null`. A failed prefetch must
 * NOT fail the SSR render — the client-side `useQuery` will retry
 * on hydration and the user gets a brief skeleton instead of a
 * broken page.
 */

import { ACTIVE_ORG_COOKIE, organizationId } from '@pivox/client';
import { createServerFn } from '@tanstack/react-start';
import { getCookie } from '@tanstack/react-start/server';

import { getServerSession } from './auth-session';
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
export type ListSpacesResponse =
  components['schemas']['v1ListSpacesResponse'];

/**
 * prefetchOrgsForCurrentUser server-fn: fetches the caller's org
 * list using an SSR-minted actor JWT. Returns the response body on
 * success, `null` on any failure (no session, no pivox_user_id
 * claim, gateway error). `null` is the signal to skip cache
 * priming — client-side useQuery will pick up.
 */
export const prefetchOrgsForCurrentUser = createServerFn({ method: 'GET' })
  .handler(async (): Promise<ListAccountOrganizationsResponse | null> => {
    const session = await getServerSession();
    if (!session.user?.pivoxUserId) return null;

    try {
      const client = createServerApiClient(session.user.pivoxUserId);
      const { data } = await client.GET(
        '/v1/accounts/me/organizations',
        { params: { path: { parent: 'accounts/me' } } },
      );
      return data ?? null;
    } catch {
      // Don't fail SSR on prefetch — surfacing the error would
      // mean a redirect / error page where a brief loading
      // skeleton would do. The client retries on hydration via
      // its own useQuery call.
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
export const prefetchSpacesForActiveOrg = createServerFn({ method: 'GET' })
  .handler(async (): Promise<PrefetchedSpaces | null> => {
    const session = await getServerSession();
    if (!session.user?.pivoxUserId) return null;

    const activeOrg = getCookie(ACTIVE_ORG_COOKIE);
    if (!activeOrg) return null;

    try {
      // Cookie value is the canonical resource name
      // (`organizations/<slug>`). The gateway path uses the slug
      // only, so split here. `organizationId` throws on malformed
      // resource names (odd segment counts, etc.) — the try/catch
      // catches that path too so a hand-edited / corrupted cookie
      // degrades to a skeleton render rather than a 500.
      const orgSlug = organizationId(activeOrg);
      if (!orgSlug) return null;

      const client = createServerApiClient(session.user.pivoxUserId);
      const { data } = await client.GET(
        '/v1/organizations/{organization}/spaces',
        { params: { path: { organization: orgSlug } } },
      );
      if (!data) return null;
      return { orgSlug, spaces: data };
    } catch {
      return null;
    }
  });
