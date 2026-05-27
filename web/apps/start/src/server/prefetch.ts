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

import { createServerFn } from '@tanstack/react-start';

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
