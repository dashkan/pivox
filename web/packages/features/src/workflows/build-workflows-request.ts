import { orderByParam } from '@pivox/ui/resource-admin';

import { buildWorkflowFilter } from '@/workflows/build-workflow-filter';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

/** AIP list query params — identical shape client-side and in the SSR loader. */
export interface WorkflowsListQuery {
  filter?: string;
  orderBy?: string;
  pageSize: number;
  pageToken?: string;
}

/**
 * The workflows list request (path params + query) derived from the list-controls
 * state. Unlike connectors/secrets there is NO space-scope union: ListWorkflows is
 * org-direct only (an org-level parent lists just the space_id-NULL workflows, not
 * a rollup — see the WorkflowFilter declaration), so the list has a single parent
 * path and the controls' `scope` is always empty here.
 */
export interface WorkflowsListRequest {
  pathParams: { organization: string };
  query: WorkflowsListQuery;
}

/**
 * Builds the workflows list request from the controls state. The single source of
 * truth for BOTH the client `useQuery` and the SSR prefetch, so their react-query
 * keys can't drift and SSR-primed data is read on hydration instead of silently
 * refetching.
 */
export function buildWorkflowsListRequest(
  organization: string,
  value: ListControlsValue,
): WorkflowsListRequest {
  return {
    pathParams: { organization },
    query: {
      filter: buildWorkflowFilter(value.filters),
      orderBy: orderByParam(value.sort),
      pageSize: value.pageSize,
      pageToken: value.pageToken,
    },
  };
}
