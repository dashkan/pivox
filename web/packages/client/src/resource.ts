/**
 * Resource-name helpers — AIP resource names are
 * `collection/id[/collection/id]*` (e.g. `organizations/acme`,
 * `organizations/acme/spaces/main`). The Pivox proto carries them in
 * canonical form everywhere — request fields, response fields,
 * resource references.
 *
 * The grpc-gateway → OpenAPI translation flattens AIP path bindings
 * like `/v1/{parent=organizations/*}/spaces` into a literal
 * `/v1/organizations/{organization}/spaces` — `{organization}` is
 * now just the slug, not the full name. So anywhere we hold a
 * resource name in state and feed it to an openapi-fetch path
 * param, we need to split it down to the segment the URL wants.
 *
 * These helpers are the boundary. State stays canonical (full
 * resource names); URL construction asks for the specific segment.
 */

/**
 * Parse an AIP resource name into a collection→id map.
 *
 * `organizations/acme/spaces/main` → `{ organizations: 'acme', spaces: 'main' }`
 *
 * Tolerates an empty input by returning an empty map (callers can
 * use the returned `organizations`/`spaces` keys as `string | undefined`).
 * Throws on a malformed name (odd number of segments) — that's a
 * programmer error, not a runtime input.
 */
export function parseResourceName(name: string): Record<string, string> {
  if (name === '') return {};
  const parts = name.split('/');
  if (parts.length % 2 !== 0) {
    throw new Error(
      `parseResourceName: malformed resource name "${name}" — ` +
        `AIP names have an even number of segments (collection/id pairs)`,
    );
  }
  const out: Record<string, string> = {};
  for (let i = 0; i < parts.length; i += 2) {
    const collection = parts[i];
    const id = parts[i + 1];
    if (!collection || !id) {
      throw new Error(
        `parseResourceName: empty segment in "${name}" at index ${i}`,
      );
    }
    // Reject duplicate collection segments — they'd silently last-win
    // and the Record<string, string> return shape gives callers no
    // way to detect the collision. AIP doesn't define semantics for
    // a name like `organizations/a/organizations/b`; better to fail
    // loudly than to return ambiguous data.
    if (collection in out) {
      throw new Error(
        `parseResourceName: duplicate collection "${collection}" in "${name}"`,
      );
    }
    out[collection] = id;
  }
  return out;
}

/**
 * Extract the `organizations/{id}` slug from any resource name that
 * carries one. Returns `''` for empty input — callers can guard with
 * `enabled: !!organizationId(...)` on react-query.
 *
 * Examples:
 *   organizationId('organizations/acme')                → 'acme'
 *   organizationId('organizations/acme/spaces/main')    → 'acme'
 *   organizationId('')                                  → ''
 */
export function organizationId(name: string): string {
  if (name === '') return '';
  return parseResourceName(name).organizations ?? '';
}

/**
 * Extract the `spaces/{id}` slug from any resource name that
 * carries one. Same semantics as {@link organizationId}.
 */
export function spaceId(name: string): string {
  if (name === '') return '';
  return parseResourceName(name).spaces ?? '';
}
