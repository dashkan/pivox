'use client';

import { workflowsListView } from '@pivox/ui/resource-admin';

import { buildWorkflowsListRequest } from '@/workflows/build-workflows-request';
import { deleteWorkflow } from '@/workflows/delete-workflow';

import type { ListDescriptor, ResourceAdmin } from '@/resource-admin';
import type { components } from '@pivox/client/types';
import type { Workflow, WorkflowListExtras } from '@pivox/ui/resource-admin';

type ListWorkflowsResponse = components['schemas']['v1ListWorkflowsResponse'];

const WORKFLOWS_PATH = '/v1/organizations/{organization}/workflows' as const;
const SPACE_WORKFLOWS_PATH =
  '/v1/organizations/{organization}/spaces/{space}/workflows' as const;

/**
 * react-query `placeholderData` that keeps the previous page rendered while a new
 * query key (filter / sort / page change) loads — no empty flash, no `isLoading`
 * flip. Equivalent to `keepPreviousData`, inlined to avoid a direct dep on it.
 */
function keepPrevious<T>(previous: T): T {
  return previous;
}

/**
 * The workflows LIST descriptor (data side) — the sibling of
 * `secretsListDescriptor`. The scope switches the list PARENT path; both queries
 * are declared so the hook count stays stable and only the one matching the scope
 * is enabled. Its react-query key is byte-identical to the SSR loader's prime
 * (same literal path + shared `buildWorkflowsListRequest`), so primed rows hydrate
 * instead of refetching. Workflows carry no per-response facet, so `extrasOf`
 * returns an empty bag.
 *
 * Unlike secrets/connectors the org level is an EXACT space_id match, not a rollup
 * (see the backend `WorkflowFilter`): `scope: ''` lists only the org-direct
 * workflows, a space slug lists that space's — the request plumbing is the same,
 * only the backend semantics differ.
 *
 * The row action navigates to the bespoke React Flow CANVAS, not a form — that is
 * the injected `onEdit` the route points at the `$workflowId` route; the
 * descriptor is agnostic to where it lands.
 */
export const workflowsListDescriptor: ListDescriptor<
  Workflow,
  WorkflowListExtras,
  Record<string, never>,
  ListWorkflowsResponse
> = {
  key: 'workflows',
  useList({ $api, organization, state }) {
    // The shared builder produces the exact query the SSR loader keys on.
    const { query } = buildWorkflowsListRequest(organization, state);
    const scope = state.scope;

    const orgListQuery = $api.useQuery(
      'get',
      WORKFLOWS_PATH,
      { params: { path: { organization }, query } },
      { enabled: scope === '', placeholderData: keepPrevious },
    );
    const spaceListQuery = $api.useQuery(
      'get',
      SPACE_WORKFLOWS_PATH,
      { params: { path: { organization, space: scope }, query } },
      { enabled: scope !== '', placeholderData: keepPrevious },
    );
    const listQuery = scope === '' ? orgListQuery : spaceListQuery;
    return {
      data: listQuery.data,
      isLoading: listQuery.isLoading,
      // react-query uses `null` for "no error"; the ApiError arm is `undefined`.
      error: listQuery.error ?? undefined,
      refetch: listQuery.refetch,
    };
  },
  rowsOf: (data) => data?.workflows ?? [],
  nextPageTokenOf: (data) => data?.nextPageToken,
  rowId: (workflow) => workflow.name ?? '',
  // Workflows carry no resource-specific list-view data (no spaces, no agents).
  extrasOf: () => ({}),
  remove: (apiClient, workflow) => deleteWorkflow({ apiClient, workflow }),
  loadErrorFallback: "Couldn't load workflows.",
  view: workflowsListView,
};

/**
 * The workflows admin — LIST ONLY. No `form` (creation/editing is the bespoke
 * React Flow canvas, wired at the route as the injected row action), no
 * `createView`/`editView` override registered here (the canvas lives in the app's
 * `$workflowId` route, untouched by this migration). This is the design's
 * "List always shared; create/edit is a custom surface" case reduced to its
 * minimum: the shared List, and nothing else.
 */
export const workflowsResourceAdmin: ResourceAdmin<
  Workflow,
  never,
  WorkflowListExtras,
  Record<string, never>,
  ListWorkflowsResponse
> = {
  list: workflowsListDescriptor,
};
