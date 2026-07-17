'use client';

import { SecretsAdmin } from '@pivox/ui/resource-admin';

import { useSecrets } from './use-secrets';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsChange,
  ListControlsValue,
  Secret,
} from '@pivox/ui/resource-admin';

/**
 * Secrets CRUD LIST feature. Reads via `$api`, writes via `apiClient`, and yields
 * the domain state to `SecretsAdmin` — the same shape as `ConnectorsFeature`.
 * List-controls state (filter/sort/scope/page) is owned by the caller (the route,
 * from URL search params) and passed in. Create/edit are routed pages, so the
 * route injects `onCreate` / `onEdit` navigation — the feature stays
 * router-agnostic.
 */
export function SecretsFeature({
  $api,
  apiClient,
  parent,
  listState,
  onListStateChange,
  onCreate,
  onEdit,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  onCreate: () => void;
  onEdit: (secret: Secret) => void;
}) {
  const value = useSecrets({
    $api,
    apiClient,
    parent,
    listState,
    onListStateChange,
    onCreate,
    onEdit,
  });
  return (
    <SecretsAdmin.Provider value={value}>
      <SecretsAdmin.Root />
    </SecretsAdmin.Provider>
  );
}
