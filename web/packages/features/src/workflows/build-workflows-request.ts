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
 * state. A discriminated union so `isSpaceScoped` narrows `pathParams` without
 * casts — the workflow twin of `SecretsListRequest`. Workflows are an org+space
 * leveled resource (proto ListWorkflows has an `organizations/*​/spaces/*` binding):
 * the empty scope lists the org-direct workflows, a space slug lists that space's.
 * Note the org level is an EXACT space_id match, NOT the connectors-style rollup
 * (see the backend `WorkflowFilter` declaration), so `scope: ''` returns only the
 * org-direct rows — but the request shape is identical to secrets'.
 */
export type WorkflowsListRequest =
  | {
      isSpaceScoped: false;
      pathParams: { organization: string };
      query: WorkflowsListQuery;
    }
  | {
      isSpaceScoped: true;
      pathParams: { organization: string; space: string };
      query: WorkflowsListQuery;
    };

/**
 * Builds the workflows list request from the controls state. The single source of
 * truth for BOTH the client `useQuery` and the SSR prefetch, so their react-query
 * keys can't drift and SSR-primed data is read on hydration instead of silently
 * refetching. A non-empty `value.scope` (the path-owned space slug) selects the
 * space-scoped parent path.
 */
export function buildWorkflowsListRequest(
  organization: string,
  value: ListControlsValue,
): WorkflowsListRequest {
  const query: WorkflowsListQuery = {
    filter: buildWorkflowFilter(value.filters),
    orderBy: orderByParam(value.sort),
    pageSize: value.pageSize,
    pageToken: value.pageToken,
  };
  if (value.scope) {
    return {
      isSpaceScoped: true,
      pathParams: { organization, space: value.scope },
      query,
    };
  }
  return { isSpaceScoped: false, pathParams: { organization }, query };
}
