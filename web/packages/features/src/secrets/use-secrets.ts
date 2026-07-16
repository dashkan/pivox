'use client';

import { useCallback, useMemo, useState } from 'react';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  DialogState,
  RemoveState,
  Secret,
  SecretFormValues,
  SecretsAdminContextValue,
} from '@pivox/ui/resource-admin';

import { describeRpcError, mapDeleteError } from '@/resource-admin/rpc-error';
import {
  buildSecretCreateBody,
  buildSecretUpdateBody,
} from '@/secrets/build-secret-body';
import { resourcePathParams } from '@/workflows/resource-paths';

const SECRETS_PATH = '/v1/organizations/{organization}/secrets' as const;
const SECRET_PATH = '/v1/organizations/{organization}/secrets/{secret}' as const;

const initialDialog: DialogState<Secret> = {
  open: false,
  mode: 'create',
  editing: null,
  error: null,
  pending: false,
};

const initialRemove: RemoveState<Secret> = {
  target: null,
  error: null,
  pending: false,
};

/** Exact `{ organization, secret }` for the item routes (name is well-formed). */
function secretParams(name: string): { organization: string; secret: string } {
  const p = resourcePathParams(name);
  return { organization: p.organization ?? '', secret: p.secret ?? '' };
}

/**
 * Drives the Secrets admin surface. Set-only: the `value` is INPUT_ONLY and
 * never returned by the API, so the list/edit views render metadata only. A
 * rotate names `value` in the field mask (via body-field presence); a
 * metadata-only edit omits it. Delete surfaces `FAILED_PRECONDITION` (secret
 * still referenced by a connector) as the server's referrer-naming message.
 */
export function useSecrets(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
}): SecretsAdminContextValue {
  const { $api, apiClient, parent } = input;
  // `parent` is an org resource name; the tested helper yields `{ organization }`.
  const path = useMemo(
    () => ({ organization: resourcePathParams(parent).organization ?? '' }),
    [parent],
  );

  const listQuery = $api.useQuery('get', SECRETS_PATH, { params: { path } });

  const secrets = useMemo<Secret[]>(
    () => listQuery.data?.secrets ?? [],
    [listQuery.data],
  );

  const [dialog, setDialog] = useState<DialogState<Secret>>(initialDialog);
  const [remove, setRemove] = useState<RemoveState<Secret>>(initialRemove);

  const { refetch } = listQuery;

  const openCreate = useCallback(() => {
    setDialog({ ...initialDialog, open: true, mode: 'create' });
  }, []);

  const openEdit = useCallback((secret: Secret) => {
    setDialog({ ...initialDialog, open: true, mode: 'edit', editing: secret });
  }, []);

  const closeDialog = useCallback(() => {
    setDialog((d) => ({ ...d, open: false }));
  }, []);

  const submit = useCallback(
    (values: SecretFormValues) => {
      setDialog((d) => ({ ...d, pending: true, error: null }));
      void (async () => {
        const resp =
          dialog.mode === 'create'
            ? await apiClient.POST(SECRETS_PATH, {
                params: { path, query: { secretId: values.secretId } },
                body: buildSecretCreateBody(values),
              })
            : await apiClient.PATCH(SECRET_PATH, {
                params: { path: secretParams(dialog.editing?.name ?? '') },
                body: buildSecretUpdateBody({
                  values,
                  etag: dialog.editing?.etag,
                }),
              });
        if (resp.error) {
          setDialog((d) => ({
            ...d,
            pending: false,
            error: describeRpcError(resp.error, "Couldn't save the secret."),
          }));
          return;
        }
        setDialog((d) => ({ ...d, open: false, pending: false }));
        await refetch();
      })();
    },
    [apiClient, dialog.mode, dialog.editing, path, refetch],
  );

  const openRemove = useCallback((secret: Secret) => {
    setRemove({ ...initialRemove, target: secret });
  }, []);

  const closeRemove = useCallback(() => {
    setRemove((r) => ({ ...r, target: null }));
  }, []);

  const confirmRemove = useCallback(() => {
    const target = remove.target;
    if (!target?.name) return;
    setRemove((r) => ({ ...r, pending: true, error: null }));
    void (async () => {
      const resp = await apiClient.DELETE(SECRET_PATH, {
        params: {
          path: secretParams(target.name ?? ''),
          query: target.etag ? { etag: target.etag } : {},
        },
      });
      if (resp.error) {
        setRemove((r) => ({
          ...r,
          pending: false,
          error: mapDeleteError(resp.error),
        }));
        return;
      }
      setRemove(initialRemove);
      await refetch();
    })();
  }, [apiClient, remove.target, refetch]);

  return {
    state: {
      secrets,
      isLoading: listQuery.isLoading,
      loadError: listQuery.error
        ? describeRpcError(listQuery.error, "Couldn't load secrets.")
        : null,
      dialog,
      remove,
    },
    actions: {
      openCreate,
      openEdit,
      closeDialog,
      submit,
      openRemove,
      closeRemove,
      confirmRemove,
    },
  };
}
