import { ConnectorsFeature, fetchAgentOptions } from '@pivox/features/connectors';
import { connectorSpaceSlug, leafId } from '@pivox/ui/resource-admin';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import type {
  Connector,
  ListControlsChange,
} from '@pivox/ui/resource-admin';

import { $api, apiClient } from '@/lib/api-client';
import { connectorAgentsQueryKey } from '@/lib/connector-agents-query';
import {
  searchToValue,
  valueToSearch,
  type ConnectorsSearch,
} from '@/lib/connectors-search';

// Agent membership changes rarely; keep the SSR-primed options fresh so no
// gateways/agents XHR fires on page load (a stale-time-0 hydration would refetch).
const AGENTS_STALE_TIME = 5 * 60 * 1000;

/** valueToSearch, dropping `scope` — scope lives in the PATH for these routes. */
function listSearch(value: Parameters<typeof valueToSearch>[0]): ConnectorsSearch {
  const { scope: _scope, ...rest } = valueToSearch(value);
  return rest;
}

/**
 * The single shared connectors LIST feature for the scope-in-URL routes. Both the
 * org-rollup route (`/organizations/$organization/connectors`) and the
 * space-scoped route (`.../spaces/$space/connectors`) render this, passing the
 * org slug and — for the space route — the space slug from their PATH params. The
 * URL is the single source of truth for scope, so:
 *
 *  - `parent` is always the org (`organizations/{slug}`); the space narrows the
 *    list via the controls' `scope`, forced here from `spaceSlug` (not search).
 *  - the in-toolbar scope selector's changes NAVIGATE between the two routes
 *    rather than mutating a `?scope=` param — keeping the path authoritative.
 *  - create/edit navigate to the scoped routed pages, carrying `?from=` so the
 *    form can return to this exact filtered/sorted/paged view. Edit targets the
 *    connector's OWN scope (org-direct vs its space), read off its resource name.
 */
export function ScopedConnectorsList({
  orgSlug,
  spaceSlug,
  search,
}: {
  orgSlug: string;
  /** Present on the space-scoped route; absent on the org rollup. */
  spaceSlug?: string;
  search: ConnectorsSearch;
}) {
  const navigate = useNavigate();

  // Single composite agent-options query, SSR-primed by the loader under the same
  // key. `enabled` is always true here (orgSlug is guaranteed by the route).
  const agentsQuery = useQuery({
    queryKey: connectorAgentsQueryKey(orgSlug),
    queryFn: () => fetchAgentOptions(apiClient, orgSlug),
    staleTime: AGENTS_STALE_TIME,
  });
  const agentOptions = useMemo(() => agentsQuery.data ?? [], [agentsQuery.data]);

  // Controls value: search params + the scope pinned from the path param.
  const listState = useMemo(
    () => ({ ...searchToValue(search), scope: spaceSlug ?? '' }),
    [search, spaceSlug],
  );

  const onListStateChange = useCallback<ListControlsChange>(
    (next, opts) => {
      const nextScope = next.scope;
      const currentScope = spaceSlug ?? '';
      const nextSearch = listSearch(next);
      // A scope change is a ROUTE change (path owns scope); everything else
      // updates search on the current route. Discrete changes push a history
      // entry; debounced search text replaces so keystrokes don't clutter it.
      if (nextScope !== currentScope) {
        if (nextScope) {
          void navigate({
            to: '/organizations/$organization/spaces/$space/connectors',
            params: { organization: orgSlug, space: nextScope },
            search: nextSearch,
          });
        } else {
          void navigate({
            to: '/organizations/$organization/connectors',
            params: { organization: orgSlug },
            search: nextSearch,
          });
        }
        return;
      }
      if (spaceSlug) {
        void navigate({
          to: '/organizations/$organization/spaces/$space/connectors',
          params: { organization: orgSlug, space: spaceSlug },
          search: nextSearch,
          replace: opts.history === 'replace',
        });
      } else {
        void navigate({
          to: '/organizations/$organization/connectors',
          params: { organization: orgSlug },
          search: nextSearch,
          replace: opts.history === 'replace',
        });
      }
    },
    [navigate, orgSlug, spaceSlug],
  );

  // Capture THIS list view's exact URL so the form pages can return to it
  // (filters/scope/page/scroll are already encoded here). The `?from=` param is
  // the single source both create-scope and return-target flow from.
  const currentHref = useRouterState({
    select: (s) => s.location.pathname + s.location.searchStr,
  });

  const onCreate = useCallback(() => {
    if (spaceSlug) {
      void navigate({
        to: '/organizations/$organization/spaces/$space/connectors/new',
        params: { organization: orgSlug, space: spaceSlug },
        search: { from: currentHref },
      });
    } else {
      void navigate({
        to: '/organizations/$organization/connectors/new',
        params: { organization: orgSlug },
        search: { from: currentHref },
      });
    }
  }, [navigate, orgSlug, spaceSlug, currentHref]);

  const onEdit = useCallback(
    (connector: Connector) => {
      // Edit targets the connector's OWN scope (it may be space-scoped even in an
      // org rollup), read off its resource name — not the route's scope.
      const connectorSpace = connectorSpaceSlug(connector.name);
      const connectorId = leafId(connector.name);
      if (connectorSpace) {
        void navigate({
          to: '/organizations/$organization/spaces/$space/connectors/$connectorId/edit',
          params: { organization: orgSlug, space: connectorSpace, connectorId },
          search: { from: currentHref },
        });
      } else {
        void navigate({
          to: '/organizations/$organization/connectors/$connectorId/edit',
          params: { organization: orgSlug, connectorId },
          search: { from: currentHref },
        });
      }
    },
    [navigate, orgSlug, currentHref],
  );

  return (
    <ConnectorsFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      listState={listState}
      onListStateChange={onListStateChange}
      agentOptions={agentOptions}
      onCreate={onCreate}
      onEdit={onEdit}
    />
  );
}
