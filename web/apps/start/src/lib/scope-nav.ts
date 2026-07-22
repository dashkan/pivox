/**
 * Space-select coherence for the URL-scoped route tree.
 *
 * When the scope picker changes the active space, the user must stay on the
 * CURRENT resource — picking a space on the connectors list navigates to the
 * space connectors, on the secrets list to the space secrets. The resource is
 * read off the current pathname (the URL is the single source of scope truth),
 * and the target route is derived here as a plain string so the derivation is
 * pure and unit-testable, decoupled from the router.
 */

/** Resource families reachable under the URL-scoped route tree. */
export type ScopedResource = 'connectors' | 'secrets' | 'workflows';

/**
 * The resource family a scoped app pathname addresses, read off its path
 * segments. A scoped path carries exactly one of these keywords as a segment
 * (`.../connectors`, `.../secrets`, `.../workflows`). Null when the path is not
 * under a known resource (e.g. the org home) — callers fall back to a default.
 */
export function resourceFromPathname(pathname: string): ScopedResource | null {
  // Read the resource at its FIXED structural position, not by scanning the
  // whole path — an org/space slug (both free-form) named after a resource must
  // not be mistaken for it:
  //   /organizations/{org}/{resource}...
  //   /organizations/{org}/spaces/{space}/{resource}...
  const s = pathname.split('/').filter(Boolean);
  if (s[0] !== 'organizations' || !s[1]) return null;
  const resource = s[2] === 'spaces' ? s[4] : s[2];
  if (
    resource === 'connectors' ||
    resource === 'secrets' ||
    resource === 'workflows'
  ) {
    return resource;
  }
  return null;
}

/**
 * Where selecting a space (or "All spaces", `spaceSlug = null`) from the scope
 * picker navigates: the same resource's scoped variant. Every resource
 * (connectors, secrets, workflows) is org-or-space scoped, so the resource is
 * preserved and only the scope changes. Off a known resource, defaults to
 * connectors.
 */
export function spaceNavTarget(
  pathname: string,
  orgSlug: string,
  spaceSlug: string | null,
): string {
  const resource = resourceFromPathname(pathname) ?? 'connectors';
  return spaceSlug
    ? `/organizations/${orgSlug}/spaces/${spaceSlug}/${resource}`
    : `/organizations/${orgSlug}/${resource}`;
}

/**
 * Where selecting an ORGANIZATION from the picker navigates: the same resource
 * section in the new org, at org-rollup scope — a space can't carry across orgs.
 * Falls back to the org home when the path is not under a known resource.
 */
export function orgNavTarget(pathname: string, orgSlug: string): string {
  const resource = resourceFromPathname(pathname);
  return resource
    ? `/organizations/${orgSlug}/${resource}`
    : `/organizations/${orgSlug}`;
}
