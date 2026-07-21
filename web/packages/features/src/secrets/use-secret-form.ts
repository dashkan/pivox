'use client';

import { secretsFormDescriptor } from '@/secrets/secrets-descriptor';
import { useResourceForm } from '@/resource-admin';

import type { ResourceFormResult } from '@/resource-admin';
import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { Secret, SecretFormValues } from '@pivox/ui/resource-admin';
import type { FormMode } from '@pivox/ui/form-page';

/** The delete-confirm slice the edit page's `DeleteDialog` binds to. */
export type { ResourceRemoveState as SecretRemoveState } from '@/resource-admin';

/**
 * Orchestrates a routed secret create/edit page — a thin wrapper over the
 * generic, descriptor-driven {@link useResourceForm}, the secret twin of
 * `useConnectorForm`. Supplies the secrets form descriptor and maps `secretId` →
 * the generic `id`; the generic hook owns the SSR-primed record load, the
 * create/update mutation (write-only value handled in `saveSecret`), and the edit
 * delete-confirm. Navigation (`onDone`) is injected from the route.
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
}): ResourceFormResult<Secret, SecretFormValues> {
  const { secretId, ...rest } = input;
  return useResourceForm(secretsFormDescriptor, { ...rest, id: secretId });
}
