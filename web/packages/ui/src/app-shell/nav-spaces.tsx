/**
 * Shape a space needs to be listable in the scope picker. Maps to the
 * subset of fields the Space proto from `/v1/organizations/
 * {organization}/spaces` actually carries. `icon` is client-side
 * derived — the API doesn't ship one.
 *
 * The old sidebar "Spaces" nav group was removed once the scope picker
 * subsumed space selection (the URL owns scope now); this interface
 * survives as the shared space shape the picker + feature hook consume.
 *
 * `href` is retained for callers that still route to a space landing
 * page; the scope picker itself navigates via the injected handlers.
 */
export interface NavSpacesSpace {
  /** Resource name, e.g. "organizations/acme/spaces/dev". */
  space: string;
  displayName: string;
  href: string;
  icon?: React.ReactNode;
}
