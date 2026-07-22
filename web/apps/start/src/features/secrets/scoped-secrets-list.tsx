import { SecretsFeature } from '@pivox/features/secrets';
import { secretLeafId, secretSpaceSlug } from '@pivox/ui/resource-admin';
import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import type { ListControlsChange, Secret } from '@pivox/ui/resource-admin';

import { $api, apiClient } from '@/lib/api-client';
import {
  searchToValue,
  valueToSearch,
  type SecretsSearch,
} from '@/lib/secrets-search';

/** valueToSearch, dropping `scope` — scope lives in the PATH for these routes. */
function listSearch(value: Parameters<typeof valueToSearch>[0]): SecretsSearch {
  const { scope: _scope, ...rest } = valueToSearch(value);
  return rest;
}

/**
 * The single shared secrets LIST feature for the scope-in-URL routes — the secret
 * twin of `ScopedConnectorsList`. Both the org-rollup route
 * (`/organizations/$organization/secrets`) and the space-scoped route
 * (`.../spaces/$space/secrets`) render this, passing the org slug and — for the
 * space route — the space slug from their PATH params. The URL is the single
 * source of truth for scope, so:
 *
 *  - `parent` is always the org (`organizations/{slug}`); the space narrows the
 *    list via the controls' `scope`, forced here from `spaceSlug` (path-owned, not
 *    search). Scope is changed by the sidebar picker (navigation), not in-list.
 *  - create/edit navigate to the scoped routed pages, carrying `?from=` so the
 *    form can return to this exact filtered/sorted/paged view. Edit targets the
 *    secret's OWN scope (org-direct vs its space), read off its resource name.
 */
export function ScopedSecretsList({
  orgSlug,
  spaceSlug,
  search,
}: {
  orgSlug: string;
  /** Present on the space-scoped route; absent on the org rollup. */
  spaceSlug?: string;
  search: SecretsSearch;
}) {
  const navigate = useNavigate();

  // Controls value: search params + the scope pinned from the path param.
  const listState = useMemo(
    () => ({ ...searchToValue(search), scope: spaceSlug ?? '' }),
    [search, spaceSlug],
  );

  const onListStateChange = useCallback<ListControlsChange>(
    (next, opts) => {
      // Scope is path-owned (sidebar navigation) — the list only updates search
      // on the current route.
      const nextSearch = listSearch(next);
      if (spaceSlug) {
        void navigate({
          to: '/organizations/$organization/spaces/$space/secrets',
          params: { organization: orgSlug, space: spaceSlug },
          search: nextSearch,
          replace: opts.history === 'replace',
        });
      } else {
        void navigate({
          to: '/organizations/$organization/secrets',
          params: { organization: orgSlug },
          search: nextSearch,
          replace: opts.history === 'replace',
        });
      }
    },
    [navigate, orgSlug, spaceSlug],
  );

  // Capture THIS list view's exact URL so the form pages can return to it.
  const currentHref = useRouterState({
    select: (s) => s.location.pathname + s.location.searchStr,
  });

  const onCreate = useCallback(() => {
    if (spaceSlug) {
      void navigate({
        to: '/organizations/$organization/spaces/$space/secrets/new',
        params: { organization: orgSlug, space: spaceSlug },
        search: { from: currentHref },
      });
    } else {
      void navigate({
        to: '/organizations/$organization/secrets/new',
        params: { organization: orgSlug },
        search: { from: currentHref },
      });
    }
  }, [navigate, orgSlug, spaceSlug, currentHref]);

  const onEdit = useCallback(
    (secret: Secret) => {
      // Edit targets the secret's OWN scope (it may be space-scoped even in an
      // org rollup), read off its resource name — not the route's scope.
      const secretSpace = secretSpaceSlug(secret.name);
      const secretId = secretLeafId(secret.name);
      if (secretSpace) {
        void navigate({
          to: '/organizations/$organization/spaces/$space/secrets/$secretId/edit',
          params: { organization: orgSlug, space: secretSpace, secretId },
          search: { from: currentHref },
        });
      } else {
        void navigate({
          to: '/organizations/$organization/secrets/$secretId/edit',
          params: { organization: orgSlug, secretId },
          search: { from: currentHref },
        });
      }
    },
    [navigate, orgSlug, currentHref],
  );

  return (
    <SecretsFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      listState={listState}
      onListStateChange={onListStateChange}
      onCreate={onCreate}
      onEdit={onEdit}
    />
  );
}
