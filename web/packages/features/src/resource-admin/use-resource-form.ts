'use client';

import { useCallback, useMemo, useState } from 'react';

import {
  describeCaughtError,
  describeRpcError,
  mapDeleteError,
} from '@/resource-admin/rpc-error';
import { resourcePathParams } from '@/workflows/resource-paths';

import type { FormDescriptor } from './form-descriptor';
import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { FormMode } from '@pivox/ui/form-page';

/** The edit-only delete-confirm slice the form page's `DeleteDialog` binds to. */
export interface ResourceRemoveState {
  open: boolean;
  pending: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}

/** What `useResourceForm` returns — fed into the resource's form provider. */
export interface ResourceFormResult<Row, Values> {
  record: Row | null;
  recordLoading: boolean;
  loadError: string | null;
  pending: boolean;
  error: string | null;
  mutate: (values: Values) => void;
  /** Edit-only: open the delete-confirm. Absent in create. */
  onDelete?: () => void;
  remove: ResourceRemoveState;
}

/**
 * Generic, descriptor-driven create/edit FORM hook — the router/react-query-
 * agnostic engine generalizing `useConnectorForm` + `useSecretForm` (which were
 * near-identical). Owns the submit pending/error, the create/update mutation,
 * the SSR-primed edit-record load, and the edit delete-confirm. Navigation
 * (`onDone`) is injected from the route; the resource's form provider maps the
 * result onto the generic `FormPage` contract and calls `onDone` from the
 * mutation's success path (logic-in-handlers, not an effect watching a flag).
 *
 * Both the save and the delete are wrapped in try/catch: openapi-fetch RESOLVES
 * `{ error }` on an HTTP error status but REJECTS on a transport failure (network
 * down, TLS, an aborted request). Both must surface a message and clear pending —
 * otherwise a transport-level failure leaves a stuck spinner.
 */
export function useResourceForm<Row, Values>(
  descriptor: FormDescriptor<Row, Values>,
  input: {
    $api: ReactQueryApi;
    apiClient: ApiClient;
    /** Org resource name (`organizations/{slug}`). */
    parent: string;
    mode: FormMode;
    /** Edit-only: the record leaf id + its space slug (absent = org-direct). */
    id?: string;
    space?: string;
    /** Navigate to the launching route; called on submit-success and delete-success. */
    onDone: () => void;
  },
): ResourceFormResult<Row, Values> {
  const { $api, apiClient, parent, mode, id, space, onDone } = input;
  const organization = useMemo(
    () => resourcePathParams(parent).organization ?? '',
    [parent],
  );

  const isEdit = mode === 'edit';
  const recordQuery = descriptor.useRecord({
    $api,
    organization,
    id,
    space,
    enabled: isEdit && Boolean(id),
  });
  const record = isEdit ? (recordQuery.data ?? null) : null;

  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mutate = useCallback(
    (values: Values) => {
      setPending(true);
      setError(null);
      void (async () => {
        try {
          const resp = await descriptor.save({
            apiClient,
            mode,
            editing: record,
            organization,
            values,
          });
          if (resp.error) {
            setPending(false);
            setError(describeRpcError(resp.error, descriptor.saveErrorFallback));
            return;
          }
          // Interaction result in the handler, not an effect: navigate on success.
          setPending(false);
          onDone();
        } catch (err) {
          setPending(false);
          setError(describeCaughtError(err, descriptor.saveErrorFallback));
        }
      })();
    },
    [apiClient, mode, record, organization, onDone, descriptor],
  );

  // Edit-only delete-confirm state + mutation.
  const [removeOpen, setRemoveOpen] = useState(false);
  const [removePending, setRemovePending] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  const onDelete = useCallback(() => setRemoveOpen(true), []);
  const onRemoveOpenChange = useCallback(
    (open: boolean) => {
      // Never dismiss mid-delete; the confirm stays open until success or a shown error.
      if (!removePending) setRemoveOpen(open);
    },
    [removePending],
  );
  const confirmRemove = useCallback(() => {
    if (!record) return;
    setRemovePending(true);
    setRemoveError(null);
    void (async () => {
      try {
        const resp = await descriptor.remove(apiClient, record);
        if (resp.error) {
          setRemovePending(false);
          setRemoveError(mapDeleteError(resp.error));
          return;
        }
        onDone();
      } catch (err) {
        setRemovePending(false);
        setRemoveError(
          describeCaughtError(err, "Couldn't delete. Please try again."),
        );
      }
    })();
  }, [apiClient, record, onDone, descriptor]);

  const remove: ResourceRemoveState = {
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
        ? describeRpcError(recordQuery.error, descriptor.loadErrorFallback)
        : null,
    pending,
    error,
    mutate,
    onDelete: isEdit ? onDelete : undefined,
    remove,
  };
}
