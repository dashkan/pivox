'use client';

import { useCallback, useMemo, useState } from 'react';

import { describeRpcError, mapDeleteError } from '@/resource-admin/rpc-error';
import { resourcePathParams } from '@/workflows/resource-paths';

import { deleteConnector, saveConnector } from './save-connector';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { ConnectorFormValues } from '@pivox/ui/resource-admin';
import type { FormMode } from '@pivox/ui/form-page';

const CONNECTOR_PATH =
  '/v1/organizations/{organization}/connectors/{connector}' as const;
const SPACE_CONNECTOR_PATH =
  '/v1/organizations/{organization}/spaces/{space}/connectors/{connector}' as const;

/** Keep the previous single-record render while a fresh key loads (no flash). */
function keepPrevious<T>(previous: T): T {
  return previous;
}

/** The delete-confirm slice the edit page's `DeleteDialog` binds to. */
export interface ConnectorRemoveState {
  open: boolean;
  pending: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}

/**
 * Orchestrates a routed connector create/edit page: owns the submit
 * pending/error, the create/update mutation, the edit record load (SSR-primed
 * under the same `$api` key the loader sets), and the edit delete-confirm. The
 * navigation (`onDone`) is injected from the route — this hook stays router-free.
 *
 * `FormPage` never sees any of this: the hook feeds pending/error/mutate/record
 * into `ConnectorFormProvider`, which maps them onto the generic contract and
 * calls `onDone` from the mutation's success path (`5.8 logic-in-handlers`, not
 * an effect watching a success flag).
 */
export function useConnectorForm(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  /** Org resource name (`organizations/{slug}`). */
  parent: string;
  mode: FormMode;
  /** Edit-only: the connector leaf id + its space slug (absent = org-direct). */
  connectorId?: string;
  space?: string;
  /** Navigate to the launching route; called on submit-success and delete-success. */
  onDone: () => void;
}) {
  const { $api, apiClient, parent, mode, connectorId, space, onDone } = input;
  const organization = useMemo(
    () => resourcePathParams(parent).organization ?? '',
    [parent],
  );

  const isEdit = mode === 'edit';
  // Single-record query, keyed identically to the SSR loader's `setQueryData`
  // (same `$api` params) so the prefetched record hydrates with no XHR on load.
  const recordQuery = $api.useQuery(
    'get',
    space ? SPACE_CONNECTOR_PATH : CONNECTOR_PATH,
    {
      params: {
        path: space
          ? { organization, space, connector: connectorId ?? '' }
          : { organization, connector: connectorId ?? '' },
      },
    },
    { enabled: isEdit && Boolean(connectorId), placeholderData: keepPrevious },
  );
  const record = isEdit ? (recordQuery.data ?? null) : null;

  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mutate = useCallback(
    (values: ConnectorFormValues) => {
      setPending(true);
      setError(null);
      void (async () => {
        const resp = await saveConnector({
          apiClient,
          mode,
          editing: record,
          organization,
          values,
        });
        if (resp.error) {
          setPending(false);
          setError(describeRpcError(resp.error, "Couldn't save the connector."));
          return;
        }
        // Interaction result in the handler, not an effect (5.8). Navigate to
        // the launching route on success.
        setPending(false);
        onDone();
      })();
    },
    [apiClient, mode, record, organization, onDone],
  );

  // Edit-only delete-confirm state + mutation.
  const [removeOpen, setRemoveOpen] = useState(false);
  const [removePending, setRemovePending] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const onDelete = useCallback(() => setRemoveOpen(true), []);
  const onRemoveOpenChange = useCallback(
    (open: boolean) => {
      // Never dismiss mid-delete; the confirm stays open until success or a
      // shown error. Read `removePending` directly (the DeleteDialog isn't
      // memoized, so recreating this callback is free — and keeps the state
      // updater pure).
      if (!removePending) setRemoveOpen(open);
    },
    [removePending],
  );
  const confirmRemove = useCallback(() => {
    if (!record) return;
    setRemovePending(true);
    setRemoveError(null);
    void (async () => {
      const resp = await deleteConnector({ apiClient, connector: record });
      if (resp.error) {
        setRemovePending(false);
        setRemoveError(mapDeleteError(resp.error));
        return;
      }
      onDone();
    })();
  }, [apiClient, record, onDone]);

  const remove: ConnectorRemoveState = {
    open: removeOpen,
    pending: removePending,
    error: removeError,
    onOpenChange: onRemoveOpenChange,
    onConfirm: confirmRemove,
  };

  return {
    record,
    recordLoading: isEdit ? recordQuery.isLoading : false,
    loadError:
      isEdit && recordQuery.error
        ? describeRpcError(recordQuery.error, "Couldn't load this connector.")
        : null,
    pending,
    error,
    mutate,
    onDelete: isEdit ? onDelete : undefined,
    remove,
  };
}
