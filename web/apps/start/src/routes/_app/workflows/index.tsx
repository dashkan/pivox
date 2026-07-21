import { WorkflowsFeature } from '@pivox/features/workflows';
import { useAppShellContext } from '@pivox/ui/app-shell';
import { AdminNotice, workflowLeafId } from '@pivox/ui/resource-admin';
import { createFileRoute } from '@tanstack/react-router';
import { useCallback, useMemo } from 'react';

import type { ListControlsChange, Workflow } from '@pivox/ui/resource-admin';

import { $api, apiClient } from '@/lib/api-client';
import {
  searchToValue,
  validateWorkflowsSearch,
  valueToSearch,
} from '@/lib/workflows-search';
import { prefetchWorkflows } from '@/server/prefetch';

const WORKFLOWS_PATH = '/v1/organizations/{organization}/workflows' as const;

export const Route = createFileRoute('/_app/workflows/')({
  validateSearch: validateWorkflowsSearch,
  loaderDeps: ({ search }) => search,
  // SSR-only: prefetch this exact query + prime the client's react-query key so
  // rows are in the server HTML. Client navs skip it.
  loader: async ({ context, deps }) => {
    if (typeof window !== 'undefined') return;
    const prefetched = await prefetchWorkflows({ data: deps });
    if (prefetched) {
      const { queryKey } = $api.queryOptions('get', WORKFLOWS_PATH, {
        params: {
          path: { organization: prefetched.orgSlug },
          query: prefetched.query,
        },
      });
      context.queryClient.setQueryData(queryKey, prefetched.workflows);
    }
  },
  component: WorkflowsPage,
});

function WorkflowsPage() {
  const { state } = useAppShellContext();
  const parent = state.activeOrganization;
  const search = Route.useSearch();
  const navigate = Route.useNavigate();

  const listState = useMemo(() => searchToValue(search), [search]);
  const onListStateChange = useCallback<ListControlsChange>(
    (next, opts) => {
      void navigate({
        search: valueToSearch(next),
        replace: opts.history === 'replace',
      });
    },
    [navigate],
  );

  // The row action navigates to the bespoke React Flow canvas — NOT a form. This
  // is the "different row action" the resource-admin design calls out: the List
  // abstraction's injected `onEdit` just repoints here.
  const onEdit = useCallback(
    (workflow: Workflow) => {
      void navigate({
        to: '/workflows/$workflowId',
        params: { workflowId: workflowLeafId(workflow.name) },
      });
    },
    [navigate],
  );

  if (!parent) {
    return (
      <div className="flex flex-1 flex-col p-6">
        <AdminNotice>Select an organization to view workflows.</AdminNotice>
      </div>
    );
  }

  return (
    <WorkflowsFeature
      $api={$api}
      apiClient={apiClient}
      parent={parent}
      listState={listState}
      onListStateChange={onListStateChange}
      onEdit={onEdit}
    />
  );
}
