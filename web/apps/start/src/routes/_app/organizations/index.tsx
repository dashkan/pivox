import { organizationId } from '@pivox/client';
import { ACTIVE_ORG, storage } from '@pivox/storage';
import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useMemo } from 'react';

import { $api } from '@/lib/api-client';
import { toScopeOrgs } from '@/lib/orgs-query';

export const Route = createFileRoute('/_app/organizations/')({
  component: OrgSelectorPage,
});

/** First two initials of the display name, uppercased; "?" when empty. */
function initials(name: string): string {
  const out = name
    .split(/\s+/)
    .map((p) => p[0])
    .filter(Boolean)
    .slice(0, 2)
    .join('')
    .toUpperCase();
  return out || '?';
}

/**
 * The org chooser (`/organizations`) — the collection index doubling as the
 * selector. App-themed (not the auth-card look): a centered grid of the caller's
 * orgs. Picking one writes the last-visited `ACTIVE_ORG` hint and navigates to
 * that org's home. Zero-org callers never reach here (root sends them to
 * create-org first). Orgs come from the SSR-primed list `_app.tsx` seeded.
 */
function OrgSelectorPage() {
  const navigate = useNavigate();
  const orgsQuery = $api.useQuery('get', '/v1/accounts/me/organizations', {
    params: { path: { parent: 'accounts/me' } },
  });
  const orgs = useMemo(
    () =>
      toScopeOrgs(orgsQuery.data).sort((a, b) =>
        a.displayName.localeCompare(b.displayName, undefined, {
          sensitivity: 'base',
        }),
      ),
    [orgsQuery.data],
  );

  const select = (organization: string) => {
    storage.set(ACTIVE_ORG, organization);
    void navigate({
      to: '/organizations/$organization',
      params: { organization: organizationId(organization) },
    });
  };

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-8 p-8">
      <div className="text-center">
        <h1 className="text-2xl font-semibold">Choose an organization</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Select which organization you want to work in.
        </p>
      </div>
      <div className="grid w-full max-w-3xl gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {orgs.map((org) => (
          <button
            key={org.organization}
            type="button"
            className="flex items-center gap-3 rounded-xl border bg-card p-4 text-left text-card-foreground shadow-sm transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            onClick={() => {
              select(org.organization);
            }}
          >
            <div className="flex aspect-square size-10 items-center justify-center rounded-lg bg-primary text-sm font-medium text-primary-foreground">
              {initials(org.displayName)}
            </div>
            <div className="min-w-0">
              <div className="truncate font-medium">{org.displayName}</div>
              <div className="truncate text-xs text-muted-foreground">
                {organizationId(org.organization)}
              </div>
            </div>
          </button>
        ))}
      </div>
    </div>
  );
}
