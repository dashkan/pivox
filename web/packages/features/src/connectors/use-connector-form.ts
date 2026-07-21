'use client';

import { useResourceForm } from '@/resource-admin';
import { connectorsFormDescriptor } from '@/connectors/connectors-descriptor';

import type { ResourceFormResult } from '@/resource-admin';
import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type { Connector, ConnectorFormValues } from '@pivox/ui/resource-admin';
import type { FormMode } from '@pivox/ui/form-page';

/** The delete-confirm slice the edit page's `DeleteDialog` binds to. */
export type { ResourceRemoveState as ConnectorRemoveState } from '@/resource-admin';

/**
 * Orchestrates a routed connector create/edit page — a thin wrapper over the
 * generic, descriptor-driven {@link useResourceForm}. Supplies the connectors
 * form descriptor and maps `connectorId` → the generic `id`; the generic hook
 * owns the SSR-primed record load, the create/update mutation, and the edit
 * delete-confirm. Navigation (`onDone`) is injected from the route.
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
}): ResourceFormResult<Connector, ConnectorFormValues> {
  const { connectorId, ...rest } = input;
  return useResourceForm(connectorsFormDescriptor, { ...rest, id: connectorId });
}
