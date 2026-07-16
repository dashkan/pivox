import { parseResourceName } from '@pivox/client';

/**
 * Flattened openapi path-param object for an AIP resource name.
 *
 * grpc-gateway flattens AIP bindings like `{parent=organizations/*}` into a
 * literal `/v1/organizations/{organization}/...`, so the URL wants the singular
 * segment key (`organization`), not the plural AIP collection (`organizations`).
 */
export type PathParams = Record<string, string>;

/**
 * AIP collection segment → the singular openapi path-param key the flattened
 * REST routes expose.
 */
const COLLECTION_TO_PARAM: Record<string, string> = {
  organizations: 'organization',
  spaces: 'space',
  workflows: 'workflow',
  versions: 'version',
  runs: 'run',
  connectors: 'connector',
  secrets: 'secret',
};

/**
 * Map an AIP resource (or parent) name to the openapi path params its REST
 * route expects.
 *
 *   `organizations/acme/workflows/wf1`
 *     → `{ organization: 'acme', workflow: 'wf1' }`
 *   `organizations/acme/spaces/main/connectors/c1`
 *     → `{ organization: 'acme', space: 'main', connector: 'c1' }`
 *
 * Throws on an unknown collection segment — the caller passed a name from a
 * collection this feature doesn't route.
 */
export function resourcePathParams(name: string): PathParams {
  const segments = parseResourceName(name);
  const out: PathParams = {};
  for (const [collection, id] of Object.entries(segments)) {
    const param = COLLECTION_TO_PARAM[collection];
    if (param === undefined) {
      throw new Error(
        `resourcePathParams: unknown collection "${collection}" in "${name}"`,
      );
    }
    out[param] = id;
  }
  return out;
}

/**
 * Whether a resource (or parent) name is space-scoped
 * (`organizations/*​/spaces/*​/...`) rather than org-scoped. Selects which
 * literal REST path variant a hook targets.
 */
export function isSpaceScoped(name: string): boolean {
  return parseResourceName(name).spaces !== undefined;
}
