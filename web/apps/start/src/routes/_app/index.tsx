import { organizationId } from '@pivox/client';
import { ACTIVE_ORG, storage } from '@pivox/storage';
import { createFileRoute, redirect } from '@tanstack/react-router';

import { accountOrgsQueryOptions, toScopeOrgs } from '@/lib/orgs-query';
import { resolveRootTarget } from '@/lib/scope-resolve';
import { getActiveOrgCookie } from '@/server/prefetch';

/**
 * App root (`/`). Not a page — a scope router. Reads the caller's orgs (reusing
 * the SSR-primed entry `_app.tsx` seeded) + the last-visited `ACTIVE_ORG` hint,
 * then `resolveRootTarget` decides where to land:
 *
 *  - org       → that org's home (`/organizations/{slug}`)
 *  - selector  → the org chooser (`/organizations`)
 *  - create    → forced onboarding (`/auth/create-org`) for zero-org callers
 *
 * The cookie is a hint, never authoritative — a stale value can only demote to
 * the selector (it's checked against the membership list), never cross into an
 * org the caller doesn't belong to.
 */
export const Route = createFileRoute('/_app/')({
  beforeLoad: async ({ context }) => {
    const data = await context.queryClient.ensureQueryData(
      accountOrgsQueryOptions(),
    );
    const orgs = toScopeOrgs(data);
    // Server pass reads the cookie via the server-fn; client reads it directly.
    const remembered =
      typeof window === 'undefined'
        ? await getActiveOrgCookie()
        : storage.get(ACTIVE_ORG);

    const target = resolveRootTarget(orgs, remembered);
    if (target.kind === 'create') {
      throw redirect({ to: '/auth/create-org' });
    }
    if (target.kind === 'selector') {
      throw redirect({ to: '/organizations' });
    }
    throw redirect({
      to: '/organizations/$organization',
      params: { organization: organizationId(target.organization) },
    });
  },
});
