'use client';

import {
  ConnectorCreateFields,
  ConnectorEditFields,
  ConnectorFormProvider,
  DeleteDialog,
  leafId,
  ResourceFormPage,
} from '@pivox/ui/resource-admin';

import { useSpaces } from '@/spaces/use-spaces';

import { useConnectorForm } from './use-connector-form';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { AgentOption } from '@pivox/ui/resource-admin';
import type { ReactNode } from 'react';

/**
 * Routed connector CREATE page (org rollup). Composes the resource form provider
 * (owns values) + `ResourceFormPage.Create` + the create field-set. The scope
 * PICKER lives inside `ConnectorCreateFields` — scope is just a form value, so
 * `submit()` reads it the same as a route-pinned scope would; the shell stays
 * scope-blind. `back` / `onCancel` / `onSubmitSuccess` are router values the
 * route computes (from the sanitized `?from=`) and injects here — the feature
 * imports no router.
 */
export function ConnectorCreateFeature({
  $api,
  apiClient,
  parent,
  agentOptions,
  back,
  onCancel,
  onSubmitSuccess,
  onDirtyChange,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  agentOptions: AgentOption[];
  back: ReactNode;
  onCancel: () => void;
  onSubmitSuccess: () => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { spaces } = useSpaces({ $api, parent });
  const { pending, error, mutate } = useConnectorForm({
    $api,
    apiClient,
    parent,
    mode: 'create',
    onDone: onSubmitSuccess,
  });

  return (
    <ConnectorFormProvider
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
      agentOptions={agentOptions}
    >
      <ResourceFormPage.Create back={back}>
        <ConnectorCreateFields />
      </ResourceFormPage.Create>
    </ConnectorFormProvider>
  );
}

/**
 * Routed connector EDIT page. SSR-primed record loads through `useConnectorForm`
 * (same `$api` key as the loader). The provider is REMOUNTED by a `key` on the
 * record name so its lazily-seeded values reset across records (values live in
 * the provider, so the keyed reset belongs there — `5.1 keyed-reset` adapted).
 * `FormPage.Delete` opens the same `DeleteDialog` the list uses (rendered here as
 * a sibling inside the provider — its copy + failure text are connector-specific).
 */
export function ConnectorEditFeature({
  $api,
  apiClient,
  parent,
  connectorId,
  space,
  agentOptions,
  back,
  onCancel,
  onSubmitSuccess,
  onDirtyChange,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  connectorId: string;
  /** Space slug for a space-scoped connector; absent = org-direct. */
  space?: string;
  agentOptions: AgentOption[];
  back: ReactNode;
  onCancel: () => void;
  onSubmitSuccess: () => void;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const { spaces } = useSpaces({ $api, parent });
  const { record, recordLoading, loadError, pending, error, mutate, onDelete, remove } =
    useConnectorForm({
      $api,
      apiClient,
      parent,
      mode: 'edit',
      connectorId,
      space,
      onDone: onSubmitSuccess,
    });

  return (
    <ConnectorFormProvider
      key={record?.name ?? connectorId}
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
      agentOptions={agentOptions}
    >
      <ResourceFormPage.Edit back={back}>
        <ConnectorEditFields />
      </ResourceFormPage.Edit>
      {/* Custom UI outside the form frame but inside the provider — the same
          DeleteDialog the list row uses; FormPage.Delete opens it via onDelete. */}
      <DeleteDialog
        open={remove.open}
        onOpenChange={remove.onOpenChange}
        title="Delete connector?"
        description={`This permanently deletes "${
          record?.displayName || leafId(record?.name)
        }". Activities that reference it will fail.`}
        error={remove.error}
        pending={remove.pending}
        onConfirm={remove.onConfirm}
      />
    </ConnectorFormProvider>
  );
}
