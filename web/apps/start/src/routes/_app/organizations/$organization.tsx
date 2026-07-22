import { createFileRoute, notFound } from '@tanstack/react-router';

import { accountOrgsQueryOptions, toScopeOrgs } from '@/lib/orgs-query';
import { resolveOrgBySlug } from '@/lib/scope-resolve';

/**
 * `$organization` scope layout. The URL segment is the single source of truth for
 * the active org, so this layout RESOLVES it against the caller's memberships and
 * hard-fails an unknown/unauthorized slug with `notFound()` — never a silent
 * fallback into another org. The resolved org (resource name + slug) flows into
 * child route context.
 *
 * Reads orgs via `ensureQueryData` on the SAME key `_app.tsx` primed on the SSR
 * pass, so the SSR-primed list is reused (no server fetch — the browser apiClient
 * can't fetch on the server) and a client nav fetches at most once.
 */
export const Route = createFileRoute('/_app/organizations/$organization')({
  beforeLoad: async ({ context, params }) => {
    const data = await context.queryClient.ensureQueryData(
      accountOrgsQueryOptions(),
    );
    const org = resolveOrgBySlug(toScopeOrgs(data), params.organization);
    if (!org) throw notFound();
    return { organization: org.organization, orgSlug: params.organization };
  },
});
