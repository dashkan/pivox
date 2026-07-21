'use client';

import { ResourceList, workflowsListView } from '@pivox/ui/resource-admin';

import { useWorkflows } from './use-workflows';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsChange,
  ListControlsValue,
  Workflow,
} from '@pivox/ui/resource-admin';

/**
 * Workflows LIST feature — a thin wrapper: run the descriptor-driven
 * {@link useWorkflows} hook and render the generic `ResourceList` with the
 * workflows view. List-controls state (filter/sort/page) is owned by the route
 * (URL search params) and passed in. There is no create action; `onEdit`
 * navigates to the bespoke canvas, injected by the route so the feature stays
 * router-agnostic.
 */
export function WorkflowsFeature({
  $api,
  apiClient,
  parent,
  listState,
  onListStateChange,
  onEdit,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  onEdit: (workflow: Workflow) => void;
}) {
  const value = useWorkflows({
    $api,
    apiClient,
    parent,
    listState,
    onListStateChange,
    onEdit,
  });
  return (
    <ResourceList.Provider value={value}>
      <ResourceList.Root view={workflowsListView} />
    </ResourceList.Provider>
  );
}
