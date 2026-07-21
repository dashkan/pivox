import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsValue,
  ResourceListView,
} from '@pivox/ui/resource-admin';
import type { ApiError } from '@/resource-admin/rpc-error';

/**
 * The read result of a descriptor's client-side list query — the slice
 * `useResourceList` consumes. Deliberately the openapi-react-query subset the
 * generic hook needs (data / isLoading / error / refetch), typed loosely so the
 * descriptor can hold the literal path types that key + type the query while the
 * generic hook stays resource-agnostic.
 */
export interface ListQueryState<Resp = unknown> {
  data: Resp | undefined;
  isLoading: boolean;
  /** The gateway's `rpcStatus` body on failure, else undefined (openapi-react-query's error arm). */
  error: ApiError;
  refetch: () => Promise<unknown>;
}

/**
 * The DATA side of a resource's LIST descriptor (the presentational side is the
 * `@pivox/ui` `ResourceListView` on `view`). Router- and — at this boundary —
 * react-query-agnostic: the query runs through the injected `$api`/`apiClient`,
 * never an import. A new admin resource supplies one of these + a thin route.
 *
 * @typeParam Row     — a list row (e.g. a connector).
 * @typeParam Extras  — resource-specific data the view needs but a row can't give
 *                      (assignable agents, spaces to scope by, facet results).
 * @typeParam Injected — the route-owned slice of `Extras` (SSR-prefetched agent
 *                      options, spaces); merged with per-response extras by
 *                      `extrasOf`.
 * @typeParam Resp     — the list response wire type (defaults to `unknown`);
 *                      supply it to type `rowsOf`/`extrasOf`/`nextPageTokenOf`
 *                      without a cast.
 */
export interface ListDescriptor<Row, Extras, Injected, Resp = unknown> {
  /** Resource id (query-key namespace / route base), e.g. `'connectors'`. */
  key: string;
  /**
   * Client-side list query — a descriptor-owned HOOK, not a plain builder,
   * because openapi-react-query needs the LITERAL path templates to both type and
   * KEY the query (a `string` path can't). The "two scoped queries (org rollup +
   * space), one enabled, `keepPreviousData`" pattern lives here where those
   * literals naturally do; its react-query key is byte-identical to the SSR
   * loader's prime (both go through the same literal path + shared request
   * builder), so primed rows hydrate instead of refetching. Called
   * unconditionally by `useResourceList` (stable hook count).
   */
  useList(input: {
    $api: ReactQueryApi;
    /** Org slug (the `{organization}` path param), resolved from `parent`. */
    organization: string;
    /** URL-owned list-controls state (filter/sort/scope/page). */
    state: ListControlsValue;
  }): ListQueryState<Resp>;
  /** Rows out of the list response. */
  rowsOf(data: Resp | undefined): Row[];
  /** The response's opaque next-page cursor, if any. */
  nextPageTokenOf(data: Resp | undefined): string | undefined;
  /** Stable row identity — the delete guard + the grid row key. */
  rowId(row: Row): string;
  /**
   * Assemble the view `extras` from the per-response data (facets like
   * `agentsInUse`) plus the route-injected slice (SSR-prefetched agents/spaces).
   */
  extrasOf(data: Resp | undefined, injected: Injected): Extras;
  /** Delete a row (threads the optimistic-concurrency etag). Returns the openapi-fetch result. */
  remove(apiClient: ApiClient, row: Row): Promise<{ error?: ApiError }>;
  /** Fallback text when the list query fails without a server message. */
  loadErrorFallback: string;
  /** The presentational list view — columns-as-data, toolbar controls, copy. */
  view: ResourceListView<Row, Extras>;
}
