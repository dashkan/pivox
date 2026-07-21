'use client';

import { useMemo } from 'react';

import { useResourceList } from '@/resource-admin';
import { connectorsListDescriptor } from '@/connectors/connectors-descriptor';
import { useSpaces } from '@/spaces/use-spaces';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  AgentOption,
  Connector,
  ConnectorListExtras,
  ListControlsChange,
  ListControlsValue,
  ResourceListContextValue,
} from '@pivox/ui/resource-admin';

/**
 * Drives the Connectors admin LIST surface — now a thin wrapper over the generic,
 * descriptor-driven {@link useResourceList}. It supplies the connectors
 * descriptor and the route-owned extras (SSR-prefetched `agentOptions` + the
 * `spaceOptions` it reads via `useSpaces`); the generic hook owns the scoped
 * query, the list-controls, the row-delete confirm, and pagination. Behavior is
 * unchanged from the previous connector-specific implementation.
 *
 * Reads via the injected `$api`, writes via the injected `apiClient`. Create/edit
 * are ROUTED pages: `onCreate`/`onEdit` (route-injected navigation, setting
 * `?from=<origin>`) become the list's create/edit actions.
 */
export function useConnectors(input: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  /** Org resource name (`organizations/{slug}`); flows to the list + item routes. */
  parent: string;
  /** List-controls state (route-owned, e.g. URL search params). */
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  /** All assignable agents (route-owned, SSR-prefetched); passed to the view extras. */
  agentOptions: AgentOption[];
  /** Navigate to the routed create page (route-owned; sets `?from=`). */
  onCreate: () => void;
  /** Navigate to the routed edit page for a connector (route-owned; sets `?from=`). */
  onEdit: (connector: Connector) => void;
}): ResourceListContextValue<Connector, ConnectorListExtras> {
  const {
    $api,
    apiClient,
    parent,
    listState,
    onListStateChange,
    agentOptions,
    onCreate,
    onEdit,
  } = input;

  const { spaces } = useSpaces({ $api, parent });
  const injected = useMemo(
    () => ({ agentOptions, spaceOptions: spaces }),
    [agentOptions, spaces],
  );

  return useResourceList(connectorsListDescriptor, {
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
