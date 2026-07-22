import { spaceId } from '@pivox/client';
import { createFileRoute, notFound } from '@tanstack/react-router';

import { orgSpacesQueryOptions } from '@/lib/orgs-query';
import { prefetchSpacesForOrg } from '@/server/prefetch';

/**
 * `$space` scope layout, nested under `$organization`. Narrows scope to a space
 * the same way the org layout resolves the org: the `$space` segment is
 * authoritative, so this RESOLVES it against the org's spaces and hard-fails an
 * unknown slug with `notFound()` — never a silent fallback to the org rollup.
 *
 * On the SSR pass the org's spaces aren't guaranteed to be primed (the shell only
 * primes the cookie-org's spaces), so we prime them here via a server-fn, then
 * `ensureQueryData` reuses that entry; on the client `ensureQueryData` fetches
 * once. The resolved space (resource name + slug) flows into child context.
 */
export const Route = createFileRoute(
  '/_app/organizations/$organization/spaces/$space',
)({
  beforeLoad: async ({ context, params }) => {
    if (typeof window === 'undefined') {
      const primed = await prefetchSpacesForOrg({
        data: { orgSlug: params.organization },
      });
      if (primed) {
        context.queryClient.setQueryData(
          orgSpacesQueryOptions(params.organization).queryKey,
          primed,
        );
      }
    }

    const data = await context.queryClient.ensureQueryData(
      orgSpacesQueryOptions(params.organization),
    );
    const found = (data.spaces ?? []).some(
      (s) => typeof s.name === 'string' && spaceId(s.name) === params.space,
    );
    if (!found) throw notFound();

    return {
      space: `organizations/${params.organization}/spaces/${params.space}`,
      spaceSlug: params.space,
    };
  },
});
