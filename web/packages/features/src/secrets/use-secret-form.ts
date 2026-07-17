'use client';

import { useCallback, useMemo, useState } from 'react';

import {
  describeCaughtError,
  describeRpcError,
  mapDeleteError,
} from '@/resource-admin/rpc-error';
import { resourcePathParams } from '@/workflows/resource-paths';

import { deleteSecret, saveSecret } from './save-secret';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { SecretFormValues } from '@pivox/ui/resource-admin';
import type { FormMode } from '@pivox/ui/form-page';

const SECRET_PATH =
  '/v1/organizations/{organization}/secrets/{secret}' as const;
const SPACE_SECRET_PATH =
  '/v1/organizations/{organization}/spaces/{space}/secrets/{secret}' as const;

/** Keep the previous single-record render while a fresh key loads (no flash). */
function keepPrevious<T>(previous: T): T {
  return previous;
}

/** The delete-confirm slice the edit page's `DeleteDialog` binds to. */
export interface SecretRemoveState {
  open: boolean;
  pending: boolean;
  error: string | null;
  onOpenChange: (open: boolean) => void;
  onConfirm: () => void;
}

/**
 * Orchestrates a routed secret create/edit page: owns the submit pending/error,
 * the create/update mutation, the edit record load (SSR-primed under the same
 * `$api` key the loader sets), and the edit delete-confirm — the secret twin of
 * `useConnectorForm`. Navigation (`onDone`) is injected from the route; this hook
 * stays router-free.
 */
export function useSecretForm(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  /** Org resource name (`organizations/{slug}`). */
  parent: string;
  mode: FormMode;
  /** Edit-only: the secret leaf id + its space slug (absent = org-direct). */
  secretId?: string;
  space?: string;
  /** Navigate to the launching route; called on submit-success and delete-success. */
  onDone: () => void;
}) {
  const { $api, apiClient, parent, mode, secretId, space, onDone } = input;
  const organization = useMemo(
    () => resourcePathParams(parent).organization ?? '',
    [parent],
  );

  const isEdit = mode === 'edit';
  // Single-record query, keyed identically to the SSR loader's `setQueryData`
  // (same `$api` params) so the prefetched record hydrates with no XHR on load.
  const recordQuery = $api.useQuery(
    'get',
    space ? SPACE_SECRET_PATH : SECRET_PATH,
    {
      params: {
        path: space
          ? { organization, space, secret: secretId ?? '' }
          : { organization, secret: secretId ?? '' },
      },
    },
    { enabled: isEdit && Boolean(secretId), placeholderData: keepPrevious },
  );
  const record = isEdit ? (recordQuery.data ?? null) : null;

  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mutate = useCallback(
    (values: SecretFormValues) => {
      setPending(true);
      setError(null);
      void (async () => {
        // openapi-fetch RESOLVES `{ error }` on an HTTP error status but
        // REJECTS on a transport failure (network down, TLS, an aborted/
        // cancelled request). Both must surface a message and clear pending —
        // the missing catch here is what let a space-scoped create fail
        // silently, leaving a stuck spinner and a dangling cancelled request.
        try {
          const resp = await saveSecret({
            apiClient,
            mode,
            editing: record,
            organization,
            values,
          });
          if (resp.error) {
            setPending(false);
            setError(describeRpcError(resp.error, "Couldn't save the secret."));
            return;
          }
          setPending(false);
          onDone();
        } catch (err) {
          setPending(false);
          setError(describeCaughtError(err, "Couldn't save the secret."));
        }
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
      // shown error.
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
        const resp = await deleteSecret({ apiClient, secret: record });
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
  }, [apiClient, record, onDone]);

  const remove: SecretRemoveState = {
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
        ? describeRpcError(recordQuery.error, "Couldn't load this secret.")
        : null,
    pending,
    error,
    mutate,
    onDelete: isEdit ? onDelete : undefined,
    remove,
  };
}
