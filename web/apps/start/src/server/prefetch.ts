/**
 * Server-side prefetch helpers for SSR route loaders.
 *
 * `_app.tsx`'s `beforeLoad` runs once on the SSR pass to populate
 * the route's QueryClient before React render. Each helper here
 * encapsulates one prefetch: resolve the user's Keycloak access
 * token from the session cookie, fetch the underlying API as that
 * user, and return the response shape the client's `useQuery`
 * expects. `beforeLoad` calls `queryClient.setQueryData(...)` with
 * the result, keyed by the same key the client-side
 * `$api.queryOptions(...)` produces — so the client's hooks render
 * with hot data on first paint.
 *
 * createServerFn-wrapped so the bundler keeps these out of the
 * client build. Importing `prefetchOrgsForCurrentUser` from a route
 * file is safe because the client sees an RPC stub, not the actual
 * server code.
 *
 * Errors are caught and returned as `null`. A failed prefetch must
 * NOT fail the SSR render — the client-side `useQuery` will retry
 * on hydration and the user gets a brief skeleton instead of a
 * broken page.
 */

import { organizationId } from '@pivox/client';
import {
  buildConnectorsListRequest,
  fetchAgentOptions,
} from '@pivox/features/connectors';
import { buildSecretsListRequest } from '@pivox/features/secrets';
import { buildWorkflowsListRequest } from '@pivox/features/workflows';
import { ACTIVE_ORG } from '@pivox/storage';
import { createServerFn } from '@tanstack/react-start';
import { getCookie } from '@tanstack/react-start/server';

import { searchToValue, type ConnectorsSearch } from '../lib/connectors-search';
import {
  searchToValue as secretsSearchToValue,
  type SecretsSearch,
} from '../lib/secrets-search';
import {
  searchToValue as workflowsSearchToValue,
  type WorkflowsSearch,
} from '../lib/workflows-search';

import { getSsrAccessToken } from './oidc/ssr-token';
import { createServerApiClient } from './pivox-server-api';

import type {
  ConnectorsListQuery,
  ConnectorsListRequest,
} from '@pivox/features/connectors';
import type {
  SecretsListQuery,
  SecretsListRequest,
} from '@pivox/features/secrets';
import type { WorkflowsListQuery } from '@pivox/features/workflows';
import type { AgentOption } from '@pivox/ui/resource-admin';
import type { components } from '@pivox/client/types';

/**
 * Slim wire-shape of `/v1/accounts/me/organizations`. Matches what
 * the client's `$api.useQuery` returns and what we feed into
 * `queryClient.setQueryData`. Schema name is grpc-gateway's
 * standard `v1` prefix + the proto type name.
 */
export type ListAccountOrganizationsResponse =
  components['schemas']['v1ListAccountOrganizationsResponse'];

/**
 * Slim wire-shape of `/v1/organizations/{organization}/spaces`.
 * Same hydration role as ListAccountOrganizationsResponse — primes
 * the route's QueryClient with data the client's useQuery picks up
 * by matching key.
 */
export type ListSpacesResponse =
  components['schemas']['v1ListSpacesResponse'];

/** Slim wire-shape of the connectors list responses (org rollup + space). */
export type ListConnectorsResponse =
  components['schemas']['v1ListConnectorsResponse'];

const CONNECTORS_PATH = '/v1/organizations/{organization}/connectors' as const;
const SPACE_CONNECTORS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors' as const;
const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

/**
 * Result of prefetchConnectors. Carries the built request so the loader can
 * reproduce the exact react-query key (via `$api.queryOptions`) the client hook
 * uses — the byte-identical key is what makes the primed data hydrate instead of
 * silently refetching. Null whenever SSR can't fetch (no session / active org).
 */
export type PrefetchedConnectors = ConnectorsListRequest & {
  orgSlug: string;
  query: ConnectorsListQuery;
  connectors: ListConnectorsResponse;
};

/**
 * prefetchConnectors server-fn: reads the active-org cookie, builds the SAME
 * list request the client hook builds (via the shared `buildConnectorsListRequest`),
 * and GETs the org-rollup or space-scoped connectors path per the URL scope.
 * Returns null on any failure — SSR must never throw; the client useQuery
 * retries on hydration.
 */
