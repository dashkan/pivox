'use client';

import { ConnectorsAdmin } from '@pivox/ui/resource-admin';

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
 * Connectors CRUD LIST feature. Reads via `$api`, writes via `apiClient`, and
 * yields the domain state to `ConnectorsAdmin` — a thin provider over the hook,
 * same shape as the other `*Feature` wrappers. List-controls state (filter/sort/
 * scope/page) and the agent options are owned by the caller (the route, from URL
 * search params / an SSR-prefetched query) and passed in. Create/edit are now
 * routed pages, so the route also injects `onCreate` / `onEdit` navigation — the
 * feature stays router-agnostic.
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
    <ConnectorsAdmin.Provider value={value}>
      <ConnectorsAdmin.Root />
    </ConnectorsAdmin.Provider>
  );
}
