import { WorkflowsFeature } from '@pivox/features/workflows';
import { workflowLeafId, workflowSpaceSlug } from '@pivox/ui/resource-admin';
import { useNavigate, useRouterState } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import type { ListControlsChange, Workflow } from '@pivox/ui/resource-admin';

import { $api, apiClient } from '@/lib/api-client';
import {
  searchToValue,
  valueToSearch,
  type WorkflowsSearch,
} from '@/lib/workflows-search';

/**
 * The shared workflows LIST feature for the scope-in-URL routes — the workflow
 * twin of `ScopedSecretsList`. Both the org route
 * (`/organizations/$organization/workflows`) and the space-scoped route
 * (`.../spaces/$space/workflows`) render this, passing the org slug and — for the
 * space route — the space slug from their PATH params. The URL is the single
 * source of truth for scope, so `parent` is always the org and the space narrows
 * the list via the controls' `scope`, forced here from `spaceSlug` (not search).
 *
 * The row action navigates to the bespoke React Flow canvas detail (NOT a form):
 * the List abstraction's injected `onEdit` repoints there, targeting the
 * workflow's OWN scope (org-direct vs its space), read off its resource name,
 * carrying `?from=` so the canvas can return to this exact filtered/sorted/paged
 * view.
 *
 * Note: unlike secrets the org list is NOT a rollup (the backend `WorkflowFilter`
 * lists only org-direct rows at the org level), so there is no in-list scope
 * selector — space navigation flows from the app-shell scope picker.
 */
export function ScopedWorkflowsList({
  orgSlug,
  spaceSlug,
  search,
}: {
  orgSlug: string;
  /** Present on the space-scoped route; absent on the org list. */
  spaceSlug?: string;
  search: WorkflowsSearch;
}) {
  const navigate = useNavigate();

  // Controls value: search params + the scope pinned from the path param.
  const listState = useMemo(
    () => ({ ...searchToValue(search), scope: spaceSlug ?? '' }),
    [search, spaceSlug],
  );

  const onListStateChange = useCallback<ListControlsChange>(
    (next, opts) => {
      const nextSearch = valueToSearch(next);
      if (spaceSlug) {
        void navigate({
          to: '/organizations/$organization/spaces/$space/workflows',
          params: { organization: orgSlug, space: spaceSlug },
          search: nextSearch,
          replace: opts.history === 'replace',
        });
      } else {
        void navigate({
          to: '/organizations/$organization/workflows',
          params: { organization: orgSlug },
          search: nextSearch,
          replace: opts.history === 'replace',
        });
      }
    },
    [navigate, orgSlug, spaceSlug],
  );

  // Capture THIS list view's exact URL so the canvas detail can return to it.
  const currentHref = useRouterState({
    select: (s) => s.location.pathname + s.location.searchStr,
  });

  const onEdit = useCallback(
    (workflow: Workflow) => {
      // Target the workflow's OWN scope, read off its resource name — not the
      // route's scope (robust even if a space-scoped row surfaces in the org list).
      const workflowSpace = workflowSpaceSlug(workflow.name);
      const workflowId = workflowLeafId(workflow.name);
      if (workflowSpace) {
        void navigate({
          to: '/organizations/$organization/spaces/$space/workflows/$workflowId',
          params: { organization: orgSlug, space: workflowSpace, workflowId },
          search: { from: currentHref },
        });
      } else {
        void navigate({
          to: '/organizations/$organization/workflows/$workflowId',
          params: { organization: orgSlug, workflowId },
          search: { from: currentHref },
        });
      }
    },
    [navigate, orgSlug, currentHref],
  );

  return (
    <WorkflowsFeature
      $api={$api}
      apiClient={apiClient}
      parent={`organizations/${orgSlug}`}
      listState={listState}
      onListStateChange={onListStateChange}
      onEdit={onEdit}
    />
  );
}