export const prefetchConnectors = createServerFn({ method: 'GET' })
  .validator((search: ConnectorsSearch): ConnectorsSearch => search)
  .handler(async ({ data }): Promise<PrefetchedConnectors | null> => {
    const accessToken = await getSsrAccessToken();
    if (!accessToken) return null;

    const activeOrg = getCookie(ACTIVE_ORG.name);
    if (!activeOrg) return null;

    try {
      const orgSlug = organizationId(activeOrg);
      if (!orgSlug) return null;

      const req = buildConnectorsListRequest(orgSlug, searchToValue(data));
      const client = createServerApiClient(accessToken);

      if (req.isSpaceScoped) {
        const { data: body, response } = await client.GET(SPACE_CONNECTORS_PATH, {
          params: { path: req.pathParams, query: req.query },
        });
        if (!body) {
          console.warn('[ssr-prefetch] connectors: space non-2xx or empty', {
            status: response.status,
            orgSlug,
          });
          return null;
        }
        return {
          orgSlug,
          isSpaceScoped: true,
          pathParams: req.pathParams,
          query: req.query,
          connectors: body,
        };
      }

      const { data: body, response } = await client.GET(CONNECTORS_PATH, {
        params: { path: { organization: orgSlug }, query: req.query },
      });
      if (!body) {
        console.warn('[ssr-prefetch] connectors: org non-2xx or empty', {
          status: response.status,
          orgSlug,
        });
        return null;
      }
      return {
        orgSlug,
        isSpaceScoped: false,
        pathParams: req.pathParams,
        query: req.query,
        connectors: body,
      };
    } catch (err) {
      console.warn('[ssr-prefetch] connectors: threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  });

/** Wire-shape of a single connector, primed for the routed edit page. */
export type ConnectorRecord = components['schemas']['v1Connector'];

/** Which connector the edit route wants: its leaf id + optional space slug. */
export interface PrefetchConnectorInput {
  connectorId: string;
  /** Space slug for a space-scoped connector; absent = org-direct. */
  space?: string;
}

/**
 * Result of prefetchConnector. Carries `orgSlug` + `space` + `connectorId` so
 * the edit loader can reproduce the exact `$api.queryOptions` key the client
 * hook (`useConnectorForm`) reads — the byte-identical key is what makes the
 * primed record hydrate instead of firing an XHR on load. Null on any failure.
 */
export type PrefetchedConnector = {
  orgSlug: string;
  space: string | undefined;
  connectorId: string;
  connector: ConnectorRecord;
} | null;

/**
 * prefetchConnector server-fn: reads the active-org cookie, then GETs the single
 * connector (org-direct or space-scoped per `space`) as the user, mirroring
 * prefetchConnectors for the list. Returns null on any failure — SSR must never
 * throw; the client `useConnectorForm` query retries on hydration.
 */
export const prefetchConnector = createServerFn({ method: 'GET' })
  .validator((input: PrefetchConnectorInput): PrefetchConnectorInput => input)
  .handler(async ({ data }): Promise<PrefetchedConnector> => {
    const accessToken = await getSsrAccessToken();
    if (!accessToken) return null;

    const activeOrg = getCookie(ACTIVE_ORG.name);
    if (!activeOrg) return null;

    try {
      const orgSlug = organizationId(activeOrg);
      if (!orgSlug) return null;

      const client = createServerApiClient(accessToken);

      if (data.space) {
        const { data: body, response } = await client.GET(SPACE_CONNECTOR_PATH, {
          params: {
            path: {
              organization: orgSlug,
              space: data.space,
              connector: data.connectorId,
            },
          },
        });
        if (!body) {
          console.warn('[ssr-prefetch] connector: space non-2xx or empty', {
            status: response.status,
            orgSlug,
          });
          return null;
        }
        return {
          orgSlug,
          space: data.space,
          connectorId: data.connectorId,
          connector: body,
        };
      }

      const { data: body, response } = await client.GET(CONNECTOR_PATH, {
        params: { path: { organization: orgSlug, connector: data.connectorId } },
      });
      if (!body) {
        console.warn('[ssr-prefetch] connector: org non-2xx or empty', {
          status: response.status,
          orgSlug,
        });
        return null;
      }
      return {
        orgSlug,
        space: undefined,
        connectorId: data.connectorId,
        connector: body,
      };
    } catch (err) {
      console.warn('[ssr-prefetch] connector: threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  });

/** Result of prefetchConnectorAgents: the org the options belong to + the options. */
export interface PrefetchedConnectorAgents {
  orgSlug: string;
  options: AgentOption[];
}

/**
 * prefetchConnectorAgents server-fn: reads the active-org cookie itself (rather
 * than taking orgSlug as input) so the connectors loader can run it in parallel
 * with prefetchConnectors instead of waterfalling on that result. Fans out
 * gateways → agents for the active org (via the shared `fetchAgentOptions`) so
 * the page's agent options are SSR-primed and no gateways/agents XHR fires on
 * load. Returns the resolved orgSlug alongside the options so the loader can key
 * the primed data. Null on any failure — the client's composite query then
 * fetches on hydration.
 */
export const prefetchConnectorAgents = createServerFn({
  method: 'GET',
}).handler(async (): Promise<PrefetchedConnectorAgents | null> => {
  const accessToken = await getSsrAccessToken();
  if (!accessToken) return null;

  const activeOrg = getCookie(ACTIVE_ORG.name);
  if (!activeOrg) return null;

  try {
    const orgSlug = organizationId(activeOrg);
    if (!orgSlug) return null;

    const client = createServerApiClient(accessToken);
    return { orgSlug, options: await fetchAgentOptions(client, orgSlug) };
  } catch (err) {
    console.warn('[ssr-prefetch] connector-agents: threw', {
      message: err instanceof Error ? err.message : String(err),
    });
    return null;
  }
});

/** Slim wire-shape of the secrets list responses (org rollup + space). */
export type ListSecretsResponse =
  components['schemas']['v1ListSecretsResponse'];

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SPACE_SECRETS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets' as const;
const SECRET_PATH =
  '/v1/organizations/{organization}/secrets/{secret}' as const;
const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}' as const;

/**
 * Result of prefetchSecrets. Carries the built request so the loader can
 * reproduce the exact react-query key (via `$api.queryOptions`) the client hook
 * uses — the secret twin of PrefetchedConnectors. Null whenever SSR can't fetch.
 */
export type PrefetchedSecrets = SecretsListRequest & {
  orgSlug: string;
  query: SecretsListQuery;
  secrets: ListSecretsResponse;
};

/**
 * prefetchSecrets server-fn: reads the active-org cookie, builds the SAME list
 * request the client hook builds (via the shared `buildSecretsListRequest`), and
 * GETs the org-rollup or space-scoped secrets path per the URL scope. Returns
 * null on any failure — SSR must never throw; the client useQuery retries on
 * hydration.
 */
export const prefetchSecrets = createServerFn({ method: 'GET' })
  .validator((search: SecretsSearch): SecretsSearch => search)
  .handler(async ({ data }): Promise<PrefetchedSecrets | null> => {
    const accessToken = await getSsrAccessToken();
    if (!accessToken) return null;

    const activeOrg = getCookie(ACTIVE_ORG.name);
    if (!activeOrg) return null;

    try {
      const orgSlug = organizationId(activeOrg);
      if (!orgSlug) return null;

      const req = buildSecretsListRequest(orgSlug, secretsSearchToValue(data));
      const client = createServerApiClient(accessToken);

      if (req.isSpaceScoped) {
        const { data: body, response } = await client.GET(SPACE_SECRETS_PATH, {
          params: { path: req.pathParams, query: req.query },
        });
        if (!body) {
          console.warn('[ssr-prefetch] secrets: space non-2xx or empty', {
            status: response.status,
            orgSlug,
          });
          return null;
        }
        return {
          orgSlug,
          isSpaceScoped: true,
          pathParams: req.pathParams,
          query: req.query,
          secrets: body,
        };
      }

      const { data: body, response } = await client.GET(SECRETS_PATH, {
        params: { path: { organization: orgSlug }, query: req.query },
      });
      if (!body) {
        console.warn('[ssr-prefetch] secrets: org non-2xx or empty', {
          status: response.status,
          orgSlug,
        });
        return null;
      }
      return {
        orgSlug,
        isSpaceScoped: false,
        pathParams: req.pathParams,
        query: req.query,
        secrets: body,
      };
    } catch (err) {
      console.warn('[ssr-prefetch] secrets: threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  });

/** Wire-shape of a single secret, primed for the routed edit page. */
export type SecretRecord = components['schemas']['v1Secret'];

/** Which secret the edit route wants: its leaf id + optional space slug. */
export interface PrefetchSecretInput {
  secretId: string;
  /** Space slug for a space-scoped secret; absent = org-direct. */
  space?: string;
}

/**
 * Result of prefetchSecret. Carries `orgSlug` + `space` + `secretId` so the edit
 * loader can reproduce the exact `$api.queryOptions` key the client hook
 * (`useSecretForm`) reads. Null on any failure — the secret twin of
 * PrefetchedConnector.
 */
export type PrefetchedSecret = {
  orgSlug: string;
  space: string | undefined;
  secretId: string;
  secret: SecretRecord;
} | null;

/**
 * prefetchSecret server-fn: reads the active-org cookie, then GETs the single
 * secret (org-direct or space-scoped per `space`) as the user, mirroring
 * prefetchConnector. The value is INPUT_ONLY and never returned, so the primed
 * record is metadata only. Returns null on any failure.
 */
export const prefetchSecret = createServerFn({ method: 'GET' })
  .validator((input: PrefetchSecretInput): PrefetchSecretInput => input)
  .handler(async ({ data }): Promise<PrefetchedSecret> => {
    const accessToken = await getSsrAccessToken();
    if (!accessToken) return null;

    const activeOrg = getCookie(ACTIVE_ORG.name);
    if (!activeOrg) return null;

    try {
      const orgSlug = organizationId(activeOrg);
      if (!orgSlug) return null;

      const client = createServerApiClient(accessToken);

      if (data.space) {
        const { data: body, response } = await client.GET(SPACE_SECRET_PATH, {
          params: {
            path: {
              organization: orgSlug,
              space: data.space,
              secret: data.secretId,
            },
          },
        });
        if (!body) {
          console.warn('[ssr-prefetch] secret: space non-2xx or empty', {
            status: response.status,
            orgSlug,
          });
          return null;
        }
        return {
          orgSlug,
          space: data.space,
          secretId: data.secretId,
          secret: body,
        };
      }

      const { data: body, response } = await client.GET(SECRET_PATH, {
        params: { path: { organization: orgSlug, secret: data.secretId } },
      });
      if (!body) {
        console.warn('[ssr-prefetch] secret: org non-2xx or empty', {
          status: response.status,
          orgSlug,
        });
        return null;
      }
      return {
        orgSlug,
        space: undefined,
        secretId: data.secretId,
        secret: body,
      };
    } catch (err) {
      console.warn('[ssr-prefetch] secret: threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  });

/** Slim wire-shape of the workflows list response (org-direct only). */
export type ListWorkflowsResponse =
  components['schemas']['v1ListWorkflowsResponse'];

const WORKFLOWS_PATH = '/v1/organizations/{organization}/workflows' as const;

/**
 * Result of prefetchWorkflows. Carries the org slug + built query so the loader
 * can reproduce the exact react-query key (via `$api.queryOptions`) the client
 * hook uses — the byte-identical key is what makes the primed rows hydrate
 * instead of silently refetching. Workflows are org-direct only, so there is no
 * space-scope variant. Null whenever SSR can't fetch.
 */
export type PrefetchedWorkflows = {
  orgSlug: string;
  query: WorkflowsListQuery;
  workflows: ListWorkflowsResponse;
};

/**
 * prefetchWorkflows server-fn: reads the active-org cookie, builds the SAME list
 * request the client hook builds (via the shared `buildWorkflowsListRequest`),
 * and GETs the org-direct workflows path. Returns null on any failure — SSR must
 * never throw; the client useQuery retries on hydration.
 */
export const prefetchWorkflows = createServerFn({ method: 'GET' })
  .validator((search: WorkflowsSearch): WorkflowsSearch => search)
  .handler(async ({ data }): Promise<PrefetchedWorkflows | null> => {
    const accessToken = await getSsrAccessToken();
    if (!accessToken) return null;

    const activeOrg = getCookie(ACTIVE_ORG.name);
    if (!activeOrg) return null;

    try {
      const orgSlug = organizationId(activeOrg);
      if (!orgSlug) return null;

      const req = buildWorkflowsListRequest(orgSlug, workflowsSearchToValue(data));
      const client = createServerApiClient(accessToken);

      const { data: body, response } = await client.GET(WORKFLOWS_PATH, {
        params: { path: { organization: orgSlug }, query: req.query },
      });
      if (!body) {
        console.warn('[ssr-prefetch] workflows: org non-2xx or empty', {
          status: response.status,
          orgSlug,
        });
        return null;
      }
      return { orgSlug, query: req.query, workflows: body };
    } catch (err) {
      console.warn('[ssr-prefetch] workflows: threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  });

/**
 * getActiveOrgCookie server-fn: returns the SSR-time value of the
 * active-org cookie, or null if absent / malformed.
 *
 * Exposed as a server fn (rather than a direct
 * `getCookie(ACTIVE_ORG.name)` call in beforeLoad) because
 * `_app.tsx` is shared SSR+client code and TanStack Start's
 * import-protection plugin blocks `@tanstack/react-start/server`
 * imports from any module the client bundle reaches. Wrapping the
 * read in a server fn keeps the import isolated to this
 * server-only file; the client sees only the RPC stub.
 */
export const getActiveOrgCookie = createServerFn({ method: 'GET' }).handler(
  (): string | null => {
    try {
      const v = getCookie(ACTIVE_ORG.name);
      return v ? ACTIVE_ORG.parse(v) : null;
    } catch (err) {
      // getCookie shouldn't throw under normal h3 flow, but defensive
      // here matches the surrounding prefetch fns — beforeLoad must
      // never fail because the active-org cookie read tripped.
      console.warn('[ssr-prefetch] active-org cookie read threw', {
        message: err instanceof Error ? err.message : String(err),
      });
      return null;
    }
  },
);

/**
 * prefetchOrgsForCurrentUser server-fn: fetches the caller's org
 * list using the user's Keycloak access token (from the session
 * cookie). Returns the response body on success, `null` on any
 * failure (no session, gateway error). `null` is the signal to skip
 * cache priming — client-side useQuery will pick up.
 */
export const prefetchOrgsForCurrentUser = createServerFn({
  method: 'GET',
}).handler(async (): Promise<ListAccountOrganizationsResponse | null> => {
  const accessToken = await getSsrAccessToken();
  if (!accessToken) {
    // No usable session → unauthed visit (auth gate redirects) or a
    // refresh that couldn't complete. Nothing to prime.
    return null;
  }

  try {
    const client = createServerApiClient(accessToken);
    const { data, response } = await client.GET(
      '/v1/accounts/me/organizations',
      { params: { path: { parent: 'accounts/me' } } },
    );
    if (!data) {
      // openapi-fetch returns data=undefined on non-2xx — log
      // the status so misconfigured backends (wrong audience,
      // SA not allowlisted, etc.) are diagnosable.
      console.warn('[ssr-prefetch] orgs: gateway non-2xx or empty body', {
        status: response.status,
      });
      return null;
    }
    return data;
  } catch (err) {
    // Most likely: PIVOX_API_URL missing so createServerApiClient
    // throws. Surface the message so the operator can see why SSR
    // prefetch is degrading to CSR.
    console.warn('[ssr-prefetch] orgs: threw', {
      message: err instanceof Error ? err.message : String(err),
    });
    return null;
  }
});

/**
 * Result of prefetchSpacesForActiveOrg. Returns null whenever the
 * server can't determine an active org (no cookie, no session,
 * malformed cookie value); the orgSlug field is non-null on success
 * so beforeLoad can reuse it when constructing the matching
 * queryKey via `$api.queryOptions(...)`.
 */
export interface PrefetchedSpaces {
  orgSlug: string;
  spaces: ListSpacesResponse;
}

/**
 * prefetchSpacesForActiveOrg server-fn: reads the active-org cookie
 * (`pivox.active-organization`, written client-side by the org
 * picker), mints an actor JWT for the verified user, and fetches
 * `/v1/organizations/{org}/spaces` for that org.
 *
 * Returns `null` when there's no active-org cookie — first-time
 * visitors, sign-out, freshly-created accounts. The shell's
 * spaces section renders skeleton in that case; the client picks
 * an active org after orgs load and the client-side useQuery fires
 * naturally.
 *
 * Errors swallow to null for the same SSR-shouldn't-fail reasoning
 * as prefetchOrgsForCurrentUser.
 */
export const prefetchSpacesForActiveOrg = createServerFn({
  method: 'GET',
}).handler(async (): Promise<PrefetchedSpaces | null> => {
  const accessToken = await getSsrAccessToken();
  if (!accessToken) return null;

  const activeOrg = getCookie(ACTIVE_ORG.name);
  if (!activeOrg) return null;

  try {
    const orgSlug = organizationId(activeOrg);
    if (!orgSlug) {
      console.warn('[ssr-prefetch] spaces: active-org cookie parsed empty', {
        value: activeOrg,
      });
      return null;
    }

    const client = createServerApiClient(accessToken);
    const { data, response } = await client.GET(
      '/v1/organizations/{organization}/spaces',
      { params: { path: { organization: orgSlug } } },
    );
    if (!data) {
      console.warn('[ssr-prefetch] spaces: gateway non-2xx or empty body', {
        status: response.status,
        orgSlug,
      });
      return null;
    }
    return { orgSlug, spaces: data };
  } catch (err) {
    console.warn('[ssr-prefetch] spaces: threw', {
      message: err instanceof Error ? err.message : String(err),
    });
    return null;
  }
});
