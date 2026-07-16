'use client';

import { SecretsAdmin } from '@pivox/ui/resource-admin';

import { useSecrets } from './use-secrets';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';

/**
 * Secrets CRUD feature (set-only). Reads via `$api`, writes via `apiClient`,
 * and yields the domain state to `SecretsAdmin`. The value is never read back
 * from the API — the feature only ever writes it.
 */
export function SecretsFeature({
  $api,
  apiClient,
  parent,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
}) {
  const value = useSecrets({ $api, apiClient, parent });
  return (
    <SecretsAdmin.Provider value={value}>
      <SecretsAdmin.Root />
    </SecretsAdmin.Provider>
  );
}
