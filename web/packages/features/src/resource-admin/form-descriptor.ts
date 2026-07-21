import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { FormMode } from '@pivox/ui/form-page';
import type { ApiError } from '@/resource-admin/rpc-error';

/** The read result of a descriptor's single-record detail query. */
export interface RecordQueryState<Row> {
  data: Row | undefined;
  isLoading: boolean;
  error: ApiError;
}

/**
 * The DATA side of a resource's create/edit FORM descriptor. The presentational
 * side — the field-set components + the values→`FormPage` mapping — is the
 * resource's own form provider (`ConnectorFormProvider`), which owns the values
 * the router-agnostic contract deliberately omits. This descriptor carries only
 * the async: the edit-record detail query (SSR-primed under a byte-identical
 * key), the create/update save, and the delete. Router- and, at this boundary,
 * react-query-agnostic.
 */
export interface FormDescriptor<Row, Values> {
  /**
   * Single-record detail query for the edit form — a descriptor-owned HOOK for
   * the same literal-path reason as `ListDescriptor.useList`. Its key is
   * byte-identical to the SSR loader's `setQueryData`, so a prefetched record
   * hydrates with no XHR on load. Called unconditionally by `useResourceForm`.
   */
  useRecord(input: {
    $api: ReactQueryApi;
    /** Org slug, resolved from `parent`. */
    organization: string;
    /** Record leaf id (edit only). */
    id?: string;
    /** Space slug for a space-scoped record; absent = org-direct. */
    space?: string;
    /** Whether the query is active (edit with an id). */
    enabled: boolean;
  }): RecordQueryState<Row>;
  /**
   * Create or update on the path the scope dictates (create → `values.scope`;
   * update → the record's name). Returns the openapi-fetch result.
   */
  save(input: {
    apiClient: ApiClient;
    mode: FormMode;
    /** The edit record (update); null on create. */
    editing: Row | null;
    organization: string;
    values: Values;
  }): Promise<{ error?: ApiError }>;
  /** Delete the record (threads the etag). Returns the openapi-fetch result. */
  remove(apiClient: ApiClient, record: Row): Promise<{ error?: ApiError }>;
  /** Fallback text when the detail query fails without a server message. */
  loadErrorFallback: string;
  /** Fallback text when a save fails without a server message. */
  saveErrorFallback: string;
}
