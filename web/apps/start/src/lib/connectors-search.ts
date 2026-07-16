import {
  AGENT_FILTER_ANY,
  DEFAULT_PAGE_SIZE,
  PAGE_SIZES,
} from '@pivox/ui/resource-admin';

import type { ListControlsValue } from '@pivox/ui/resource-admin';

/**
 * URL search-param shape for the connectors list. Short keys keep links tidy;
 * defaults are omitted (see {@link valueToSearch}) so a clean list has a bare
 * `/connectors` URL. This is the single source of the list controls' state —
 * the route owns it; the feature/ui packages consume it controlled.
 */
export interface ConnectorsSearch {
  /** Name substring filter. */
  q?: string;
  /** Agent filter value (sentinel or agent resource name); absent = any. */
  agent?: string;
  /** Space slug scope; absent = the org rollup. */
  scope?: string;
  /** Sort field (e.g. `displayName`, `updateTime`). */
  sort?: string;
  /** Sort direction; absent = ascending. */
  dir?: 'asc' | 'desc';
  /** Page size; absent = the default. */
  size?: number;
  /** Opaque page cursor. */
  cursor?: string;
}

/** Route `validateSearch`: coerce untyped URL params into {@link ConnectorsSearch}. */
export function validateConnectorsSearch(
  search: Record<string, unknown>,
): ConnectorsSearch {
  const out: ConnectorsSearch = {};
  if (typeof search.q === 'string' && search.q) out.q = search.q;
  if (typeof search.agent === 'string' && search.agent) out.agent = search.agent;
  if (typeof search.scope === 'string' && search.scope) out.scope = search.scope;
  if (typeof search.sort === 'string' && search.sort) out.sort = search.sort;
  if (search.dir === 'asc' || search.dir === 'desc') out.dir = search.dir;
  const size = Number(search.size);
  if ((PAGE_SIZES as readonly number[]).includes(size)) out.size = size;
  if (typeof search.cursor === 'string' && search.cursor) out.cursor = search.cursor;
  return out;
}

/** URL search → the controlled list-controls value. */
export function searchToValue(search: ConnectorsSearch): ListControlsValue {
  const filters: Record<string, string> = {};
  if (search.q) filters.displayName = search.q;
  if (search.agent) filters.agent = search.agent;
  return {
    filters,
    sort: search.sort
      ? { field: search.sort, direction: search.dir ?? 'asc' }
      : null,
    pageSize: search.size ?? DEFAULT_PAGE_SIZE,
    scope: search.scope ?? '',
    pageToken: search.cursor,
  };
}

/** The controlled list-controls value → URL search, omitting defaults. */
export function valueToSearch(value: ListControlsValue): ConnectorsSearch {
  const out: ConnectorsSearch = {};
  const q = value.filters.displayName?.trim();
  if (q) out.q = q;
  const agent = value.filters.agent;
  if (agent && agent !== AGENT_FILTER_ANY) out.agent = agent;
  if (value.scope) out.scope = value.scope;
  if (value.sort) {
    out.sort = value.sort.field;
    if (value.sort.direction === 'desc') out.dir = 'desc';
  }
  if (value.pageSize !== DEFAULT_PAGE_SIZE) out.size = value.pageSize;
  if (value.pageToken) out.cursor = value.pageToken;
  return out;
}
