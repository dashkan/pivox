'use client';

import {
  DeleteDialog,
  ResourceFormPage,
  SecretCreateFields,
  SecretEditFields,
  SecretFormProvider,
  secretLeafId,
} from '@pivox/ui/resource-admin';

import { useSpaces } from '@/spaces/use-spaces';

import { useSecretForm } from './use-secret-form';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { ReactNode } from 'react';

/**
 * Routed secret CREATE page (org rollup) — the secret twin of
 * `ConnectorCreateFeature`. Composes the resource form provider (owns values) +
 * `ResourceFormPage.Create` + the create field-set. The scope PICKER lives inside
 * `SecretCreateFields`; the shell stays scope-blind. `back` / `onCancel` /
 * `onSubmitSuccess` are router values the route computes (from the sanitized
 * `?from=`) and injects here — the feature imports no router.
 */
export function SecretCreateFeature({
  $api,
  apiClient,
  parent,
  back,
  onCancel,
  onSubmitSuccess,
  onDirtyChange,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  back: ReactNode;
  onCancel: () => void;
  onSubmitSuccess: () => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { spaces } = useSpaces({ $api, parent });
  const { pending, error, mutate } = useSecretForm({
    $api,
    apiClient,
    parent,
    mode: 'create',
    onDone: onSubmitSuccess,
  });

  return (
    <SecretFormProvider
      mode="create"
      record={null}
      recordLoading={false}
      loadError={null}
      pending={pending}
      error={error}
      mutate={mutate}
      onCancel={onCancel}
      onDirtyChange={onDirtyChange}
      spaceOptions={spaces}
    >
      <ResourceFormPage.Create back={back}>
        <SecretCreateFields />
      </ResourceFormPage.Create>
    </SecretFormProvider>
  );
}

/**
 * Routed secret EDIT page — the secret twin of `ConnectorEditFeature`. The
 * SSR-primed record loads through `useSecretForm` (same `$api` key as the
 * loader). The provider is REMOUNTED by a `key` on the record name so its
 * lazily-seeded values reset across records. `FormPage.Delete` opens the same
 * `DeleteDialog` the list uses (rendered here as a sibling inside the provider —
 * its copy is secret-specific).
 */
export function SecretEditFeature({
  $api,
  apiClient,
  parent,
  secretId,
  space,
  back,
  onCancel,
  onSubmitSuccess,
  onDirtyChange,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  secretId: string;
  /** Space slug for a space-scoped secret; absent = org-direct. */
  space?: string;
  back: ReactNode;
  onCancel: () => void;
  onSubmitSuccess: () => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { spaces } = useSpaces({ $api, parent });
  const { record, recordLoading, loadError, pending, error, mutate, onDelete, remove } =
    useSecretForm({
      $api,
      apiClient,
      parent,
      mode: 'edit',
      secretId,
      space,
      onDone: onSubmitSuccess,
    });

  return (
    <SecretFormProvider
      key={record?.name ?? secretId}
      mode="edit"
      record={record}
      recordLoading={recordLoading}
      loadError={loadError}
      pending={pending}
      error={error}
      mutate={mutate}
      onCancel={onCancel}
      onDelete={onDelete}
      onDirtyChange={onDirtyChange}
      spaceOptions={spaces}
    >
      <ResourceFormPage.Edit back={back}>
        <SecretEditFields />
      </ResourceFormPage.Edit>
      {/* Custom UI outside the form frame but inside the provider — the same
          DeleteDialog the list row uses; FormPage.Delete opens it via onDelete. */}
      <DeleteDialog
        open={remove.open}
        onOpenChange={remove.onOpenChange}
        title="Delete secret?"
        description={`This permanently deletes "${
          record?.displayName || secretLeafId(record?.name)
        }". A secret still referenced by a connector can't be deleted.`}
        error={remove.error}
        pending={remove.pending}
        onConfirm={remove.onConfirm}
      />
    </SecretFormProvider>
  );
}
