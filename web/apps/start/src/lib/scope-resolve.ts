import { organizationId } from '@pivox/client';

/** Minimal org shape the scope resolvers need: the resource name. */
export interface ScopeOrg {
  organization: string; // e.g. "organizations/acme"
}

/** Where the root route sends an authenticated user. */
export type RootTarget =
  | { kind: 'org'; organization: string } // redirect to this org's home
  | { kind: 'selector' } // show the org chooser
  | { kind: 'create' }; // forced create-org onboarding

/**
 * Resolve where `/` sends an authenticated user. Zero orgs → forced create-org
 * onboarding. Otherwise the remembered org (the last-visited cookie) if it's
 * still a membership → its home; a missing or stale remembered org → the org
 * selector. The cookie is a hint, never authoritative — a stale value can only
 * demote to the selector, never cross into an org the caller doesn't belong to.
 */
export function resolveRootTarget(
  orgs: readonly ScopeOrg[],
  rememberedOrg: string | null,
): RootTarget {
  if (orgs.length === 0) return { kind: 'create' };
  if (rememberedOrg && orgs.some((o) => o.organization === rememberedOrg)) {
    return { kind: 'org', organization: rememberedOrg };
  }
  return { kind: 'selector' };
}

/**
 * Resolve a route's `{organization}` slug against the caller's memberships, or
 * null when the slug is empty or names an org they don't belong to. The layout
 * maps null → notFound — never a silent fallback to another org.
 */
export function resolveOrgBySlug<T extends ScopeOrg>(
  orgs: readonly T[],
  slug: string,
): T | null {
  if (slug === '') return null;
  return orgs.find((o) => organizationId(o.organization) === slug) ?? null;
}
