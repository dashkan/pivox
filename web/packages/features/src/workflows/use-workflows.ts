'use client';

import { useResourceList } from '@/resource-admin';
import { workflowsListDescriptor } from '@/workflows/workflows-descriptor';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsChange,
  ListControlsValue,
  ResourceListContextValue,
  Workflow,
  WorkflowListExtras,
} from '@pivox/ui/resource-admin';

/** Workflows have no create-from-list flow, so the create action is a no-op. */
const noCreate = (): void => {};
/** No route-owned extras: workflows are org-direct only, with no spaces/agents. */
const noInjected: Record<string, never> = {};

/**
 * Drives the Workflows admin LIST surface — the third consumer of the generic,
 * descriptor-driven {@link useResourceList}, proving the List half stands alone
 * without a form. The generic hook owns the org query, the list-controls, and
 * pagination; workflows supply no extras and no create action.
 *
 * Reads via the injected `$api`, writes via the injected `apiClient`. Unlike
 * connectors/secrets, `onEdit` navigates to the bespoke React Flow CANVAS
 * (route-injected, `/workflows/$id`), not a routed form — that is the "different
 * row action" the design calls out, and it drops in with no abstraction change.
 */
export function useWorkflows(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  /** Org resource name (`organizations/{slug}`); flows to the list + canvas routes. */
  parent: string;
  /** List-controls state (route-owned, e.g. URL search params). */
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  /** Navigate to the workflow's canvas (route-injected). */
  onEdit: (workflow: Workflow) => void;
}): ResourceListContextValue<Workflow, WorkflowListExtras> {
  const { $api, apiClient, parent, listState, onListStateChange, onEdit } =
    input;

  return useResourceList(workflowsListDescriptor, {
    $api,
    apiClient,
    parent,
    state: listState,
    onStateChange: onListStateChange,
    injected: noInjected,
    onCreate: noCreate,
    onEdit,
  });
}
