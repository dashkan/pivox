import { organizationId } from '@pivox/client';
import { ACTIVE_ORG } from '@pivox/storage';
import { useStorageValue } from '@pivox/storage/react';
import { useParams, useRouterState } from '@tanstack/react-router';
import { ShieldIcon, WorkflowIcon } from 'lucide-react';

import type { NavMainItem } from '@pivox/ui/app-shell';

/**
 * Sidebar nav groups. Connectors moved under the scoped `/organizations/$org/...`
 * tree, so its link needs the active org's slug: the route's `$organization`
 * param when on a scoped route, else the LIVE last-visited cookie (SSR-seeded via
 * `useStorageValue`, then reactive — the shell writes it on every org nav).
 *
 * Deriving from the FROZEN SSR context snapshot instead left the Connectors link
 * stale → `/organizations` on flat routes and after any client-side org switch.
 * `use-nav-main.test.tsx` pins that regression.
 */
export function useNavMain(
  initialActiveOrganization: string | null,
): NavMainItem[] {
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const params = useParams({ strict: false }) as {
    organization?: string;
    space?: string;
  };
  const liveActiveOrg = useStorageValue(ACTIVE_ORG, initialActiveOrganization);
  const navOrgSlug =
    params.organization ??
    (liveActiveOrg ? organizationId(liveActiveOrg) : undefined);
  // Every admin resource is org-or-space scoped. Keep the active org AND the
  // current space (when in one) so navigating between areas stays in the space
  // instead of dropping to the org rollup. Falls back to the selector with no org.
  const scoped = (segment: string) => {
    if (!navOrgSlug) return '/organizations';
    return params.space
      ? `/organizations/${navOrgSlug}/spaces/${params.space}/${segment}`
      : `/organizations/${navOrgSlug}/${segment}`;
  };
  return [
    {
      title: 'Workflows',
      href: scoped('workflows'),
      icon: <WorkflowIcon />,
      isActive: /\/(workflows|connectors)(\/|$)/.test(pathname),
      items: [
        { title: 'Definitions', href: scoped('workflows') },
        { title: 'Connectors', href: scoped('connectors') },
      ],
    },
    {
      title: 'Admin',
      href: scoped('secrets'),
      icon: <ShieldIcon />,
      isActive: /\/secrets(\/|$)/.test(pathname),
      items: [{ title: 'Secrets', href: scoped('secrets') }],
    },
  ];
}
