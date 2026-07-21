import { DEFAULT_PAGE_SIZE, PAGE_SIZES } from '@pivox/ui/resource-admin';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

/**
 * URL search-param shape for the workflows list — the workflows sibling of
 * `SecretsSearch`, minus the `scope` workflows don't have (ListWorkflows is
 * org-direct only, no space rollup). Short keys keep links tidy; defaults are
 * omitted (see {@link valueToSearch}) so a clean list has a bare `/workflows`
 * URL. This is the single source of the list controls' state — the route owns it;
 * the feature/ui packages consume it controlled.
 */
export interface WorkflowsSearch {
  /** Name substring filter. */
  q?: string;
  /** Sort field (`displayName` or `updateTime`). */
  sort?: string;
  /** Sort direction; absent = ascending. */
  dir?: 'asc' | 'desc';
  /** Page size; absent = the default. */
  size?: number;
  /** Opaque page cursor. */
  cursor?: string;
}

/** Route `validateSearch`: coerce untyped URL params into {@link WorkflowsSearch}. */
export function validateWorkflowsSearch(
  search: Record<string, unknown>,
): WorkflowsSearch {
  const out: WorkflowsSearch = {};
  if (typeof search.q === 'string' && search.q) out.q = search.q;
  if (typeof search.sort === 'string' && search.sort) out.sort = search.sort;
  if (search.dir === 'asc' || search.dir === 'desc') out.dir = search.dir;
  const size = Number(search.size);
  if ((PAGE_SIZES as readonly number[]).includes(size)) out.size = size;
  if (typeof search.cursor === 'string' && search.cursor) out.cursor = search.cursor;
  return out;
}

/** URL search → the controlled list-controls value. */
export function searchToValue(search: WorkflowsSearch): ListControlsValue {
  const filters: Record<string, string> = {};
  if (search.q) filters.displayName = search.q;
  return {
    filters,
    sort: search.sort
      ? { field: search.sort, direction: search.dir ?? 'asc' }
      : null,
    pageSize: search.size ?? DEFAULT_PAGE_SIZE,
    // Workflows are org-direct only — the list has no space scope.
    scope: '',
    pageToken: search.cursor,
  };
}

/** The controlled list-controls value → URL search, omitting defaults. */
export function valueToSearch(value: ListControlsValue): WorkflowsSearch {
  const out: WorkflowsSearch = {};
  const q = value.filters.displayName?.trim();
  if (q) out.q = q;
  if (value.sort) {
    out.sort = value.sort.field;
    if (value.sort.direction === 'desc') out.dir = 'desc';
  }
  if (value.pageSize !== DEFAULT_PAGE_SIZE) out.size = value.pageSize;
  if (value.pageToken) out.cursor = value.pageToken;
  return out;
}
