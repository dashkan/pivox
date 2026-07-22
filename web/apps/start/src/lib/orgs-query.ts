import { $api } from '@/lib/api-client';

import type { ScopeOrg } from '@/lib/scope-resolve';
import type { components } from '@pivox/client/types';

type ListAccountOrganizationsResponse =
  components['schemas']['v1ListAccountOrganizationsResponse'];

/**
 * The caller's org-list queryOptions — the SINGLE definition shared by `_app.tsx`
 * (SSR prime), the root redirect, and the `$organization` layout resolver. Using
 * one builder guarantees the byte-identical react-query key across all three, so
 * `ensureQueryData` reuses the SSR-primed entry instead of firing a fetch (which
 * would fail on the SSR pass — the browser apiClient has no usable base there).
 */
export function accountOrgsQueryOptions() {
  return $api.queryOptions('get', '/v1/accounts/me/organizations', {
    params: { path: { parent: 'accounts/me' } },
  });
}

/** Project the org-list response into the `{ organization, displayName }` shape the resolvers use. */
export function toScopeOrgs(
  data: ListAccountOrganizationsResponse | undefined,
): (ScopeOrg & { displayName: string })[] {
  return (data?.accountOrganizations ?? [])
    .filter(
      (o): o is { organization: string; displayName?: string } =>
        typeof o.organization === 'string' && o.organization.length > 0,
    )
    .map((o) => ({
      organization: o.organization,
      displayName: o.displayName ?? o.organization,
    }));
}

/** The spaces-list queryOptions for one org — shared by the space layout resolver + prime. */
export function orgSpacesQueryOptions(orgSlug: string) {
  return $api.queryOptions('get', '/v1/organizations/{organization}/spaces', {
    params: { path: { organization: orgSlug } },
  });
}
