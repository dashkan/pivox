'use client';

import {
  connectorDeleteDescription,
  connectorsListView,
  ResourceList,
} from '@pivox/ui/resource-admin';

import { useConnectors } from './use-connectors';

import type { ApiClient } from '@pivox/client';
import type { ReactQueryApi } from '@pivox/client/react-query';
import type {
  AgentOption,
  Connector,
  ListControlsChange,
  ListControlsValue,
} from '@pivox/ui/resource-admin';

/**
 * Connectors CRUD LIST feature — a thin wrapper: run the descriptor-driven
 * {@link useConnectors} hook and render the generic `ResourceList` with the
 * connectors view. List-controls state (filter/sort/scope/page) + the agent
 * options are owned by the caller (the route, from URL search params / an
 * SSR-prefetched query) and passed in. Create/edit are routed pages, so the route
 * injects `onCreate`/`onEdit` navigation — the feature stays router-agnostic.
 */
export function ConnectorsFeature({
  $api,
  apiClient,
  parent,
  listState,
  onListStateChange,
  agentOptions,
  onCreate,
  onEdit,
}: {
  $api: ReactQueryApi;
  apiClient: ApiClient;
  parent: string;
  listState: ListControlsValue;
  onListStateChange: ListControlsChange;
  agentOptions: AgentOption[];
  onCreate: () => void;
  onEdit: (connector: Connector) => void;
}) {
  const value = useConnectors({
    $api,
    apiClient,
    parent,
    listState,
    onListStateChange,
    agentOptions,
    onCreate,
    onEdit,
  });
  return (
    <ResourceList.Provider value={value}>
      {/* The 90% preset: composed New button + edit+delete affordance column +
          confirm dialog. Presence of the composition is the create/delete config —
          no descriptor flags. */}
      <ResourceList.Default
        view={connectorsListView}
        noun="connector"
        confirmDelete={connectorDeleteDescription}
      />
    </ResourceList.Provider>
  );
}
