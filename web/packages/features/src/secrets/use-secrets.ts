'use client';

import { useMemo } from 'react';

import { secretsListDescriptor } from '@/secrets/secrets-descriptor';
import { useResourceList } from '@/resource-admin';
import { useSpaces } from '@/spaces/use-spaces';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  ListControlsChange,
  ListControlsValue,
  Secret,
  SecretListExtras,
  ResourceListContextValue,
} from '@pivox/ui/resource-admin';

/**
 * Drives the Secrets admin LIST surface — now a thin wrapper over the generic,
 * descriptor-driven {@link useResourceList}, the secret twin of `useConnectors`.
 * It supplies the secrets descriptor and the route-owned extras (the
 * `spaceOptions` it reads via `useSpaces`); the generic hook owns the scoped
 * query, the list-controls, the row-delete confirm, and pagination. Behavior is
 * unchanged from the previous secret-specific implementation.
 *
 * Reads via the injected `$api`, writes via the injected `apiClient`. Create/edit
 * are ROUTED pages: `onCreate`/`onEdit` (route-injected navigation, setting
 * `?from=<origin>`) become the list's create/edit actions.
 */
export function useSecrets(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  /** Org resource name (`organizations/{slug}`); flows to the list + item routes. */
  parent: string;
  /** List-controls state (route-owned, e.g. URL search params). */
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  /** Navigate to the routed create page (route-owned; sets `?from=`). */
  onCreate: () => void;
  /** Navigate to the routed edit page for a secret (route-owned; sets `?from=`). */
  onEdit: (secret: Secret) => void;
}): ResourceListContextValue<Secret, SecretListExtras> {
  const {
    $api,
    apiClient,
    parent,
    listState,
    onListStateChange,
    onCreate,
    onEdit,
  } = input;

  const { spaces } = useSpaces({ $api, parent });
  const injected = useMemo(() => ({ spaceOptions: spaces }), [spaces]);

  return useResourceList(secretsListDescriptor, {
    $api,
    apiClient,
    parent,
    state: listState,
    onStateChange: onListStateChange,
    injected,
    onCreate,
    onEdit,
  });
}
